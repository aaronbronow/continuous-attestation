package dnslink

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// CloudflareDNSConfig configures the Cloudflare DNS API v4 provider for publishing `_dnslink.<label>.<domain>` TXT records.
type CloudflareDNSConfig struct {
	APIBaseURL string // Default: "https://api.cloudflare.com/client/v4"
	ZoneID     string // CF_ZONE_ID (optional if token has Zone:Read to auto-discover from domain)
	APIToken   string // CF_API_TOKEN or CLOUDFLARE_API_TOKEN
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
	token := strings.TrimSpace(os.Getenv("CF_API_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))
	}
	return CloudflareDNSConfig{
		APIBaseURL: strings.TrimSuffix(base, "/"),
		ZoneID:     strings.TrimSpace(os.Getenv("CF_ZONE_ID")),
		APIToken:   token,
	}
}

// ResolveZoneIDForDomain queries `GET /zones?name=<apex>` to find the Cloudflare Zone ID when CF_ZONE_ID is omitted.
func ResolveZoneIDForDomain(client *http.Client, apiBase, apiToken, domain string) (string, error) {
	parts := strings.Split(strings.TrimSuffix(strings.TrimSpace(domain), "."), ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("cannot infer registrable zone from domain %q", domain)
	}
	apex := strings.Join(parts[len(parts)-2:], ".")
	u := fmt.Sprintf("%s/zones?name=%s", apiBase, url.QueryEscape(apex))
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("Cloudflare zones lookup HTTP %d: %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Success bool `json:"success"`
		Result  []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Result) == 0 {
		return "", fmt.Errorf("no Cloudflare zone found matching %q", apex)
	}
	return parsed.Result[0].ID, nil
}

// PublishCloudflareDNSLink upserts the `_dnslink.<base32-digest>.<domain>` TXT record (`"dnslink=/ipfs/<CID>"`) via Cloudflare DNS API v4.
func PublishCloudflareDNSLink(cfg CloudflareDNSConfig, digest, domain, ipfsCID string) (string, string, string, error) {
	if cfg.APIToken == "" {
		return "", "", "", fmt.Errorf("CF_API_TOKEN (or CLOUDFLARE_API_TOKEN) environment variable is required for --provider cloudflare")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	zoneID := cfg.ZoneID
	if zoneID == "" {
		resolvedID, err := ResolveZoneIDForDomain(client, cfg.APIBaseURL, cfg.APIToken, domain)
		if err != nil {
			return "", "", "", fmt.Errorf("resolving CF_ZONE_ID for %s: %w", domain, err)
		}
		zoneID = resolvedID
	}

	fqdn, _, err := FormatDNSLinkFQDN(digest, domain)
	if err != nil {
		return "", "", "", err
	}
	txtValue := fmt.Sprintf("dnslink=/ipfs/%s", ipfsCID)

	// 1. Check if TXT record already exists
	listURL := fmt.Sprintf("%s/zones/%s/dns_records?type=TXT&name=%s", cfg.APIBaseURL, zoneID, fqdn)
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
	recordURL := fmt.Sprintf("%s/zones/%s/dns_records", cfg.APIBaseURL, zoneID)
	if existingID != "" {
		method = http.MethodPut
		recordURL = fmt.Sprintf("%s/zones/%s/dns_records/%s", cfg.APIBaseURL, zoneID, existingID)
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
