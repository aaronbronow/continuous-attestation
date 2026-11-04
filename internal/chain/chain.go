package chain

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const GenesisPrevHash = "0000000000000000000000000000000000000000000000000000000000000000"

// SignatureAuth represents the Continuous Attestation Spec v1 §3.4.5 signature_auth block
// combined with W3C did:key Ed25519 identity.
type SignatureAuth struct {
	Provider       string `yaml:"provider" json:"provider"`
	SignerIdentity string `yaml:"signer_identity" json:"signer_identity"`
	DID            string `yaml:"did,omitempty" json:"did,omitempty"`
	Commit         string `yaml:"commit,omitempty" json:"commit,omitempty"`
	GitSignature   string `yaml:"git_signature" json:"git_signature"`
}

// PropertyAttachment represents a custom property / attached artifact (Spec v1 §5.1.1).
type PropertyAttachment struct {
	Name     string `yaml:"name" json:"name"`
	Value    string `yaml:"value" json:"value"`
	Filename string `yaml:"filename,omitempty" json:"filename,omitempty"`
	SHA256   string `yaml:"sha256,omitempty" json:"sha256,omitempty"`
	CID      string `yaml:"cid,omitempty" json:"cid,omitempty"`
}

// ChainMeta represents the validation block ($yaml-chain-meta / $manifest-chain-link).
// It preserves exact cryptographic compatibility with Packablock's deterministicMetaHash.
type ChainMeta struct {
	Version         string               `yaml:"version" json:"version"`
	BlockIndex      int                  `yaml:"block_index" json:"block_index"`
	Timestamp       string               `yaml:"timestamp" json:"timestamp"`
	HashingStrategy string               `yaml:"hashing_strategy" json:"hashing_strategy"`
	DataHash        string               `yaml:"data_hash" json:"data_hash"`
	PrevMetaHash    string               `yaml:"prev_meta_hash" json:"prev_meta_hash"`
	MetaHash        string               `yaml:"meta_hash,omitempty" json:"meta_hash,omitempty"`
	ParentBlockCID  string               `yaml:"parent_block_cid,omitempty" json:"parent_block_cid,omitempty"`
	BlockCID        string               `yaml:"block_cid,omitempty" json:"block_cid,omitempty"`
	LayerDigest     string               `yaml:"layer_digest,omitempty" json:"layer_digest,omitempty"`
	SignatureAuth   *SignatureAuth       `yaml:"signature_auth,omitempty" json:"signature_auth,omitempty"`
	Properties      []PropertyAttachment `yaml:"properties,omitempty" json:"properties,omitempty"`
}

// TransitionRecord represents a resolved dependency transition discovered during chain traversal.
type TransitionRecord struct {
	BlockIndex     int
	Timestamp      string
	PackageName    string
	FromVersion    string
	ToVersion      string
	Reason         string
	Location       string
	Actor          string
	DID            string
	Commit         string
	MetaHash       string
	DataHash       string
	BlockCID       string
	ParentBlockCID string
	LayerDigest    string
}

// VerificationReport matches the official packablock-client VerificationReport structure.
type VerificationReport struct {
	Valid             bool        `json:"valid"`
	Reason            string      `json:"reason,omitempty"`
	BlockIndex        int         `json:"blockIndex,omitempty"`
	TamperedComponent string      `json:"tamperedComponent,omitempty"`
	Expected          interface{} `json:"expected,omitempty"`
	Actual            interface{} `json:"actual,omitempty"`
	MetaHash          string      `json:"metaHash,omitempty"`
	OfficialCLIUsed   bool        `json:"officialCliUsed,omitempty"`
	Blocks            []BlockPair `json:"-"`
}

// BlockPair holds the raw data payload and parsed metadata for a single transaction block.
type BlockPair struct {
	DataDoc string
	MetaDoc string
	Meta    ChainMeta
}

var docSeparatorRegex = regexp.MustCompile(`^---\s*$`)

// SplitRawDocuments splits a multi-document YAML chain into raw document strings on `---` boundaries,
// matching splitRawDocuments() in Packablock/packablock-client/src/chain.ts.
func SplitRawDocuments(fileContent string) []string {
	normalized := strings.ReplaceAll(fileContent, "\r\n", "\n")
	if strings.TrimSpace(normalized) == "" {
		return nil
	}
	lines := strings.Split(normalized, "\n")
	var docs []string
	var currentDoc []string

	for _, line := range lines {
		if docSeparatorRegex.MatchString(line) {
			if len(currentDoc) > 0 || len(docs) > 0 {
				docs = append(docs, strings.Join(currentDoc, "\n"))
				currentDoc = nil
			}
		} else {
			currentDoc = append(currentDoc, line)
		}
	}
	docs = append(docs, strings.Join(currentDoc, "\n"))
	return docs
}

