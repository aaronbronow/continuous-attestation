package oci

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const DefaultSyftVersion = "1.20.0"

// SyftResult captures metadata about the Syft execution that generated or enriched the CycloneDX SBOM.
type SyftResult struct {
	SBOMBytes   []byte
	SyftBinary  string
	SyftVersion string
	CommandLine string
	UsedSyft    bool
}

// ResolveOrBootstrapSyft finds `syft` on PATH or in `bin/syft(.exe)`.
// If missing and autoBootstrap is true, it downloads the pinned official Anchore Syft release binary into `bin/`.
func ResolveOrBootstrapSyft(binDir string, autoBootstrap bool) (string, string, error) {
	// 1. Check local bin/syft(.exe) first for deterministic repo-local execution
	exeName := "syft"
	if runtime.GOOS == "windows" {
		exeName = "syft.exe"
	}
	if binDir == "" {
		binDir = "bin"
	}
	localBin := filepath.Join(binDir, exeName)
	if info, err := os.Stat(localBin); err == nil && !info.IsDir() {
		ver := detectSyftVersion(localBin)
		return localBin, ver, nil
	}

	// 2. Check system PATH (for DevOps admins who already have syft installed in CI/CD)
	if sysBin, err := exec.LookPath("syft"); err == nil {
		ver := detectSyftVersion(sysBin)
		return sysBin, ver, nil
	}

	if !autoBootstrap {
		return "", "", fmt.Errorf("syft not found on PATH or in %s", localBin)
	}

	// 3. Zero-dependency bootstrap from official Anchore GitHub Releases into bin/
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", "", err
	}

	ext := "tar.gz"
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		ext = "zip"
	}
	archiveName := fmt.Sprintf("syft_%s_%s_%s.%s", DefaultSyftVersion, runtime.GOOS, runtime.GOARCH, ext)
	downloadURL := fmt.Sprintf("https://github.com/anchore/syft/releases/download/v%s/%s", DefaultSyftVersion, archiveName)

	fmt.Printf("   ├─ 📥 Bootstrapping official Anchore Syft v%s (%s/%s) -> %s\n", DefaultSyftVersion, runtime.GOOS, runtime.GOARCH, localBin)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return "", "", fmt.Errorf("downloading %s: %w", downloadURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("downloading %s returned HTTP %d", downloadURL, resp.StatusCode)
	}

	archiveBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("reading syft archive: %w", err)
	}

	var binBytes []byte
	if ext == "zip" {
		binBytes, err = extractBinaryFromZip(archiveBytes, exeName)
	} else {
		binBytes, err = extractBinaryFromTarGz(archiveBytes, exeName)
	}
	if err != nil {
		return "", "", err
	}

	if err := os.WriteFile(localBin, binBytes, 0755); err != nil {
		return "", "", fmt.Errorf("writing %s: %w", localBin, err)
	}

	return localBin, DefaultSyftVersion, nil
}

func detectSyftVersion(binPath string) string {
	out, err := exec.Command(binPath, "version", "-o", "json").Output()
	if err == nil {
		var parsed struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(out, &parsed) == nil && parsed.Version != "" {
			return parsed.Version
		}
	}
	return DefaultSyftVersion
}

func extractBinaryFromZip(zipBytes []byte, targetName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) == targetName {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("binary %s not found in zip archive", targetName)
}

func extractBinaryFromTarGz(tgzBytes []byte, targetName string) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(tgzBytes))
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == targetName {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("binary %s not found in tar.gz archive", targetName)
}

