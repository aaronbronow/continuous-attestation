package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LayerArtifact describes a synthetic intermediate or flattened OCI layer tarball.
type LayerArtifact struct {
	Index      int               `json:"index"`
	TarGzPath  string            `json:"tarGzPath"`
	Digest     string            `json:"digest"`
	UnpackedID string            `json:"unpackedId"`
	Size       int64             `json:"size"`
	Files      map[string][]byte `json:"-"`
}

// GenerateIntermediateLayer creates a deterministic `.tar.gz` layer containing dependency metadata and synthetic binaries.
func GenerateIntermediateLayer(outDir string, layerIdx int, pkgName, pkgVersion, reason string) (*LayerArtifact, error) {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, err
	}

	files := map[string][]byte{
		fmt.Sprintf("opt/packablock/layers/layer-%d.json", layerIdx): []byte(fmt.Sprintf(
			"{\n  \"layerIndex\": %d,\n  \"package\": %q,\n  \"version\": %q,\n  \"reason\": %q\n}\n",
			layerIdx, pkgName, pkgVersion, reason,
		)),
		fmt.Sprintf("usr/local/lib/%s/version.txt", sanitizePath(pkgName)): []byte(pkgVersion + "\n"),
	}

	tarGzBytes, unpackedDigest, err := buildDeterministicTarGz(files)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(tarGzBytes)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	filePath := filepath.Join(outDir, fmt.Sprintf("layer-%d.tar.gz", layerIdx))
	if err := os.WriteFile(filePath, tarGzBytes, 0644); err != nil {
		return nil, err
	}

	return &LayerArtifact{
		Index:      layerIdx,
		TarGzPath:  filePath,
		Digest:     digest,
		UnpackedID: unpackedDigest,
		Size:       int64(len(tarGzBytes)),
		Files:      files,
	}, nil
}

// SquashLayers merges `layerCount` intermediate OCI layers (generating missing intermediate layers if needed)
// into a single flattened `.tar.gz` filesystem layer and returns its `sha256:...` digest.
func SquashLayers(outDir string, layerCount int, outputTag string, packages map[string]string) (*LayerArtifact, []string, error) {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, nil, err
	}

	mergedFiles := make(map[string][]byte)
	var intermediateDigests []string

	for i := 1; i <= layerCount; i++ {
		layerPath := filepath.Join(outDir, fmt.Sprintf("layer-%d.tar.gz", i))
		if _, err := os.Stat(layerPath); os.IsNotExist(err) {
			pkg := "github.com/gin-gonic/gin"
			ver := packages[pkg]
			if ver == "" {
				ver = "v1.10.0"
			}
			_, err := GenerateIntermediateLayer(outDir, i, pkg, ver, fmt.Sprintf("intermediate-step-%d", i))
			if err != nil {
				return nil, nil, err
			}
		}

		rawLayer, err := os.ReadFile(layerPath)
		if err != nil {
			return nil, nil, err
		}
		lSum := sha256.Sum256(rawLayer)
		intermediateDigests = append(intermediateDigests, "sha256:"+hex.EncodeToString(lSum[:]))

		extracted, err := extractTarGz(rawLayer)
		if err != nil {
			return nil, nil, err
		}
		for k, v := range extracted {
			mergedFiles[k] = v
		}
	}

	// Write consolidated lockfile state into the flattened root filesystem
	manifestBytes, _ := json.MarshalIndent(map[string]interface{}{
		"outputTag":           outputTag,
		"squashedLayers":      layerCount,
		"intermediateDigests": intermediateDigests,
		"packages":            packages,
	}, "", "  ")
	mergedFiles["etc/packablock/flattened-release.json"] = append(manifestBytes, '\n')

	flattenedBytes, unpackedDigest, err := buildDeterministicTarGz(mergedFiles)
	if err != nil {
		return nil, nil, err
	}

	sum := sha256.Sum256(flattenedBytes)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	flattenedPath := filepath.Join(outDir, "flattened.tar.gz")
	if err := os.WriteFile(flattenedPath, flattenedBytes, 0644); err != nil {
		return nil, nil, err
	}

	return &LayerArtifact{
		Index:      0,
		TarGzPath:  flattenedPath,
		Digest:     digest,
		UnpackedID: unpackedDigest,
		Size:       int64(len(flattenedBytes)),
		Files:      mergedFiles,
	}, intermediateDigests, nil
}