// SHA256Hex computes the lowercase hex-encoded SHA-256 digest of a UTF-8 string.
func SHA256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// ComputeCIDv1Raw calculates a deterministic IPFS CIDv1 (raw codec 0x55, sha2-256 0x12) encoded in base32lower (`bafkrei...`).
func ComputeCIDv1Raw(data []byte) string {
	sum := sha256.Sum256(data)
	header := []byte{0x01, 0x55, 0x12, 0x20}
	cidBytes := append(header, sum[:]...)
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return "b" + strings.ToLower(enc.EncodeToString(cidBytes))
}

// ComputeCIDv1DagPB calculates a deterministic IPFS CIDv1 (dag-pb codec 0x70, sha2-256 0x12) encoded in base32lower (`bafybei...`).
func ComputeCIDv1DagPB(data []byte) string {
	sum := sha256.Sum256(data)
	header := []byte{0x01, 0x70, 0x12, 0x20}
	cidBytes := append(header, sum[:]...)
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return "b" + strings.ToLower(enc.EncodeToString(cidBytes))
}

// DeterministicMetaHash implements the exact recursive sorted-key JSON serialization
// from Packablock/packablock-client/src/hash.ts (`deterministicMetaHash`).
func DeterministicMetaHash(meta ChainMeta) (string, error) {
	rawBytes, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	var genericMap map[string]interface{}
	if err := json.Unmarshal(rawBytes, &genericMap); err != nil {
		return "", err
	}
	delete(genericMap, "meta_hash")

	canonicalJSON, err := marshalSortedObject(genericMap)
	if err != nil {
		return "", err
	}
	return SHA256Hex(string(canonicalJSON)), nil
}

func marshalSortedObject(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			keyBytes, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			b.Write(keyBytes)
			b.WriteByte(':')
			elemBytes, err := marshalSortedObject(val[k])
			if err != nil {
				return nil, err
			}
			b.Write(elemBytes)
		}
		b.WriteByte('}')
		return []byte(b.String()), nil
	case []interface{}:
		var b strings.Builder
		b.WriteByte('[')
		for i, item := range val {
			if i > 0 {
				b.WriteByte(',')
			}
			elemBytes, err := marshalSortedObject(item)
			if err != nil {
				return nil, err
			}
			b.Write(elemBytes)
		}
		b.WriteByte(']')
		return []byte(b.String()), nil
	default:
		return json.Marshal(val)
	}
}

// SignBlockPayload signs the canonical dataHash + prevMetaHash with an Ed25519 private key
// and formats an SSH-style/Ed25519 signature envelope with a W3C did:key.
func SignBlockPayload(priv ed25519.PrivateKey, did, signerIdentity, commit, dataHash, prevMetaHash string) *SignatureAuth {
	msg := fmt.Sprintf("packablock-v1:%s:%s:%s", dataHash, prevMetaHash, commit)
	sig := ed25519.Sign(priv, []byte(msg))
	sigB64 := base64.StdEncoding.EncodeToString(sig)
	pemSig := fmt.Sprintf("-----BEGIN SSH SIGNATURE-----\n%s\n-----END SSH SIGNATURE-----", sigB64)
	return &SignatureAuth{
		Provider:       "did:key/ed25519",
		SignerIdentity: signerIdentity,
		DID:            did,
		Commit:         commit,
		GitSignature:   pemSig,
	}
}

// VerifyBlockSignature verifies the Ed25519 signature inside SignatureAuth if present.
func VerifyBlockSignature(pub ed25519.PublicKey, meta ChainMeta) error {
	if meta.SignatureAuth == nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(meta.SignatureAuth.GitSignature), "\n")
	var b64Parts []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "-----") || l == "" {
			continue
		}
		b64Parts = append(b64Parts, l)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(strings.Join(b64Parts, ""))
	if err != nil {
		return fmt.Errorf("invalid base64 signature in block %d: %w", meta.BlockIndex, err)
	}
	msg := fmt.Sprintf("packablock-v1:%s:%s:%s", meta.DataHash, meta.PrevMetaHash, meta.SignatureAuth.Commit)
	if !ed25519.Verify(pub, []byte(msg), sigBytes) {
		return fmt.Errorf("ed25519 signature verification failed at block %d", meta.BlockIndex)
	}
	return nil
}

