package chain_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Packablock/pkaput/internal/attestation"
	"github.com/Packablock/pkaput/internal/chain"
	"github.com/Packablock/pkaput/internal/dnslink"
	"github.com/Packablock/pkaput/internal/ipfs"
	"github.com/Packablock/pkaput/internal/oci"
	"github.com/Packablock/pkaput/internal/policy"
)

func TestOfficialPackablockGenesisHashCompatibility(t *testing.T) {
	officialYAML := `source: "bun.lockb"
event: "dependencies_baseline"
packages:
  @types/bun: "1.3.14"
  @types/node: "25.9.1"
  bun-types: "1.3.14"
  commander: "15.0.0"
  typescript: "5.9.3"
  undici-types: "7.24.6"
  yaml: "2.9.0"
---
$yaml-chain-meta:
  version: 1.0.0
  block_index: 0
  timestamp: 2026-05-31T03:54:17.824Z
  hashing_strategy: raw
  data_hash: c826814ca69b9cc95a3f1292db26ed1bd7fa52f344b1716a13d229fd402e031d
  prev_meta_hash: "0000000000000000000000000000000000000000000000000000000000000000"
  meta_hash: 94d9d2bc48a74357e0c8337621ffc1df97854ee4b9d73e8df198c88dac366111
`
	report, err := chain.VerifyChainContent(officialYAML, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !report.Valid {
		t.Fatalf("expected official packablock.yaml to verify, failed: %s (expected=%v, actual=%v)", report.Reason, report.Expected, report.Actual)
	}
	if report.MetaHash != "94d9d2bc48a74357e0c8337621ffc1df97854ee4b9d73e8df198c88dac366111" {
		t.Fatalf("unexpected terminal meta_hash: %s", report.MetaHash)
	}
}

func TestOfficialPkablkSourcemapDiffTraversal(t *testing.T) {
	// Simulates a 2-block chain produced by official `pkablk init` + `pkablk append`
	b0Data := `lockfiles:
  package-lock.v1.json:
    packages:
      - github.com/gin-gonic/gin: 1.9.1
      - golang.org/x/crypto: 0.28.0
`
	b0Hash := chain.SHA256Hex(b0Data[:len(b0Data)-1])
	b0Meta := chain.ChainMeta{
		Version:         "1.0.0",
		BlockIndex:      0,
		Timestamp:       "2026-11-04T06:00:00.000Z",
		HashingStrategy: "raw",
		DataHash:        b0Hash,
		PrevMetaHash:    chain.GenesisPrevHash,
	}
	b0Meta.MetaHash, _ = chain.DeterministicMetaHash(b0Meta)
	b0MetaYAML, _ := chain.FormatMetaYAML(b0Meta)

	b1Data := `lockfiles:
  package-lock.v1.json:
    packages:
      - github.com/gin-gonic/gin:
          - old: 1.9.1
          - new: 1.10.0
          - loc: 16,18
`
	b1Hash := chain.SHA256Hex(b1Data[:len(b1Data)-1])
	b1Meta := chain.ChainMeta{
		Version:         "1.0.0",
		BlockIndex:      1,
		Timestamp:       "2026-11-04T06:05:00.000Z",
		HashingStrategy: "raw",
		DataHash:        b1Hash,
		PrevMetaHash:    b0Meta.MetaHash,
	}
	b1Meta.MetaHash, _ = chain.DeterministicMetaHash(b1Meta)
	b1MetaYAML, _ := chain.FormatMetaYAML(b1Meta)

	stream := b0Data + "---\n" + b0MetaYAML + "---\n" + b1Data + "---\n" + b1MetaYAML
	rep, err := chain.VerifyChainContent(stream, nil)
	if err != nil || !rep.Valid {
		t.Fatalf("failed to verify official sourcemap diff stream: %v / %+v", err, rep)
	}

	transitions := chain.FindTransitions(rep.Blocks, "github.com/gin-gonic/gin", "1.10.0")
	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition from official sourcemap diff, got %d", len(transitions))
	}
	if transitions[0].FromVersion != "1.9.1" || transitions[0].ToVersion != "1.10.0" || transitions[0].BlockIndex != 1 {
		t.Fatalf("unexpected transition details: %+v", transitions[0])
	}

	pkgs := chain.ReconstructLatestPackages(rep.Blocks)
	if pkgs["github.com/gin-gonic/gin"] != "1.10.0" {
		t.Fatalf("expected reconstructed version 1.10.0, got %s", pkgs["github.com/gin-gonic/gin"])
	}
}