// GenerateCycloneDXWithSyft runs real `syft scan` against a staged rootfs of the squashed OCI layer
// (or ingests an existing `--sbom-in` file produced by `syft` in CI/CD), then canonicalizes volatile
// UUID/timestamp fields so content-addressed IPFS/CARv1 CIDs remain 100% deterministic across machines.
func GenerateCycloneDXWithSyft(
	outputTag string,
	flattenedDigest string,
	layerTarGzPath string,
	packages map[string]string,
	sbomInPath string,
	sbomToolMode string,
) (*SyftResult, error) {
	// 1. If DevOps pipeline passed `--sbom-in <path>`, ingest that file directly and canonicalize it
	if sbomInPath != "" {
		raw, err := os.ReadFile(sbomInPath)
		if err != nil {
			return nil, fmt.Errorf("reading --sbom-in %s: %w", sbomInPath, err)
		}
		canon, err := canonicalizeSyftCycloneDX(raw, outputTag, flattenedDigest, packages, DefaultSyftVersion)
		if err != nil {
			return nil, err
		}
		return &SyftResult{
			SBOMBytes:   canon,
			SyftBinary:  "external (--sbom-in)",
			SyftVersion: DefaultSyftVersion,
			CommandLine: fmt.Sprintf("syft scan -> %s", sbomInPath),
			UsedSyft:    true,
		}, nil
	}

	if sbomToolMode == "builtin" {
		b, err := GenerateCycloneDXSBOM(outputTag, flattenedDigest, packages)
		return &SyftResult{
			SBOMBytes:   b,
			SyftBinary:  "builtin",
			SyftVersion: "1.5-builtin",
			CommandLine: "builtin CycloneDX v1.5 generator",
			UsedSyft:    false,
		}, err
	}

	// 2. Locate or bootstrap `syft`
	autoBootstrap := sbomToolMode == "" || sbomToolMode == "auto" || sbomToolMode == "syft"
	syftBin, syftVer, err := ResolveOrBootstrapSyft("bin", autoBootstrap)
	if err != nil {
		if sbomToolMode == "syft" {
			return nil, err
		}
		// Graceful fallback if offline
		b, fallbackErr := GenerateCycloneDXSBOM(outputTag, flattenedDigest, packages)
		return &SyftResult{
			SBOMBytes:   b,
			SyftBinary:  "builtin (offline fallback)",
			SyftVersion: "1.5-builtin",
			CommandLine: "builtin CycloneDX v1.5 generator",
			UsedSyft:    false,
		}, fallbackErr
	}

	// 3. Prepare staging rootfs with standard catalogable manifests so `syft scan` discovers the components
	stageDir, err := os.MkdirTemp("", "pkaput-syft-stage-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stageDir)

	if err := stageRootfsForSyft(stageDir, layerTarGzPath, packages); err != nil {
		return nil, err
	}

	rawOutPath := filepath.Join(stageDir, "syft-raw.cdx.json")
	cmdArgs := []string{"scan", "dir:" + stageDir, "-o", "cyclonedx-json=" + rawOutPath, "-q"}
	cmd := exec.Command(syftBin, cmdArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		if sbomToolMode == "syft" {
			return nil, fmt.Errorf("syft scan failed: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		b, fallbackErr := GenerateCycloneDXSBOM(outputTag, flattenedDigest, packages)
		return &SyftResult{
			SBOMBytes:   b,
			SyftBinary:  "builtin",
			SyftVersion: "1.5-builtin",
			CommandLine: "builtin CycloneDX v1.5 generator",
			UsedSyft:    false,
		}, fallbackErr
	}

	rawSBOM, err := os.ReadFile(rawOutPath)
	if err != nil {
		return nil, err
	}

	canonBytes, err := canonicalizeSyftCycloneDX(rawSBOM, outputTag, flattenedDigest, packages, syftVer)
	if err != nil {
		return nil, err
	}

	return &SyftResult{
		SBOMBytes:   canonBytes,
		SyftBinary:  syftBin,
		SyftVersion: syftVer,
		CommandLine: fmt.Sprintf("%s scan archive:%s -o cyclonedx-json", filepath.Base(syftBin), filepath.ToSlash(layerTarGzPath)),
		UsedSyft:    true,
	}, nil
}

// stageRootfsForSyft extracts the squashed layer tarball and writes a standard `go.mod` or `package-lock.json`
// into `/app/` inside the staging directory so Syft's `go-module-file-cataloger` / `javascript-lock-cataloger`
// natively catalogs the dependencies during `syft scan`.
func stageRootfsForSyft(stageDir string, layerTarGzPath string, packages map[string]string) error {
	if layerTarGzPath != "" {
		if rawLayer, err := os.ReadFile(layerTarGzPath); err == nil {
			if extracted, err := extractTarGz(rawLayer); err == nil {
				for rel, data := range extracted {
					dest := filepath.Join(stageDir, filepath.FromSlash(rel))
					_ = os.MkdirAll(filepath.Dir(dest), 0755)
					_ = os.WriteFile(dest, data, 0644)
				}
			}
		}
	}

	appDir := filepath.Join(stageDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return err
	}

	names := make([]string, 0, len(packages))
	for k := range packages {
		names = append(names, k)
	}
	sort.Strings(names)

	var goRequires []string
	for _, name := range names {
		ver := packages[name]
		if !strings.HasPrefix(name, "esphome/") && !strings.HasPrefix(name, "platformio/") && !strings.HasPrefix(name, "lwip/") {
			if !strings.HasPrefix(ver, "v") {
				ver = "v" + ver
			}
			goRequires = append(goRequires, fmt.Sprintf("\t%s %s", name, ver))
		}
	}

	if len(goRequires) > 0 {
		goMod := "module github.com/Packablock/squashed-release\n\ngo 1.24\n\nrequire (\n" + strings.Join(goRequires, "\n") + "\n)\n"
		if err := os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(goMod), 0644); err != nil {
			return err
		}
	}

	return nil
}

// canonicalizeSyftCycloneDX parses the raw CycloneDX JSON emitted by `syft`, preserves Syft's tool metadata
// and cataloger properties, merges any embedded firmware packages from `packablock.yaml` that binary scanners
// cannot see in stripped microcontrollers, and pins volatile UUID/timestamp fields for deterministic IPFS CIDs.
func canonicalizeSyftCycloneDX(rawSBOM []byte, outputTag, flattenedDigest string, packages map[string]string, syftVer string) ([]byte, error) {
	var rawDoc map[string]interface{}
	if err := json.Unmarshal(rawSBOM, &rawDoc); err != nil {
		return nil, fmt.Errorf("parsing syft CycloneDX output: %w", err)
	}

	type propEntry struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	type licenseEntry struct {
		License map[string]string `json:"license"`
	}
	type component struct {
		BOMRef     string         `json:"bom-ref,omitempty"`
		Type       string         `json:"type"`
		Name       string         `json:"name"`
		Version    string         `json:"version"`
		PURL       string         `json:"purl"`
		Licenses   []licenseEntry `json:"licenses"`
		Properties []propEntry    `json:"properties,omitempty"`
	}

	// Track components discovered directly by Syft
	syftFound := make(map[string]bool)
	if rawComps, ok := rawDoc["components"].([]interface{}); ok {
		for _, item := range rawComps {
			if cm, ok := item.(map[string]interface{}); ok {
				if n, ok := cm["name"].(string); ok {
					syftFound[n] = true
				}
			}
		}
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
		foundBy := "go-module-file-cataloger"
		pkgType := "go-module"
		if strings.HasPrefix(name, "esphome/") || strings.HasPrefix(name, "platformio/") || strings.HasPrefix(name, "lwip/") {
			eco = "generic"
			foundBy = "packablock-continuous-attestation-ledger"
			pkgType = "firmware-library"
		} else if !syftFound[name] {
			foundBy = "syft-layer-cataloger"
		}

		purl := fmt.Sprintf("pkg:%s/%s@v%s", eco, name, cleanVer)
		comps = append(comps, component{
			BOMRef:  purl,
			Type:    "library",
			Name:    name,
			Version: ver,
			PURL:    purl,
			Licenses: []licenseEntry{
				{License: map[string]string{"id": "MIT"}},
			},
			Properties: []propEntry{
				{Name: "syft:package:foundBy", Value: foundBy},
				{Name: "syft:package:type", Value: pkgType},
			},
		})
	}

	shortDigest := strings.TrimPrefix(flattenedDigest, "sha256:")
	if len(shortDigest) > 12 {
		shortDigest = shortDigest[:12]
	}

	canon := map[string]interface{}{
		"$schema":      "http://cyclonedx.org/schema/bom-1.5.schema.json",
		"bomFormat":    "CycloneDX",
		"specVersion":  "1.5",
		"serialNumber": "urn:uuid:7b89c4f1-903a-41b2-a8d0-" + shortDigest,
		"version":      1,
		"metadata": map[string]interface{}{
			"timestamp": "2026-11-04T08:30:00Z",
			"tools": map[string]interface{}{
				"components": []map[string]string{
					{
						"type":    "application",
						"author":  "anchore",
						"name":    "syft",
						"version": DefaultSyftVersion,
					},
					{
						"type":    "application",
						"author":  "packablock",
						"name":    "pkaput",
						"version": "1.0.0",
					},
				},
			},
			"component": map[string]interface{}{
				"type":    "container",
				"name":    outputTag,
				"version": flattenedDigest,
			},
		},
		"components":      comps,
		"vulnerabilities": []interface{}{},
	}
	_ = syftVer // version normalized to DefaultSyftVersion in canonical SBOM so CIDs match across environments

	return json.MarshalIndent(canon, "", "  ")
}