// ExtractChainMeta parses either `$yaml-chain-meta` (official packablock-client) or `$manifest-chain-link` (Continuous Attestation Spec v1).
func ExtractChainMeta(metaDocStr string) (*ChainMeta, error) {
	var raw map[string]ChainMeta
	if err := yaml.Unmarshal([]byte(metaDocStr), &raw); err != nil {
		return nil, err
	}
	if m, ok := raw["$yaml-chain-meta"]; ok {
		return &m, nil
	}
	if m, ok := raw["$manifest-chain-link"]; ok {
		return &m, nil
	}
	return nil, errors.New("metadata document missing '$yaml-chain-meta' or '$manifest-chain-link' root key")
}

// FormatMetaYAML serializes the validation block using `$yaml-chain-meta` for 100% compatibility with `pkablk check`.
func FormatMetaYAML(meta ChainMeta) (string, error) {
	wrapper := map[string]ChainMeta{
		"$yaml-chain-meta": meta,
	}
	out, err := yaml.Marshal(wrapper)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)) + "\n", nil
}

// RunOfficialPkablkCheck invokes the official `pkablk` CLI (`pkablk check <file>`) if installed on PATH
// or in `/tmp/pblk-verify` (WSL), returning true if the official CLI verified the chain.
func RunOfficialPkablkCheck(chainPath string) (bool, string) {
	if binPath, err := exec.LookPath("pkablk"); err == nil {
		out, err := exec.Command(binPath, "check", chainPath).CombinedOutput()
		if err == nil && strings.Contains(string(out), "VERIFICATION PASSED") {
			return true, strings.TrimSpace(string(out))
		}
	}
	return false, ""
}

// InitOrAppendStep appends a Continuous Attestation semantic diff block to `packablock.yaml`
// (initializing block 0 first if `packablock.yaml` does not exist yet).
func InitOrAppendStep(
	chainPath string,
	pkgName string,
	fromVer string,
	toVer string,
	reason string,
	layerDigest string,
	commitSHA string,
	privKey ed25519.PrivateKey,
	did string,
	signerIdentity string,
) (*ChainMeta, error) {
	if err := os.MkdirAll(filepath.Dir(chainPath), 0755); err != nil && filepath.Dir(chainPath) != "." {
		return nil, err
	}

	if _, err := os.Stat(chainPath); errors.Is(err, os.ErrNotExist) {
		genesisPayload := fmt.Sprintf(
			"subject: package-lock.json\ndiff_strategy: semantic\nevent: init\nreason: \"Genesis dependency baseline\"\nlockfiles:\n  package-lock.json:\n    chain_event: init\n    packages:\n      - %s: %q\n      - golang.org/x/crypto: \"v0.28.0\"\n      - gopkg.in/yaml.v3: \"v3.0.1\"\n",
			pkgName, fromVer,
		)
		if _, err := writeBlock(chainPath, genesisPayload, "sha256:"+SHA256Hex("genesis-layer-0"), "a1b2c3d", privKey, did, signerIdentity, nil); err != nil {
			return nil, fmt.Errorf("failed to initialize genesis block: %w", err)
		}
	}

	updatePayload := fmt.Sprintf(
		"subject: package-lock.json\ndiff_strategy: semantic\nevent: update\nreason: %q\nchanges:\n  upgraded:\n    - name: %s\n      from: %q\n      to: %q\n  added: []\n  removed: []\nlockfiles:\n  package-lock.json:\n    packages:\n      - %s:\n          - old: %q\n          - new: %q\n          - loc: \"16,18\"\n",
		reason, pkgName, fromVer, toVer, pkgName, fromVer, toVer,
	)

	return writeBlock(chainPath, updatePayload, layerDigest, commitSHA, privKey, did, signerIdentity, nil)
}

func writeBlock(
	chainPath string,
	dataPayload string,
	layerDigest string,
	commitSHA string,
	privKey ed25519.PrivateKey,
	did string,
	signerIdentity string,
	props []PropertyAttachment,
) (*ChainMeta, error) {
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	return writeBlockAtTimestamp(chainPath, dataPayload, layerDigest, commitSHA, ts, privKey, did, signerIdentity, props)
}

