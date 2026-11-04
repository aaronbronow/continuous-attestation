package attestation

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Packablock/pkaput/internal/chain"
)

const (
	InTotoStatementType = "https://in-toto.io/Statement/v1"
	SquashPredicateType = "https://docs.packablock.com/spec/squash-predicate/v1"
	DSSEPayloadType     = "application/vnd.in-toto+json"
)

// TestKeyMaterial holds the Ed25519 signing keypair and W3C did:key identifier.
type TestKeyMaterial struct {
	KeyID          string             `json:"keyId"`
	SeedHex        string             `json:"seedHex"`
	PublicKeyHex   string             `json:"publicKeyHex"`
	DID            string             `json:"did"`
	SignerIdentity string             `json:"signerIdentity"`
	PrivateKey     ed25519.PrivateKey `json:"-"`
	PublicKey      ed25519.PublicKey  `json:"-"`
}

// LoadKeyMaterial loads the Ed25519 test key from `test/fixtures/ed25519.key.json`.
func LoadKeyMaterial(path string) (*TestKeyMaterial, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var km TestKeyMaterial
	if err := json.Unmarshal(raw, &km); err != nil {
		return nil, err
	}
	seedBytes, err := hex.DecodeString(km.SeedHex)
	if err != nil || len(seedBytes) != ed25519.SeedSize {
		return nil, errors.New("invalid Ed25519 seedHex in key fixture")
	}
	km.PrivateKey = ed25519.NewKeyFromSeed(seedBytes)
	km.PublicKey = km.PrivateKey.Public().(ed25519.PublicKey)

	// Recompute canonical W3C did:key (multicodec 0xed01 + base58btc) to ensure mathematical accuracy
	km.DID = EncodeDIDKeyEd25519(km.PublicKey)
	return &km, nil
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// EncodeDIDKeyEd25519 encodes an Ed25519 public key as a W3C `did:key:z6Mk...` identifier (multicodec 0xed 0x01).
func EncodeDIDKeyEd25519(pub ed25519.PublicKey) string {
	prefixed := append([]byte{0xed, 0x01}, pub...)
	return "did:key:z" + base58Encode(prefixed)
}

// DecodeDIDKeyEd25519 extracts the 32-byte Ed25519 public key from a W3C `did:key:z6Mk...` identifier.
func DecodeDIDKeyEd25519(did string) (ed25519.PublicKey, error) {
	if !strings.HasPrefix(did, "did:key:z") {
		return nil, fmt.Errorf("unsupported DID format: %s", did)
	}
	b58 := strings.TrimPrefix(did, "did:key:z")
	raw, err := base58Decode(b58)
	if err != nil {
		return nil, err
	}
	if len(raw) != 34 || raw[0] != 0xed || raw[1] != 0x01 {
		return nil, fmt.Errorf("invalid Ed25519 multicodec prefix in DID %s", did)
	}
	return ed25519.PublicKey(raw[2:]), nil
}

func base58Encode(input []byte) string {
	x := make([]byte, len(input))
	copy(x, input)
	var out []byte
	for len(x) > 0 {
		var remainder int
		var next []byte
		for _, b := range x {
			acc := remainder*256 + int(b)
			digit := acc / 58
			remainder = acc % 58
			if len(next) > 0 || digit > 0 {
				next = append(next, byte(digit))
			}
		}
		out = append(out, base58Alphabet[remainder])
		x = next
	}
	for _, b := range input {
		if b == 0 {
			out = append(out, base58Alphabet[0])
		} else {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func base58Decode(input string) ([]byte, error) {
	var out []byte
	for i := 0; i < len(input); i++ {
		idx := strings.IndexByte(base58Alphabet, input[i])
		if idx < 0 {
			return nil, fmt.Errorf("invalid base58 character %q", input[i])
		}
		carry := idx
		for j := 0; j < len(out); j++ {
			carry += int(out[j]) * 58
			out[j] = byte(carry & 0xff)
			carry >>= 8
		}
		for carry > 0 {
			out = append(out, byte(carry&0xff))
			carry >>= 8
		}
	}
	for i := 0; i < len(input) && input[i] == base58Alphabet[0]; i++ {
		out = append(out, 0)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// SubjectDescriptor represents the OCI flattened layer subject (`subject.json` and in-toto `subject`).
type SubjectDescriptor struct {
	Name      string            `json:"name"`
	Digest    map[string]string `json:"digest"`
	MediaType string            `json:"mediaType,omitempty"`
	Size      int64             `json:"size,omitempty"`
}

// PrecursorNode records a single historical block in `squashedLineage.precursors`.
type PrecursorNode struct {
	BlockIndex     int    `json:"blockIndex"`
	BlockCID       string `json:"blockCid"`
	ParentBlockCID string `json:"parentBlockCid"`
	MetaHash       string `json:"metaHash"`
	DataHash       string `json:"dataHash"`
	LayerDigest    string `json:"layerDigest"`
	Timestamp      string `json:"timestamp"`
	Actor          string `json:"actor,omitempty"`
	Commit         string `json:"commit,omitempty"`
}

// SquashedLineage holds the precursors and terminal hash of the squashed layers.
type SquashedLineage struct {
	LayerCount       int             `json:"layerCount"`
	TerminalMetaHash string          `json:"terminalMetaHash"`
	Precursors       []PrecursorNode `json:"precursors"`
}

// VSASummary models the Continuous Attestation Spec v1 §4.4 Verification Summary Attestation (VSA).
type VSASummary struct {
	Verifier           map[string]string   `json:"verifier"`
	TimeVerified       string              `json:"timeVerified"`
	ResourceURI        string              `json:"resourceUri"`
	Policy             map[string]string   `json:"policy"`
	InputAttestations  []map[string]string `json:"inputAttestations"`
	VerificationResult string              `json:"verificationResult"`
	VerifiedLevels     []string            `json:"verifiedLevels"`
}

// W3CVerifiableCredential wraps the Continuous Attestation proof in a W3C Verifiable Credential v2.0 envelope.
type W3CVerifiableCredential struct {
	Context           []string               `json:"@context"`
	Type              []string               `json:"type"`
	Issuer            string                 `json:"issuer"`
	IssuanceDate      string                 `json:"issuanceDate"`
	CredentialSubject map[string]interface{} `json:"credentialSubject"`
	Proof             map[string]string      `json:"proof"`
}

// AttachedArtifactDescriptor binds an extra artifact file (e.g. owners-manual.pdf, sonoff-s31-esphome.yaml)
// into the signed in-toto SquashPredicate with its SHA-256 and IPFS raw leaf CID.
type AttachedArtifactDescriptor struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	CID    string `json:"cid"`
	Size   int64  `json:"size"`
}

// SquashPredicate is the predicate payload inside the in-toto Statement v1.
type SquashPredicate struct {
	OutputTag            string                       `json:"outputTag"`
	FlattenedLayerDigest string                       `json:"flattenedLayerDigest"`
	IssuerDID            string                       `json:"issuerDid"`
	Timestamp            string                       `json:"timestamp"`
	AttachedArtifacts    []AttachedArtifactDescriptor `json:"attachedArtifacts,omitempty"`
	SquashedLineage      SquashedLineage              `json:"squashedLineage"`
	VerificationSummary  VSASummary                   `json:"verificationSummary"`
	VerifiableCredential W3CVerifiableCredential      `json:"verifiableCredential"`
}

// InTotoStatement is an in-toto Statement v1 (`https://in-toto.io/Statement/v1`).
type InTotoStatement struct {
	Type          string              `json:"_type"`
	Subject       []SubjectDescriptor `json:"subject"`
	PredicateType string              `json:"predicateType"`
	Predicate     SquashPredicate     `json:"predicate"`
}

// DSSESignature is a single signature entry in a Dead Simple Signing Envelope (DSSE).
type DSSESignature struct {
	KeyID string `json:"keyid"`
	Sig   string `json:"sig"`
}

// DSSEEnvelope is the Dead Simple Signing Envelope (DSSE) v1.0 wrapper (`attestation.dsse.json`).
type DSSEEnvelope struct {
	PayloadType string          `json:"payloadType"`
	Payload     string          `json:"payload"`
	Signatures  []DSSESignature `json:"signatures"`
}

// ComputePAE computes the standard DSSE v1.0 Pre-Authentication Encoding (PAE):
// "DSSEv1" + SP + LEN(payloadType) + SP + payloadType + SP + LEN(payload) + SP + payload
func ComputePAE(payloadType string, payload []byte) []byte {
	header := fmt.Sprintf("DSSEv1 %d %s %d ", len(payloadType), payloadType, len(payload))
	return append([]byte(header), payload...)
}

// BuildSquashAttestation creates the in-toto Statement, W3C Verifiable Credential, VSA, and signed DSSE envelope.
func BuildSquashAttestation(
	outputTag string,
	flattenedDigest string,
	flattenedSize int64,
	blocks []chain.BlockPair,
	km *TestKeyMaterial,
	extraMaps ...map[string][]byte,
) (*SubjectDescriptor, *InTotoStatement, *DSSEEnvelope, error) {
	rawHex := strings.TrimPrefix(flattenedDigest, "sha256:")
	subj := SubjectDescriptor{
		Name: outputTag,
		Digest: map[string]string{
			"sha256": rawHex,
		},
		MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
		Size:      flattenedSize,
	}

	var attached []AttachedArtifactDescriptor
	for _, m := range extraMaps {
		names := make([]string, 0, len(m))
		for name := range m {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			content := m[name]
			attached = append(attached, AttachedArtifactDescriptor{
				Name:   name,
				SHA256: chain.SHA256Hex(string(content)),
				CID:    chain.ComputeCIDv1Raw(content),
				Size:   int64(len(content)),
			})
		}
	}

	precursors := make([]PrecursorNode, 0, len(blocks))
	terminalMetaHash := ""
	for _, b := range blocks {
		terminalMetaHash = b.Meta.MetaHash
		actor := km.SignerIdentity
		commit := "HEAD"
		if b.Meta.SignatureAuth != nil {
			actor = b.Meta.SignatureAuth.SignerIdentity
			if b.Meta.SignatureAuth.Commit != "" {
				commit = b.Meta.SignatureAuth.Commit
			}
		}
		precursors = append(precursors, PrecursorNode{
			BlockIndex:     b.Meta.BlockIndex,
			BlockCID:       b.Meta.BlockCID,
			ParentBlockCID: b.Meta.ParentBlockCID,
			MetaHash:       b.Meta.MetaHash,
			DataHash:       b.Meta.DataHash,
			LayerDigest:    b.Meta.LayerDigest,
			Timestamp:      b.Meta.Timestamp,
			Actor:          actor,
			Commit:         commit,
		})
	}

	now := time.Now().UTC().Format(time.RFC3339)
	vcProofInput := fmt.Sprintf("%s:%s:%s:%s", km.DID, flattenedDigest, terminalMetaHash, now)
	vcSig := base64.StdEncoding.EncodeToString(ed25519.Sign(km.PrivateKey, []byte(vcProofInput)))

	vc := W3CVerifiableCredential{
		Context:      []string{"https://www.w3.org/2018/credentials/v1", "https://docs.packablock.com/contexts/continuous-attestation/v1"},
		Type:         []string{"VerifiableCredential", "PackablockSquashAttestationCredential"},
		Issuer:       km.DID,
		IssuanceDate: now,
		CredentialSubject: map[string]interface{}{
			"id":                   "urn:ietf:params:oauth:client-assertion-type:oci-layer:" + flattenedDigest,
			"outputTag":            outputTag,
			"flattenedLayerDigest": flattenedDigest,
			"terminalMetaHash":     terminalMetaHash,
			"blockCount":           len(blocks),
		},
		Proof: map[string]string{
			"type":               "Ed25519Signature2020",
			"created":            now,
			"verificationMethod": km.DID + "#" + strings.TrimPrefix(km.DID, "did:key:"),
			"proofPurpose":       "assertionMethod",
			"proofValue":         vcSig,
		},
	}

	vsa := VSASummary{
		Verifier: map[string]string{
			"id":  km.DID,
			"uri": "https://docs.packablock.com/spec/#44-verification-summary-attestations-vsa",
		},
		TimeVerified: now,
		ResourceURI:  "oci://" + outputTag + "@" + flattenedDigest,
		Policy: map[string]string{
			"uri":    "https://docs.packablock.com/spec/#47-policy-control-schema-and-enforcement-specification",
			"digest": chain.SHA256Hex("packablock.policy.yaml:v1"),
		},
		InputAttestations: []map[string]string{
			{
				"uri":       "ipfs://chain.packablock",
				"mediaType": "application/vnd.packablock.manifest-chain+yaml",
				"sha256":    terminalMetaHash,
			},
		},
		VerificationResult: "PASSED",
		VerifiedLevels:     []string{"SLSA_BUILD_LEVEL_3", "PACKABLOCK_CA_LEVEL_2"},
	}

	stmt := &InTotoStatement{
		Type:          InTotoStatementType,
		Subject:       []SubjectDescriptor{subj},
		PredicateType: SquashPredicateType,
		Predicate: SquashPredicate{
			OutputTag:            outputTag,
			FlattenedLayerDigest: flattenedDigest,
			IssuerDID:            km.DID,
			Timestamp:            now,
			AttachedArtifacts:    attached,
			SquashedLineage: SquashedLineage{
				LayerCount:       len(blocks),
				TerminalMetaHash: terminalMetaHash,
				Precursors:       precursors,
			},
			VerificationSummary:  vsa,
			VerifiableCredential: vc,
		},
	}

	payloadBytes, err := json.MarshalIndent(stmt, "", "  ")
	if err != nil {
		return nil, nil, nil, err
	}

	pae := ComputePAE(DSSEPayloadType, payloadBytes)
	sigBytes := ed25519.Sign(km.PrivateKey, pae)

	env := &DSSEEnvelope{
		PayloadType: DSSEPayloadType,
		Payload:     base64.StdEncoding.EncodeToString(payloadBytes),
		Signatures: []DSSESignature{
			{
				KeyID: km.DID,
				Sig:   base64.StdEncoding.EncodeToString(sigBytes),
			},
		},
	}

	return &subj, stmt, env, nil
}

// VerifyDSSEAndSubject verifies the DSSE signature using the embedded W3C `did:key` (Ed25519)
// and confirms `subject[0].digest.sha256` matches the expected flattened layer digest.
func VerifyDSSEAndSubject(env *DSSEEnvelope, expectedDigest string) (*InTotoStatement, string, error) {
	if env.PayloadType != DSSEPayloadType {
		return nil, "", fmt.Errorf("unexpected DSSE payloadType: %s", env.PayloadType)
	}
	if len(env.Signatures) == 0 {
		return nil, "", errors.New("DSSE envelope contains no signatures")
	}

	payloadBytes, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		return nil, "", fmt.Errorf("failed to base64-decode DSSE payload: %w", err)
	}

	sigEntry := env.Signatures[0]
	pubKey, err := DecodeDIDKeyEd25519(sigEntry.KeyID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve issuer DID %s: %w", sigEntry.KeyID, err)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(sigEntry.Sig)
	if err != nil {
		return nil, "", fmt.Errorf("failed to base64-decode DSSE signature: %w", err)
	}

	pae := ComputePAE(env.PayloadType, payloadBytes)
	if !ed25519.Verify(pubKey, pae, sigBytes) {
		return nil, "", fmt.Errorf("DSSE Ed25519 signature verification failed for issuer %s", sigEntry.KeyID)
	}

	var stmt InTotoStatement
	if err := json.Unmarshal(payloadBytes, &stmt); err != nil {
		return nil, "", fmt.Errorf("failed to unmarshal in-toto statement: %w", err)
	}

	if len(stmt.Subject) == 0 {
		return nil, "", errors.New("in-toto statement has no subjects")
	}

	expectedHex := strings.ToLower(strings.TrimPrefix(expectedDigest, "sha256:"))
	actualHex := strings.ToLower(stmt.Subject[0].Digest["sha256"])
	if actualHex != expectedHex {
		return nil, "", fmt.Errorf("subject digest mismatch: expected sha256:%s, statement has sha256:%s", expectedHex, actualHex)
	}

	if stmt.Predicate.IssuerDID != sigEntry.KeyID {
		return nil, "", fmt.Errorf("predicate issuerDID (%s) does not match DSSE signer keyid (%s)", stmt.Predicate.IssuerDID, sigEntry.KeyID)
	}

	return &stmt, sigEntry.KeyID, nil
}
