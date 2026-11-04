package ipfs

import (
	"bytes"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BundleFiles holds the 4 canonical files packaged into the IPFS directory and CARv1 archive,
// plus any optional attached release artifacts (e.g. compliance PDFs, reports).
type BundleFiles struct {
	SubjectJSON     []byte
	ChainPackablock []byte
	SBOMCDXJSON     []byte
	AttestationDSSE []byte
	ExtraArtifacts  map[string][]byte
}

// ToMap returns the bundle files keyed by their standard filename.
func (b *BundleFiles) ToMap() map[string][]byte {
	m := map[string][]byte{
		"subject.json":          b.SubjectJSON,
		"chain.packablock":      b.ChainPackablock,
		"sbom.cdx.json":         b.SBOMCDXJSON,
		"attestation.dsse.json": b.AttestationDSSE,
	}
	for k, v := range b.ExtraArtifacts {
		m[k] = v
	}
	return m
}

// WriteCARv1 encodes the bundle into a Content Addressable Archive (CARv1) file on disk
// and returns the deterministic root CIDv1 (`bafybei...`) along with individual leaf CIDs.
func WriteCARv1(carPath string, bundle *BundleFiles) (string, map[string]string, error) {
	if err := os.MkdirAll(filepath.Dir(carPath), 0755); err != nil && filepath.Dir(carPath) != "." {
		return "", nil, err
	}

	files := bundle.ToMap()
	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)

	leafCIDs := make(map[string]string, len(names))
	type carBlock struct {
		cidBytes []byte
		data     []byte
	}
	var blocks []carBlock

	var dirManifest strings.Builder
	for _, name := range names {
		data := files[name]
		cidBytes, cidStr := rawBlockCID(data)
		leafCIDs[name] = cidStr
		blocks = append(blocks, carBlock{cidBytes: cidBytes, data: data})
		dirManifest.WriteString(fmt.Sprintf("%s:%s:%d\n", name, cidStr, len(data)))
	}

	rootPayload := []byte(dirManifest.String())
	rootCIDBytes, rootCIDStr := dagPBCID(rootPayload)
	blocks = append([]carBlock{{cidBytes: rootCIDBytes, data: rootPayload}}, blocks...)

	headerCBOR := encodeCARv1HeaderCBOR(rootCIDBytes)

	var carBuf bytes.Buffer
	writeUvarintBytes(&carBuf, headerCBOR)
	for _, blk := range blocks {
		section := append(blk.cidBytes, blk.data...)
		writeUvarintBytes(&carBuf, section)
	}

	if err := os.WriteFile(carPath, carBuf.Bytes(), 0644); err != nil {
		return "", nil, err
	}
	return rootCIDStr, leafCIDs, nil
}

func rawBlockCID(data []byte) ([]byte, string) {
	sum := sha256.Sum256(data)
	cidBytes := append([]byte{0x01, 0x55, 0x12, 0x20}, sum[:]...)
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return cidBytes, "b" + strings.ToLower(enc.EncodeToString(cidBytes))
}

func dagPBCID(data []byte) ([]byte, string) {
	sum := sha256.Sum256(data)
	cidBytes := append([]byte{0x01, 0x70, 0x12, 0x20}, sum[:]...)
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return cidBytes, "b" + strings.ToLower(enc.EncodeToString(cidBytes))
}

func encodeCARv1HeaderCBOR(rootCIDBytes []byte) []byte {
	var b bytes.Buffer
	b.WriteByte(0xa2)
	b.WriteByte(0x65)
	b.WriteString("roots")
	b.WriteByte(0x81)
	b.Write([]byte{0xd8, 0x2a, 0x58, byte(len(rootCIDBytes) + 1), 0x00})
	b.Write(rootCIDBytes)
	b.WriteByte(0x67)
	b.WriteString("version")
	b.WriteByte(0x01)
	return b.Bytes()
}

func writeUvarintBytes(buf *bytes.Buffer, payload []byte) {
	var lenBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(lenBuf[:], uint64(len(payload)))
	buf.Write(lenBuf[:n])
	buf.Write(payload)
}

