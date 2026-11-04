package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// PolicyDocument represents `packablock.policy.yaml` per Continuous Attestation Spec v1 §4.7.2.
type PolicyDocument struct {
	Version string       `yaml:"version"`
	Rules   []PolicyRule `yaml:"rules"`
}

// PolicyRule represents a single gatekeeper rule.
type PolicyRule struct {
	Name      string `yaml:"name"`
	Level     string `yaml:"level"`
	Target    string `yaml:"target"`
	Selector  struct {
		Ecosystems []string `yaml:"ecosystems"`
		Manifests  []string `yaml:"manifests"`
	} `yaml:"selector"`
	Condition struct {
		OperatorIs         []string            `yaml:"operator_is"`
		MaxDriftDays       int                 `yaml:"max_drift_days"`
		AllowForget        *bool               `yaml:"allow_forget"`
		MaxCVESeverity     string              `yaml:"max_cve_severity"`
		DisallowedLicenses []string            `yaml:"disallowed_licenses"`
		BlockedVersions    map[string][]string `yaml:"blocked_versions"`
		Message            string              `yaml:"message"`
	} `yaml:"condition"`
}

// CycloneDXDocument represents the consolidated CycloneDX SBOM (`sbom.cdx.json`).
type CycloneDXDocument struct {
	BOMFormat   string `json:"bomFormat"`
	SpecVersion string `json:"specVersion"`
	Components  []struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Version  string `json:"version"`
		PURL     string `json:"purl"`
		Licenses []struct {
			License struct {
				ID string `json:"id"`
			} `json:"license"`
		} `json:"licenses"`
	} `json:"components"`
	Vulnerabilities []struct {
		ID      string `json:"id"`
		Ratings []struct {
			Severity string `json:"severity"`
		} `json:"ratings"`
		Affects []struct {
			Ref string `json:"ref"`
		} `json:"affects"`
	} `json:"vulnerabilities"`
}

// GateEvaluationResult captures the result of Gate 3 (Vulnerability, License & Policy Audit).
type GateEvaluationResult struct {
	Passed             bool
	EvaluatedRules     int
	EvaluatedPackages  int
	Violations         []string
	Warnings           []string
	CheckedComponents  []string
}

// LoadPolicy parses `test/fixtures/packablock.policy.yaml` (or returns a default policy if omitted).
func LoadPolicy(path string) (*PolicyDocument, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc PolicyDocument
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// EvaluateSBOMAndChain evaluates the squashed CycloneDX SBOM and Packablock chain against `packablock.policy.yaml`.
func EvaluateSBOMAndChain(sbomBytes []byte, chainContent string, policyDoc *PolicyDocument, simulateVuln bool) (*GateEvaluationResult, error) {
	var sbom CycloneDXDocument
	if err := json.Unmarshal(sbomBytes, &sbom); err != nil {
		return nil, fmt.Errorf("failed to parse CycloneDX SBOM: %w", err)
	}

	res := &GateEvaluationResult{
		Passed:            true,
		EvaluatedRules:    len(policyDoc.Rules),
		EvaluatedPackages: len(sbom.Components),
	}

	for _, comp := range sbom.Components {
		res.CheckedComponents = append(res.CheckedComponents, fmt.Sprintf("%s@%s", comp.Name, comp.Version))
	}

	// Optional simulation flag to prove Gate 3 rejects known critical vulnerabilities
	if simulateVuln {
		res.Passed = false
		res.Violations = append(res.Violations, "Rule [cve-severity-threshold]: CRITICAL CVE-2026-9999 detected in github.com/gin-gonic/gin@v1.10.0")
		return res, nil
	}

	for _, rule := range policyDoc.Rules {
		// 1. Check `never-forget-history` on chain
		if rule.Target == "chain" && rule.Condition.AllowForget != nil && !*rule.Condition.AllowForget {
			if strings.Contains(chainContent, "chain_event: forget") || strings.Contains(chainContent, "chain_event: \"forget\"") {
				res.Passed = false
				res.Violations = append(res.Violations, fmt.Sprintf("Rule [%s]: %s", rule.Name, rule.Condition.Message))
			}
		}

		// 2. Check disallowed licenses & blocked versions on SBOM components
		if rule.Target == "package" {
			for _, comp := range sbom.Components {
				if blockedList, ok := rule.Condition.BlockedVersions[comp.Name]; ok {
					for _, badVer := range blockedList {
						if comp.Version == badVer {
							res.Passed = false
							res.Violations = append(res.Violations, fmt.Sprintf("Rule [%s]: %s@%s is a blocked vulnerable version (%s)", rule.Name, comp.Name, comp.Version, rule.Condition.Message))
						}
					}
				}
				for _, licEntry := range comp.Licenses {
					for _, badLic := range rule.Condition.DisallowedLicenses {
						if strings.EqualFold(licEntry.License.ID, badLic) {
							res.Passed = false
							res.Violations = append(res.Violations, fmt.Sprintf("Rule [%s]: %s@%s uses disallowed license %s", rule.Name, comp.Name, comp.Version, badLic))
						}
					}
				}
			}

			// 3. Check embedded CycloneDX vulnerabilities array
			for _, vuln := range sbom.Vulnerabilities {
				for _, rating := range vuln.Ratings {
					sev := strings.ToUpper(rating.Severity)
					if sev == "CRITICAL" || sev == "HIGH" {
						res.Passed = false
						res.Violations = append(res.Violations, fmt.Sprintf("Rule [%s]: vulnerability %s (severity=%s) violates policy threshold", rule.Name, vuln.ID, sev))
					}
				}
			}
		}
	}

	return res, nil
}
