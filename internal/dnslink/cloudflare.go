package dnslink

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

// CloudflareDNSConfig configures the Cloudflare DNS API v4 provider for publishing `_dnslink.<label>.<domain>` TXT records.
type CloudflareDNSConfig struct {
	APIBaseURL string // Default: "https://api.cloudflare.com/client/v4"
	ZoneID     string // CF_ZONE_ID
	APIToken   string // CF_API_TOKEN
}

// LoadCloudflareDNSConfigFromEnv loads Cloudflare DNS settings from environment variables.
func LoadCloudflareDNSConfigFromEnv(apiBaseOverride string) CloudflareDNSConfig {
	base := apiBaseOverride
	if base == "" {
		base = os.Getenv("CF_API_BASE_URL")
	}
	if base == "" {
		base = "https://api.cloudflare.com/client/v4"
	}
	return CloudflareDNSConfig{
		APIBaseURL: strings.TrimSuffix(base, "/"),
		ZoneID:     os.Getenv("CF_ZONE_ID"),
		APIToken:   os.Getenv("CF_API_TOKEN"),
	}
}

// PublishCloudflareDNSLink upserts the `_dnslink.<base32-digest>.<domain>` TXT record (`"dnslink=/ipfs/<CID>"`) via Cloudflare DNS API v4.
func PublishCloudflareDNSLink(cfg CloudflareDNSConfig, digest, domain, ipfsCID string) (string, string, string, error) {
	if cfg.ZoneID == "" || cfg.APIToken == "" {
		return "", "", "", fmt.Errorf("CF_ZONE_ID and CF_API_TOKEN environment variables are required for --provider cloudflare")
	}

	fqdn, _, err := FormatDNSLinkFQDN(digest, domain)
	if err != nil {
		return "", "", "", err
	}
	txtValue := fmt.Sprintf("dnslink=/ipfs/%s", ipfsCID)

	client := &http.Client{Timeout: 10 * time.Second}

	// 1. Check if TXT record already exists
	listURL := fmt.Sprintf("%s/zones/%s/dns_records?type=TXT&name=%s", cfg.APIBaseURL, cfg.ZoneID, fqdn)
	listReq, err := http.NewRequest(http.MethodGet, listURL, nil)
	if err != nil {
		return "", "", "", err
	}
	listReq.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	listReq.Header.Set("Content-Type", "application/json")

	var existingID string
	if resp, err := client.Do(listReq); err == nil {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var listRes struct {
			Success bool `json:"success"`
			Result  []struct {
				ID string `json:"id"`
			} `json:"result"`
		}
		if json.Unmarshal(body, &listRes) == nil && len(listRes.Result) > 0 {
			existingID = listRes.Result[0].ID
		}
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"type":    "TXT",
		"name":    fqdn,
		"content": fmt.Sprintf("%q", txtValue),
		"ttl":     60,
		"comment": "Packablock pkaput DNSLink pointer",
	})

	method := http.MethodPost
	recordURL := fmt.Sprintf("%s/zones/%s/dns_records", cfg.APIBaseURL, cfg.ZoneID)
	if existingID != "" {
		method = http.MethodPut
		recordURL = fmt.Sprintf("%s/zones/%s/dns_records/%s", cfg.APIBaseURL, cfg.ZoneID, existingID)
	}

	req, err := http.NewRequest(method, recordURL, bytes.NewReader(payload))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("Cloudflare DNS %s failed: %w", method, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", "", "", fmt.Errorf("Cloudflare DNS API returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var upsertRes struct {
		Success bool `json:"success"`
		Result  struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	_ = json.Unmarshal(body, &upsertRes)
	return fqdn, txtValue, upsertRes.Result.ID, nil
}
