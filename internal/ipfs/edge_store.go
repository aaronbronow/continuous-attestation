package ipfs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// EdgeStoreConfig configures the Cloudflare R2 / Worker Edge Gateway and Storacha CARv1 pinning backends.
type EdgeStoreConfig struct {
	WorkerEndpoint string // e.g. "https://ipfs.packablock.com"
	WorkerPutToken string // Bearer token for PUT /ipfs/<CID>/<filename> and PUT /car/<CID>.car
	StorachaURL    string // e.g. "https://up.storacha.network/car" or Pinata/W3UP endpoint
	StorachaToken  string // Bearer token / UCAN for CARv1 upload
}

// LoadEdgeStoreConfigFromEnv reads Edge storage credentials from environment variables.
func LoadEdgeStoreConfigFromEnv(workerURLOverride string) EdgeStoreConfig {
	workerURL := workerURLOverride
	if workerURL == "" {
		workerURL = os.Getenv("PKAPUT_WORKER_URL")
	}
	if workerURL == "" {
		workerURL = "https://ipfs.packablock.com"
	}
	storachaURL := os.Getenv("PKAPUT_STORACHA_URL")
	if storachaURL == "" {
		storachaURL = "https://api.pinata.cloud/pinning/pinFileToIPFS"
	}
	return EdgeStoreConfig{
		WorkerEndpoint: strings.TrimSuffix(workerURL, "/"),
		WorkerPutToken: os.Getenv("PKAPUT_WORKER_TOKEN"),
		StorachaURL:    storachaURL,
		StorachaToken:  os.Getenv("PKAPUT_STORACHA_TOKEN"),
	}
}

// PutBundleToR2Worker uploads each file in the UnixFS bundle (`PUT /ipfs/<rootCID>/<filename>`)
// plus the raw CARv1 archive (`PUT /car/<rootCID>.car`) to the Cloudflare R2 Trustless Gateway Worker.
func PutBundleToR2Worker(cfg EdgeStoreConfig, rootCID, carPath string, bundle *BundleFiles) (string, error) {
	if cfg.WorkerPutToken == "" {
		return "", fmt.Errorf("PKAPUT_WORKER_TOKEN is not set (required for --storage r2 against %s)", cfg.WorkerEndpoint)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	files := bundle.ToMap()

	for name, content := range files {
		putURL := fmt.Sprintf("%s/ipfs/%s/%s", cfg.WorkerEndpoint, rootCID, name)
		req, err := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(content))
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+cfg.WorkerPutToken)
		req.Header.Set("Content-Type", detectMediaType(name))
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("R2 Worker PUT %s failed: %w", putURL, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return "", fmt.Errorf("R2 Worker PUT %s returned HTTP %d: %s", putURL, resp.StatusCode, string(body))
		}
	}

	if carBytes, err := os.ReadFile(carPath); err == nil {
		carURL := fmt.Sprintf("%s/car/%s.car", cfg.WorkerEndpoint, rootCID)
		req, err := http.NewRequest(http.MethodPut, carURL, bytes.NewReader(carBytes))
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+cfg.WorkerPutToken)
			req.Header.Set("Content-Type", "application/vnd.ipld.car")
			if resp, err := client.Do(req); err == nil {
				resp.Body.Close()
			}
		}
	}

	return fmt.Sprintf("%s/ipfs/%s/", cfg.WorkerEndpoint, rootCID), nil
}

// PinCARToStoracha uploads the self-contained CARv1 archive (`bin/release-bundle.car`) to a CAR pinning service (Storacha / W3UP / Pinata).
func PinCARToStoracha(cfg EdgeStoreConfig, rootCID, carPath string) (string, error) {
	if cfg.StorachaToken == "" {
		return "", fmt.Errorf("PKAPUT_STORACHA_TOKEN is not set (required for --storage storacha)")
	}
	carBytes, err := os.ReadFile(carPath)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest(http.MethodPost, cfg.StorachaURL, bytes.NewReader(carBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.StorachaToken)
	req.Header.Set("Content-Type", "application/vnd.ipld.car")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Storacha CAR upload failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("Storacha CAR upload returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var parsed map[string]interface{}
	_ = json.Unmarshal(body, &parsed)
	return fmt.Sprintf("https://w3s.link/ipfs/%s/", rootCID), nil
}

func detectMediaType(filename string) string {
	switch {
	case filename == "chain.packablock" || strings.HasSuffix(filename, ".yaml"):
		return "application/vnd.packablock.manifest-chain+yaml; charset=utf-8"
	case filename == "attestation.dsse.json":
		return "application/vnd.in-toto+json"
	case filename == "sbom.cdx.json":
		return "application/vnd.cyclonedx+json"
	case strings.HasSuffix(filename, ".json"):
		return "application/json"
	case strings.HasSuffix(filename, ".pdf"):
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