func TestEdgeR2WorkerAndCloudflareDNSMocks(t *testing.T) {
	storedFiles := make(map[string][]byte)
	workerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			if r.Header.Get("Authorization") != "Bearer test-worker-secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			storedFiles[r.URL.Path] = []byte("ok")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"stored":true}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer workerSrv.Close()

	tmpDir := t.TempDir()
	carPath := filepath.Join(tmpDir, "bundle.car")
	bundle := &ipfs.BundleFiles{
		SubjectJSON:     []byte(`{"name":"secure-lib:v1.0.0"}`),
		ChainPackablock: []byte(`chain`),
		SBOMCDXJSON:     []byte(`sbom`),
		AttestationDSSE: []byte(`dsse`),
	}
	cid, _, err := ipfs.WriteCARv1(carPath, bundle)
	if err != nil {
		t.Fatalf("WriteCARv1 failed: %v", err)
	}

	ep, err := ipfs.PutBundleToR2Worker(ipfs.EdgeStoreConfig{
		WorkerEndpoint: workerSrv.URL,
		WorkerPutToken: "test-worker-secret",
	}, cid, carPath, bundle)
	if err != nil {
		t.Fatalf("PutBundleToR2Worker failed: %v", err)
	}
	if ep == "" || len(storedFiles) < 5 {
		t.Fatalf("expected 4 bundle files + 1 CAR file uploaded to R2 Worker, got %d", len(storedFiles))
	}

	// Mock Cloudflare DNS API v4 server
	cfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"result":{"id":"cf-rec-12345"}}`))
	}))
	defer cfSrv.Close()

	digest := "sha256:890a0ccf7e1cd80cb2587612d58617a53b1cad1d7241c5d4707ad67bcaa3f404"
	fqdn, txtVal, recID, err := dnslink.PublishCloudflareDNSLink(dnslink.CloudflareDNSConfig{
		APIBaseURL: cfSrv.URL,
		ZoneID:     "zone-abc",
		APIToken:   "token-xyz",
	}, digest, "layers.packablock.com", cid)
	if err != nil {
		t.Fatalf("PublishCloudflareDNSLink failed: %v", err)
	}
	if recID != "cf-rec-12345" || fqdn == "" || txtVal != "dnslink=/ipfs/"+cid {
		t.Fatalf("unexpected Cloudflare DNS response: fqdn=%s txt=%s id=%s", fqdn, txtVal, recID)
	}
}

func TestEndToEndLifecycleUnit(t *testing.T) {
	tmpDir := t.TempDir()
	chainPath := filepath.Join(tmpDir, "packablock.yaml")
	layersDir := filepath.Join(tmpDir, "layers")
	carPath := filepath.Join(tmpDir, "bundle.car")
	zonePath := filepath.Join(tmpDir, "db.layers.example.internal")

	km, err := attestation.LoadKeyMaterial(filepath.Join("..", "..", "test", "fixtures", "ed25519.key.json"))
	if err != nil {
		t.Fatalf("failed to load test key: %v", err)
	}

	layer1, err := oci.GenerateIntermediateLayer(layersDir, 1, "github.com/gin-gonic/gin", "v1.10.0", "CRA update")
	if err != nil {
		t.Fatalf("GenerateIntermediateLayer failed: %v", err)
	}

	meta, err := chain.InitOrAppendStep(
		chainPath,
		"github.com/gin-gonic/gin",
		"v1.9.1",
		"v1.10.0",
		"CRA update",
		layer1.Digest,
		"deadbeef",
		km.PrivateKey,
		km.DID,
		km.SignerIdentity,
	)
	if err != nil {
		t.Fatalf("InitOrAppendStep failed: %v", err)
	}
	if meta.BlockIndex != 1 {
		t.Fatalf("expected BlockIndex 1, got %d", meta.BlockIndex)
	}

	rep, err := chain.VerifyChain(chainPath, km.PublicKey)
	if err != nil || !rep.Valid {
		t.Fatalf("VerifyChain failed: %v / %+v", err, rep)
	}

	pkgs := chain.ReconstructLatestPackages(rep.Blocks)
	flattened, _, err := oci.SquashLayers(layersDir, 3, "secure-lib:v1.0.0", pkgs)
	if err != nil {
		t.Fatalf("SquashLayers failed: %v", err)
	}

	sbomBytes, err := oci.GenerateCycloneDXSBOM("secure-lib:v1.0.0", flattened.Digest, pkgs)
	if err != nil {
		t.Fatalf("GenerateCycloneDXSBOM failed: %v", err)
	}

	subj, _, dsseEnv, err := attestation.BuildSquashAttestation("secure-lib:v1.0.0", flattened.Digest, flattened.Size, rep.Blocks, km)
	if err != nil {
		t.Fatalf("BuildSquashAttestation failed: %v", err)
	}

	stmt, issuerDID, err := attestation.VerifyDSSEAndSubject(dsseEnv, flattened.Digest)
	if err != nil {
		t.Fatalf("VerifyDSSEAndSubject failed: %v", err)
	}
	if issuerDID != km.DID {
		t.Fatalf("expected issuer %s, got %s", km.DID, issuerDID)
	}
	if len(stmt.Predicate.SquashedLineage.Precursors) != 2 {
		t.Fatalf("expected 2 precursor blocks in squashedLineage, got %d", len(stmt.Predicate.SquashedLineage.Precursors))
	}

	chainRaw, _ := os.ReadFile(chainPath)
	bundle := &ipfs.BundleFiles{
		SubjectJSON:     []byte(`{"name":"` + subj.Name + `"}`),
		ChainPackablock: chainRaw,
		SBOMCDXJSON:     sbomBytes,
		AttestationDSSE: []byte(`{}`),
	}
	carCID, _, err := ipfs.WriteCARv1(carPath, bundle)
	if err != nil || carCID == "" {
		t.Fatalf("WriteCARv1 failed: %v", err)
	}

	fqdn, txtVal, err := dnslink.UpdateCoreDNSZoneFile(zonePath, flattened.Digest, "layers.example.internal", carCID)
	if err != nil {
		t.Fatalf("UpdateCoreDNSZoneFile failed: %v", err)
	}
	if fqdn == "" || txtVal != "dnslink=/ipfs/"+carCID {
		t.Fatalf("unexpected DNSLink output: %s -> %s", fqdn, txtVal)
	}

	polDoc, err := policy.LoadPolicy(filepath.Join("..", "..", "test", "fixtures", "packablock.policy.yaml"))
	if err != nil {
		t.Fatalf("LoadPolicy failed: %v", err)
	}
	polRes, err := policy.EvaluateSBOMAndChain(sbomBytes, string(chainRaw), polDoc, false)
	if err != nil || !polRes.Passed {
		t.Fatalf("expected policy evaluation to pass: %v / %+v", err, polRes)
	}
}