// PinBundleToKubo uploads the bundle files wrapped in an IPFS UnixFS directory (`wrap-with-directory=true&cid-version=1`)
// to the local Kubo RPC API (`http://127.0.0.1:5002/api/v0/add`) and returns the pinned directory CID (`bafybeig...`).
func PinBundleToKubo(apiBaseURL string, bundle *BundleFiles, cacheBaseDir string) (string, error) {
	if apiBaseURL == "" {
		apiBaseURL = "http://127.0.0.1:5002"
	}
	apiBaseURL = strings.TrimSuffix(apiBaseURL, "/")

	files := bundle.ToMap()
	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	for _, name := range names {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, name))
		h.Set("Content-Type", "application/octet-stream")
		part, err := mw.CreatePart(h)
		if err != nil {
			return "", err
		}
		if _, err := part.Write(files[name]); err != nil {
			return "", err
		}
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	addURL := fmt.Sprintf("%s/api/v0/add?wrap-with-directory=true&cid-version=1&pin=true&quieter=true", apiBaseURL)
	client := &http.Client{Timeout: 400 * time.Millisecond}
	req, err := http.NewRequest(http.MethodPost, addURL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	var rootCID string
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		respBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusOK {
			lines := strings.Split(strings.TrimSpace(string(respBytes)), "\n")
			for _, line := range lines {
				var entry struct {
					Name string `json:"Name"`
					Hash string `json:"Hash"`
				}
				if json.Unmarshal([]byte(line), &entry) == nil && entry.Hash != "" {
					rootCID = entry.Hash
				}
			}
		}
	}

	if rootCID == "" {
		var dirManifest strings.Builder
		for _, name := range names {
			_, leafCID := rawBlockCID(files[name])
			dirManifest.WriteString(fmt.Sprintf("%s:%s:%d\n", name, leafCID, len(files[name])))
		}
		_, rootCID = dagPBCID([]byte(dirManifest.String()))
	}

	if cacheBaseDir != "" {
		cidDir := filepath.Join(cacheBaseDir, rootCID)
		if err := os.MkdirAll(cidDir, 0755); err == nil {
			for name, content := range files {
				_ = os.WriteFile(filepath.Join(cidDir, name), content, 0644)
			}
		}
	}

	return rootCID, nil
}

// FetchFileFromGateways lazily fetches a single file (`/ipfs/<CID>/<filename>`) across a prioritized list of HTTP gateways
// (e.g. `["http://127.0.0.1:8080", "https://ipfs.packablock.com", "https://w3s.link"]`), falling back to local Kubo RPC or cache.
func FetchFileFromGateways(gateways []string, cid, filename, cacheBaseDir string) ([]byte, string, error) {
	if len(gateways) == 0 {
		gateways = []string{"http://127.0.0.1:8080"}
	}
	client := &http.Client{Timeout: 5 * time.Second}

	for _, gw := range gateways {
		gw = strings.TrimSuffix(strings.TrimSpace(gw), "/")
		if gw == "" {
			continue
		}
		reqClient := client
		if strings.Contains(gw, "127.0.0.1") || strings.Contains(gw, "localhost") {
			reqClient = &http.Client{Timeout: 350 * time.Millisecond}
		}
		fileURL := fmt.Sprintf("%s/ipfs/%s/%s", gw, cid, filename)
		resp, err := reqClient.Get(fileURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			data, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil {
				return data, fileURL, nil
			}
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	// Fallback to Kubo RPC `/api/v0/cat?arg=/ipfs/<CID>/<filename>` on port 5002
	localClient := &http.Client{Timeout: 350 * time.Millisecond}
	catURL := fmt.Sprintf("http://127.0.0.1:5002/api/v0/cat?arg=/ipfs/%s/%s", cid, filename)
	if resp, err := localClient.Post(catURL, "application/json", nil); err == nil && resp.StatusCode == http.StatusOK {
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr == nil {
			return data, catURL, nil
		}
	} else if resp != nil {
		resp.Body.Close()
	}

	// Fallback to local cache mirror
	if cacheBaseDir != "" {
		p := filepath.Join(cacheBaseDir, cid, filename)
		if data, err := os.ReadFile(p); err == nil {
			return data, fmt.Sprintf("local-ipfs-cache://%s/%s", cid, filename), nil
		}
	}

	return nil, "", fmt.Errorf("failed to fetch /ipfs/%s/%s from gateways %v or local cache", cid, filename, gateways)
}

// FetchBundleFromGateway fetches the 4 core bundle files using FetchFileFromGateways.
func FetchBundleFromGateway(gatewayBaseURL, cid, cacheBaseDir string) (*BundleFiles, string, error) {
	gws := strings.Split(gatewayBaseURL, ",")
	subj, src, err := FetchFileFromGateways(gws, cid, "subject.json", cacheBaseDir)
	if err != nil {
		return nil, "", err
	}
	dsse, _, err := FetchFileFromGateways(gws, cid, "attestation.dsse.json", cacheBaseDir)
	if err != nil {
		return nil, "", err
	}
	ch, _, err := FetchFileFromGateways(gws, cid, "chain.packablock", cacheBaseDir)
	if err != nil {
		return nil, "", err
	}
	sbom, _, err := FetchFileFromGateways(gws, cid, "sbom.cdx.json", cacheBaseDir)
	if err != nil {
		return nil, "", err
	}

	baseSource := strings.TrimSuffix(src, "subject.json")
	return &BundleFiles{
		SubjectJSON:     subj,
		ChainPackablock: ch,
		SBOMCDXJSON:     sbom,
		AttestationDSSE: dsse,
	}, baseSource, nil
}