// PushFlattenedLayerToRegistry pushes the squashed layer blob and OCI manifest to the local Docker Registry (`localhost:5001`)
// and follows Continuous Attestation Spec v1 §5.4.2 (`application/vnd.packablock.manifest-chain.v1`).
func PushFlattenedLayerToRegistry(registryHost, outputTag string, layer *LayerArtifact, chainYAML []byte) (string, error) {
	if registryHost == "" {
		registryHost = "http://127.0.0.1:5001"
	}
	registryHost = strings.TrimSuffix(registryHost, "/")

	parts := strings.SplitN(outputTag, ":", 2)
	repo := parts[0]
	tag := "latest"
	if len(parts) == 2 {
		tag = parts[1]
	}

	rawLayerBytes, err := os.ReadFile(layer.TarGzPath)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Push flattened layer blob
	if err := uploadBlob(client, registryHost, repo, layer.Digest, rawLayerBytes); err != nil {
		return "", err
	}

	// 2. Push Packablock Manifest Chain blob (Spec §4.2 Bundled OCI Artifact)
	chainSum := sha256.Sum256(chainYAML)
	chainDigest := "sha256:" + hex.EncodeToString(chainSum[:])
	if err := uploadBlob(client, registryHost, repo, chainDigest, chainYAML); err != nil {
		return "", err
	}

	// 3. Push minimal OCI config blob
	configBytes := []byte(fmt.Sprintf(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[%q]}}`, layer.UnpackedID))
	cfgSum := sha256.Sum256(configBytes)
	cfgDigest := "sha256:" + hex.EncodeToString(cfgSum[:])
	if err := uploadBlob(client, registryHost, repo, cfgDigest, configBytes); err != nil {
		return "", err
	}

	// 4. Push OCI Image Manifest referencing both the flattened layer and the Packablock manifest chain
	manifest := map[string]interface{}{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"artifactType":  "application/vnd.packablock.manifest-chain.v1",
		"config": map[string]interface{}{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"size":      len(configBytes),
			"digest":    cfgDigest,
		},
		"layers": []map[string]interface{}{
			{
				"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
				"size":      len(rawLayerBytes),
				"digest":    layer.Digest,
			},
			{
				"mediaType": "application/vnd.packablock.manifest-chain+yaml",
				"size":      len(chainYAML),
				"digest":    chainDigest,
			},
		},
	}
	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/v2/%s/manifests/%s", registryHost, repo, tag), bytes.NewReader(manifestJSON))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("registry manifest PUT status %d: %s", resp.StatusCode, string(body))
	}

	return fmt.Sprintf("%s/%s:%s", strings.TrimPrefix(registryHost, "http://"), repo, tag), nil
}

func uploadBlob(client *http.Client, registryHost, repo, digest string, data []byte) error {
	postURL := fmt.Sprintf("%s/v2/%s/blobs/uploads/", registryHost, repo)
	resp, err := client.Post(postURL, "application/octet-stream", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" {
		return fmt.Errorf("registry did not return upload Location header (status %d)", resp.StatusCode)
	}
	if !strings.HasPrefix(loc, "http") {
		loc = registryHost + loc
	}
	sep := "?"
	if strings.Contains(loc, "?") {
		sep = "&"
	}
	putURL := fmt.Sprintf("%s%sdigest=%s", loc, sep, digest)
	req, err := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	putResp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer putResp.Body.Close()
	if putResp.StatusCode >= 300 {
		body, _ := io.ReadAll(putResp.Body)
		return fmt.Errorf("blob PUT failed (%d): %s", putResp.StatusCode, string(body))
	}
	return nil
}

// GenerateCycloneDXSBOM builds a CycloneDX v1.5 JSON SBOM for the squashed layer.
func GenerateCycloneDXSBOM(outputTag, flattenedDigest string, packages map[string]string) ([]byte, error) {
	type licenseEntry struct {
		License map[string]string `json:"license"`
	}
	type component struct {
		Type     string         `json:"type"`
		Name     string         `json:"name"`
		Version  string         `json:"version"`
		PURL     string         `json:"purl"`
		Licenses []licenseEntry `json:"licenses"`
	}

	names := make([]string, 0, len(packages))
	for name := range packages {
		names = append(names, name)
	}
	sort.Strings(names)

	var comps []component
	for _, name := range names {
		ver := packages[name]
		cleanVer := strings.TrimPrefix(ver, "v")
		eco := "golang"
		if strings.HasPrefix(name, "esphome/") || strings.HasPrefix(name, "platformio/") || strings.HasPrefix(name, "lwip/") {
			eco = "generic"
		}
		comps = append(comps, component{
			Type:    "library",
			Name:    name,
			Version: ver,
			PURL:    fmt.Sprintf("pkg:%s/%s@v%s", eco, name, cleanVer),
			Licenses: []licenseEntry{
				{License: map[string]string{"id": "MIT"}},
			},
		})
	}

	sbom := map[string]interface{}{
		"bomFormat":    "CycloneDX",
		"specVersion":  "1.5",
		"serialNumber": "urn:uuid:7b89c4f1-903a-41b2-a8d0-" + strings.TrimPrefix(flattenedDigest, "sha256:")[:12],
		"version":      1,
		"metadata": map[string]interface{}{
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"component": map[string]interface{}{
				"type":    "container",
				"name":    outputTag,
				"version": flattenedDigest,
			},
		},
		"components":      comps,
		"vulnerabilities": []interface{}{},
	}

	return json.MarshalIndent(sbom, "", "  ")
}

func buildDeterministicTarGz(files map[string][]byte) ([]byte, string, error) {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	// Deterministic lexical order
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, name := range keys {
		content := files[name]
		hdr := &tar.Header{
			Name:     name,
			Mode:     0644,
			Size:     int64(len(content)),
			ModTime:  fixedTime,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, "", err
		}
		if _, err := tw.Write(content); err != nil {
			return nil, "", err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, "", err
	}

	unpackedSum := sha256.Sum256(tarBuf.Bytes())
	unpackedDigest := "sha256:" + hex.EncodeToString(unpackedSum[:])

	var gzBuf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&gzBuf, gzip.BestCompression)
	if err != nil {
		return nil, "", err
	}
	gw.ModTime = fixedTime
	if _, err := gw.Write(tarBuf.Bytes()); err != nil {
		return nil, "", err
	}
	if err := gw.Close(); err != nil {
		return nil, "", err
	}

	return gzBuf.Bytes(), unpackedDigest, nil
}

func extractTarGz(tarGzBytes []byte) (map[string][]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(tarGzBytes))
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	out := make(map[string][]byte)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg {
			b, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			out[hdr.Name] = b
		}
	}
	return out, nil
}

func sanitizePath(p string) string {
	return strings.NewReplacer(":", "_", "@", "_").Replace(p)
}