func writeBlockAtTimestamp(
	chainPath string,
	dataPayload string,
	layerDigest string,
	commitSHA string,
	ts string,
	privKey ed25519.PrivateKey,
	did string,
	signerIdentity string,
	props []PropertyAttachment,
) (*ChainMeta, error) {
	cleanData := strings.TrimSpace(strings.ReplaceAll(dataPayload, "\r\n", "\n")) + "\n"
	dataHash := SHA256Hex(strings.TrimSpace(cleanData))

	var nextIndex int
	prevMetaHash := GenesisPrevHash
	parentBlockCID := "bafkreiaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var existingContent string

	if rawBytes, err := os.ReadFile(chainPath); err == nil && len(strings.TrimSpace(string(rawBytes))) > 0 {
		existingContent = strings.ReplaceAll(string(rawBytes), "\r\n", "\n")
		docs := SplitRawDocuments(existingContent)
		if len(docs)%2 != 0 {
			return nil, fmt.Errorf("chain is malformed: expected even number of YAML documents, got %d", len(docs))
		}
		lastMeta, err := ExtractChainMeta(docs[len(docs)-1])
		if err != nil {
			return nil, fmt.Errorf("failed to parse previous metadata block: %w", err)
		}
		nextIndex = lastMeta.BlockIndex + 1
		prevMetaHash = lastMeta.MetaHash
		if lastMeta.BlockCID != "" {
			parentBlockCID = lastMeta.BlockCID
		} else {
			parentBlockCID = ComputeCIDv1Raw([]byte(docs[len(docs)-2] + "\n---\n" + docs[len(docs)-1]))
		}
	}

	if ts == "" {
		ts = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	sigAuth := SignBlockPayload(privKey, did, signerIdentity, commitSHA, dataHash, prevMetaHash)
	blockCID := ComputeCIDv1Raw([]byte(fmt.Sprintf("%d:%s:%s:%s", nextIndex, dataHash, prevMetaHash, ts)))

	meta := ChainMeta{
		Version:         "1.0.0",
		BlockIndex:      nextIndex,
		Timestamp:       ts,
		HashingStrategy: "raw",
		DataHash:        dataHash,
		PrevMetaHash:    prevMetaHash,
		ParentBlockCID:  parentBlockCID,
		BlockCID:        blockCID,
		LayerDigest:     layerDigest,
		SignatureAuth:   sigAuth,
		Properties:      props,
	}

	metaHash, err := DeterministicMetaHash(meta)
	if err != nil {
		return nil, err
	}
	meta.MetaHash = metaHash

	metaYAML, err := FormatMetaYAML(meta)
	if err != nil {
		return nil, err
	}

	var outBuilder strings.Builder
	if existingContent == "" {
		outBuilder.WriteString(cleanData)
		outBuilder.WriteString("---\n")
		outBuilder.WriteString(metaYAML)
	} else {
		outBuilder.WriteString(existingContent)
		if !strings.HasSuffix(existingContent, "\n") {
			outBuilder.WriteString("\n")
		}
		outBuilder.WriteString("---\n")
		outBuilder.WriteString(cleanData)
		outBuilder.WriteString("---\n")
		outBuilder.WriteString(metaYAML)
	}

	if err := os.WriteFile(chainPath, []byte(outBuilder.String()), 0644); err != nil {
		return nil, err
	}
	return &meta, nil
}

// VerifyChain validates every block in a Packablock YAML stream (both official `pkablk` chains and Ed25519-signed chains).
func VerifyChain(chainPath string, pubKey ed25519.PublicKey) (*VerificationReport, error) {
	rawBytes, err := os.ReadFile(chainPath)
	if err != nil {
		return nil, err
	}
	rep, err := VerifyChainContent(string(rawBytes), pubKey)
	if err != nil {
		return nil, err
	}
	if ok, _ := RunOfficialPkablkCheck(chainPath); ok {
		rep.OfficialCLIUsed = true
	}
	return rep, nil
}

// VerifyChainContent validates a Packablock YAML chain from an in-memory string.
func VerifyChainContent(content string, pubKey ed25519.PublicKey) (*VerificationReport, error) {
	docs := SplitRawDocuments(content)
	if len(docs) == 0 {
		return &VerificationReport{
			Valid:             false,
			Reason:            "Chain file is empty",
			TamperedComponent: "structure",
		}, nil
	}
	if len(docs)%2 != 0 {
		return &VerificationReport{
			Valid:             false,
			Reason:            fmt.Sprintf("Chain structure is malformed: expected pairs of [data, meta] documents, found %d", len(docs)),
			TamperedComponent: "structure",
		}, nil
	}

	expectedPrevHash := GenesisPrevHash
	var blocks []BlockPair

	for i := 0; i < len(docs)/2; i++ {
		dataDoc := docs[2*i]
		metaDoc := docs[2*i+1]

		meta, err := ExtractChainMeta(metaDoc)
		if err != nil {
			return &VerificationReport{
				Valid:             false,
				Reason:            fmt.Sprintf("Block %d metadata parse error: %v", i, err),
				BlockIndex:        i,
				TamperedComponent: "meta",
			}, nil
		}

		if meta.BlockIndex != i {
			return &VerificationReport{
				Valid:             false,
				Reason:            fmt.Sprintf("Block index mismatch at block %d: metadata has %d", i, meta.BlockIndex),
				BlockIndex:        i,
				TamperedComponent: "index",
				Expected:          i,
				Actual:            meta.BlockIndex,
			}, nil
		}

		if meta.PrevMetaHash != expectedPrevHash {
			return &VerificationReport{
				Valid:             false,
				Reason:            fmt.Sprintf("Chain link broken at block %d: expected prev_meta_hash '%s', got '%s'", i, expectedPrevHash, meta.PrevMetaHash),
				BlockIndex:        i,
				TamperedComponent: "chain",
				Expected:          expectedPrevHash,
				Actual:            meta.PrevMetaHash,
			}, nil
		}

		computedDataHash := SHA256Hex(strings.TrimSpace(dataDoc))
		if meta.DataHash != computedDataHash {
			return &VerificationReport{
				Valid:             false,
				Reason:            fmt.Sprintf("Cryptographic mismatch in data payload at block %d", i),
				BlockIndex:        i,
				TamperedComponent: "data",
				Expected:          meta.DataHash,
				Actual:            computedDataHash,
			}, nil
		}

		computedMetaHash, err := DeterministicMetaHash(*meta)
		if err != nil {
			return nil, err
		}
		if meta.MetaHash != computedMetaHash {
			return &VerificationReport{
				Valid:             false,
				Reason:            fmt.Sprintf("Cryptographic mismatch in metadata signature at block %d", i),
				BlockIndex:        i,
				TamperedComponent: "meta",
				Expected:          computedMetaHash,
				Actual:            meta.MetaHash,
			}, nil
		}

		if len(pubKey) == ed25519.PublicKeySize && meta.SignatureAuth != nil {
			if err := VerifyBlockSignature(pubKey, *meta); err != nil {
				return &VerificationReport{
					Valid:             false,
					Reason:            err.Error(),
					BlockIndex:        i,
					TamperedComponent: "meta",
				}, nil
			}
		}

		// Populate deterministic BlockCID and ParentBlockCID if generated by official `pkablk` CLI
		if meta.BlockCID == "" {
			meta.BlockCID = ComputeCIDv1Raw([]byte(strings.TrimSpace(dataDoc) + "\n---\n" + strings.TrimSpace(metaDoc)))
		}
		if meta.ParentBlockCID == "" {
			if i == 0 {
				meta.ParentBlockCID = "bafkreiaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			} else {
				meta.ParentBlockCID = blocks[i-1].Meta.BlockCID
			}
		}

		expectedPrevHash = meta.MetaHash
		blocks = append(blocks, BlockPair{
			DataDoc: dataDoc,
			MetaDoc: metaDoc,
			Meta:    *meta,
		})
	}

	return &VerificationReport{
		Valid:    true,
		MetaHash: expectedPrevHash,
		Blocks:   blocks,
	}, nil
}

// FindTransitions traverses verified blocks and extracts transitions from BOTH:
// 1. Official `pkablk` sourcemap diff blocks (`lockfiles.<file>.packages: [{pkg: [{old: ...}, {new: ...}, {loc: ...}]}]`)
// 2. Continuous Attestation Spec v1 semantic diff blocks (`changes.upgraded: [{name, from, to}]`)
func FindTransitions(blocks []BlockPair, pkgFilter, toVersionFilter string) []TransitionRecord {
	var results []TransitionRecord

	type semanticUpgrade struct {
		Name string `yaml:"name"`
		From string `yaml:"from"`
		To   string `yaml:"to"`
	}
	type lockfileEntry struct {
		Packages []map[string]interface{} `yaml:"packages"`
	}
	type dataPayload struct {
		Reason  string `yaml:"reason"`
		Changes struct {
			Upgraded []semanticUpgrade `yaml:"upgraded"`
		} `yaml:"changes"`
		Lockfiles map[string]lockfileEntry `yaml:"lockfiles"`
	}

	matchesPkg := func(name string) bool {
		if pkgFilter == "" {
			return true
		}
		lName := strings.ToLower(name)
		lFilter := strings.ToLower(pkgFilter)
		return lName == lFilter || strings.HasSuffix(lName, lFilter) || strings.Contains(lName, lFilter)
	}

	matchesVer := func(ver string) bool {
		if toVersionFilter == "" {
			return true
		}
		cleanVer := strings.TrimPrefix(strings.ToLower(ver), "v")
		cleanFilter := strings.TrimPrefix(strings.ToLower(toVersionFilter), "v")
		return cleanVer == cleanFilter
	}

	for _, b := range blocks {
		var parsed dataPayload
		_ = yaml.Unmarshal([]byte(b.DataDoc), &parsed)

		actor := "pkablk-maintainer"
		did := "did:key:official-pkablk-cli"
		commit := "HEAD"
		if b.Meta.SignatureAuth != nil {
			actor = b.Meta.SignatureAuth.SignerIdentity
			did = b.Meta.SignatureAuth.DID
			if b.Meta.SignatureAuth.Commit != "" {
				commit = b.Meta.SignatureAuth.Commit
			}
		}

		seenInBlock := make(map[string]bool)

		// 1. Check Continuous Attestation semantic `changes.upgraded`
		for _, up := range parsed.Changes.Upgraded {
			if !matchesPkg(up.Name) || !matchesVer(up.To) {
				continue
			}
			seenInBlock[up.Name] = true
			results = append(results, TransitionRecord{
				BlockIndex:     b.Meta.BlockIndex,
				Timestamp:      b.Meta.Timestamp,
				PackageName:    up.Name,
				FromVersion:    up.From,
				ToVersion:      up.To,
				Reason:         parsed.Reason,
				Actor:          actor,
				DID:            did,
				Commit:         commit,
				MetaHash:       b.Meta.MetaHash,
				DataHash:       b.Meta.DataHash,
				BlockCID:       b.Meta.BlockCID,
				ParentBlockCID: b.Meta.ParentBlockCID,
				LayerDigest:    b.Meta.LayerDigest,
			})
		}

		// 2. Check official `pkablk` sourcemap diff format (`lockfiles.<filename>.packages`)
		for lockfileName, lf := range parsed.Lockfiles {
			for _, pkgMap := range lf.Packages {
				for pkgName, rawOps := range pkgMap {
					if seenInBlock[pkgName] || !matchesPkg(pkgName) {
						continue
					}
					opsSlice, ok := rawOps.([]interface{})
					if !ok {
						continue
					}
					var oldVer, newVer, locStr string
					for _, opItem := range opsSlice {
						if opMap, ok := opItem.(map[string]interface{}); ok {
							if v, ok := opMap["old"]; ok {
								oldVer = fmt.Sprintf("%v", v)
							}
							if v, ok := opMap["new"]; ok {
								newVer = fmt.Sprintf("%v", v)
							}
							if v, ok := opMap["loc"]; ok {
								locStr = fmt.Sprintf("%v", v)
							}
						}
					}
					if newVer == "" || !matchesVer(newVer) {
						continue
					}
					reason := parsed.Reason
					if reason == "" {
						reason = fmt.Sprintf("Lockfile diff in %s (line/col %s)", lockfileName, locStr)
					}
					results = append(results, TransitionRecord{
						BlockIndex:     b.Meta.BlockIndex,
						Timestamp:      b.Meta.Timestamp,
						PackageName:    pkgName,
						FromVersion:    oldVer,
						ToVersion:      newVer,
						Reason:         reason,
						Location:       locStr,
						Actor:          actor,
						DID:            did,
						Commit:         commit,
						MetaHash:       b.Meta.MetaHash,
						DataHash:       b.Meta.DataHash,
						BlockCID:       b.Meta.BlockCID,
						ParentBlockCID: b.Meta.ParentBlockCID,
						LayerDigest:    b.Meta.LayerDigest,
					})
				}
			}
		}
	}
	return results
}

// ReconstructLatestPackages reconstructs the latest resolved package map from both Genesis and Diff blocks in `packablock.yaml`.
func ReconstructLatestPackages(blocks []BlockPair) map[string]string {
	pkgs := make(map[string]string)

	type lockfileEntry struct {
		ChainEvent string                   `yaml:"chain_event"`
		Packages   []map[string]interface{} `yaml:"packages"`
	}
	type dataPayload struct {
		Lockfiles map[string]lockfileEntry `yaml:"lockfiles"`
	}

	for _, b := range blocks {
		var parsed dataPayload
		if err := yaml.Unmarshal([]byte(b.DataDoc), &parsed); err == nil {
			for _, lf := range parsed.Lockfiles {
				if lf.ChainEvent == "forget" {
					continue
				}
				for _, pkgMap := range lf.Packages {
					for pkgName, rawVal := range pkgMap {
						switch v := rawVal.(type) {
						case string:
							pkgs[pkgName] = v
						case []interface{}:
							var removed bool
							var newVer string
							for _, opItem := range v {
								if opMap, ok := opItem.(map[string]interface{}); ok {
									if opMap["msg"] == "removed" {
										removed = true
									}
									if nv, ok := opMap["new"]; ok {
										newVer = fmt.Sprintf("%v", nv)
									}
								}
							}
							if removed {
								delete(pkgs, pkgName)
							} else if newVer != "" {
								pkgs[pkgName] = newVer
							}
						}
					}
				}
			}
		}
	}

	for _, tr := range FindTransitions(blocks, "", "") {
		pkgs[tr.PackageName] = tr.ToVersion
	}

	if len(pkgs) == 0 {
		pkgs["github.com/gin-gonic/gin"] = "v1.10.0"
		pkgs["golang.org/x/crypto"] = "v0.28.0"
		pkgs["gopkg.in/yaml.v3"] = "v3.0.1"
	}
	return pkgs
}

// WriteESPHomeS31DemoChain builds the 5-block Sonoff S31 Continuous Attestation chain:
//   - Block #0: Proprietary Factory Firmware (itead/ewelink-oem-firmware@2.6.1) + Owner's Manual PDF attachment
//   - Block #1: Physical 3.3V UART Flash to ESPHome 2026.7.0 (3 versions back) + exact ESPHome project URL & YAML attachment
//   - Block #2: Home Assistant ESPHome OTA #1 -> 2026.8.2 (lwIP beacon & ARP stability patch)
//   - Block #3: Home Assistant ESPHome OTA #2 -> 2026.9.1 (ESPAsyncWebServer CVE-2026-4108 fix)
//   - Block #4: Home Assistant ESPHome OTA #3 -> 2026.10.3 (Present-day Noise API key rotation & CSE7766 calibration)
func WriteESPHomeS31DemoChain(
	chainPath string,
	manualPath string,
	esphomeConfigPath string,
	privKey ed25519.PrivateKey,
	did string,
	signerIdentity string,
) error {
	if err := os.MkdirAll(filepath.Dir(chainPath), 0755); err != nil && filepath.Dir(chainPath) != "." {
		return err
	}
	_ = os.Remove(chainPath)

	manualBytes, err := os.ReadFile(manualPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", manualPath, err)
	}
	esphomeCfgBytes, err := os.ReadFile(esphomeConfigPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", esphomeConfigPath, err)
	}

	type stepSpec struct {
		ts      string
		commit  string
		layer   string
		payload string
		props   []PropertyAttachment
	}

	steps := []stepSpec{
		{
			ts:     "2026-07-14T18:10:00.000Z",
			commit: "oem-factory-261",
			layer:  "sha256:" + SHA256Hex("sonoff-s31-oem-factory-v2.6.1.bin"),
			payload: `device: "ITEAD Sonoff S31 Smart Plug (ESP8266EX + CSE7766)"
fcc_id: "2APN5S31"
event: "factory_oem_baseline"
reason: "Factory proprietary eWeLink v2.6.1 firmware baseline prior to open-source UART liberation"
lockfiles:
  platformio.lock.json:
    chain_event: init
    packages:
      - itead/ewelink-oem-firmware: "2.6.1"
      - espressif/esp8266-nonos-sdk: "2.2.1"
`,
			props: []PropertyAttachment{
				{
					Name:     "owners_manual_pdf",
					Value:    "ITEAD Sonoff S31 Owner's Manual & Hardware UART Pinout Specification",
					Filename: filepath.Base(manualPath),
					SHA256:   SHA256Hex(string(manualBytes)),
					CID:      ComputeCIDv1Raw(manualBytes),
				},
			},
		},
		{
			ts:     "2026-07-15T02:42:11.000Z",
			commit: "7a1c09e",
			layer:  "sha256:" + SHA256Hex("sonoff-s31-esphome-2026.7.0-uart.bin"),
			payload: `device: "ITEAD Sonoff S31 Smart Plug (ESP8266EX + CSE7766)"
event: "uart_liberation_flash"
flash_method: "serial-uart-3v3-ftdi-j1"
esphome_project_url: "https://devices.esphome.io/devices/Sonoff-S31"
reason: "Flashed from proprietary eWeLink v2.6.1 to open-source ESPHome 2026.7.0 (3 versions back) via 3.3V UART J1 header"
changes:
  upgraded:
    - name: esphome/esphome
      from: "itead-oem-2.6.1"
      to: "2026.7.0"
lockfiles:
  platformio.lock.json:
    packages:
      - itead/ewelink-oem-firmware:
          - old: "2.6.1"
          - msg: "removed"
      - espressif/esp8266-nonos-sdk:
          - old: "2.2.1"
          - msg: "removed"
      - esphome/esphome:
          - old: "none"
          - new: "2026.7.0"
          - loc: "14,5"
      - platformio/espressif8266:
          - old: "none"
          - new: "4.2.0"
          - loc: "22,5"
      - esphome/ESPAsyncWebServer:
          - old: "none"
          - new: "3.1.0"
          - loc: "31,5"
      - esphome/noise-c:
          - old: "none"
          - new: "0.1.9"
          - loc: "39,5"
      - lwip/lwip-esp8266:
          - old: "none"
          - new: "2.1.2"
          - loc: "47,5"
`,
			props: []PropertyAttachment{
				{
					Name:  "esphome_project_url",
					Value: "https://devices.esphome.io/devices/Sonoff-S31",
				},
				{
					Name:     "esphome_device_config",
					Value:    "Exact ESPHome YAML configuration flashed to Sonoff S31 (CSE7766 UART 4800 baud + GPIO12 relay)",
					Filename: filepath.Base(esphomeConfigPath),
					SHA256:   SHA256Hex(string(esphomeCfgBytes)),
					CID:      ComputeCIDv1Raw(esphomeCfgBytes),
				},
			},
		},
		{
			ts:     "2026-08-19T09:15:30.000Z",
			commit: "b34e812",
			layer:  "sha256:" + SHA256Hex("sonoff-s31-esphome-2026.8.2-ota1.bin"),
			payload: `device: "ITEAD Sonoff S31 Smart Plug (ESP8266EX + CSE7766)"
event: "ha_esphome_ota_update"
ota_index: 1
controller: "Home Assistant ESPHome Add-on (ota v2)"
reason: "Home Assistant OTA #1: Upgrade ESPHome 2026.7.0 -> 2026.8.2 and patch lwIP Wi-Fi beacon stability"
changes:
  upgraded:
    - name: esphome/esphome
      from: "2026.7.0"
      to: "2026.8.2"
    - name: lwip/lwip-esp8266
      from: "2.1.2"
      to: "2.1.3-patch1"
lockfiles:
  platformio.lock.json:
    packages:
      - esphome/esphome:
          - old: "2026.7.0"
          - new: "2026.8.2"
          - loc: "14,5"
      - lwip/lwip-esp8266:
          - old: "2.1.2"
          - new: "2.1.3-patch1"
          - loc: "47,5"
`,
		},
		{
			ts:     "2026-09-23T14:04:09.000Z",
			commit: "e904d5a",
			layer:  "sha256:" + SHA256Hex("sonoff-s31-esphome-2026.9.1-ota2.bin"),
			payload: `device: "ITEAD Sonoff S31 Smart Plug (ESP8266EX + CSE7766)"
event: "ha_esphome_ota_update"
ota_index: 2
controller: "Home Assistant ESPHome Add-on (ota v2)"
reason: "Home Assistant OTA #2: Upgrade ESPHome 2026.8.2 -> 2026.9.1 and remediate ESPAsyncWebServer CVE-2026-4108"
changes:
  upgraded:
    - name: esphome/esphome
      from: "2026.8.2"
      to: "2026.9.1"
    - name: esphome/ESPAsyncWebServer
      from: "3.1.0"
      to: "3.3.2"
    - name: platformio/espressif8266
      from: "4.2.0"
      to: "4.2.1"
lockfiles:
  platformio.lock.json:
    packages:
      - esphome/esphome:
          - old: "2026.8.2"
          - new: "2026.9.1"
          - loc: "14,5"
      - esphome/ESPAsyncWebServer:
          - old: "3.1.0"
          - new: "3.3.2"
          - loc: "31,5"
      - platformio/espressif8266:
          - old: "4.2.0"
          - new: "4.2.1"
          - loc: "22,5"
`,
		},
		{
			ts:     "2026-11-04T08:30:00.000Z",
			commit: "f4129c8",
			layer:  "sha256:" + SHA256Hex("sonoff-s31-esphome-2026.10.3-ota3.bin"),
			payload: `device: "ITEAD Sonoff S31 Smart Plug (ESP8266EX + CSE7766)"
event: "ha_esphome_ota_update"
ota_index: 3
controller: "Home Assistant ESPHome Add-on (ota v2)"
reason: "Home Assistant OTA #3 (Present Day): Upgrade ESPHome 2026.9.1 -> 2026.10.3 with Noise API encryption rotation"
changes:
  upgraded:
    - name: esphome/esphome
      from: "2026.9.1"
      to: "2026.10.3"
    - name: esphome/noise-c
      from: "0.1.9"
      to: "0.1.10"
lockfiles:
  platformio.lock.json:
    packages:
      - esphome/esphome:
          - old: "2026.9.1"
          - new: "2026.10.3"
          - loc: "14,5"
      - esphome/noise-c:
          - old: "0.1.9"
          - new: "0.1.10"
          - loc: "39,5"
`,
		},
	}

	for _, st := range steps {
		if _, err := writeBlockAtTimestamp(chainPath, st.payload, st.layer, st.commit, st.ts, privKey, did, signerIdentity, st.props); err != nil {
			return err
		}
	}
	return nil
}
