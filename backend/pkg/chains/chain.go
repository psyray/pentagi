// Package chains implements a curated, deterministic library of exploit-chain
// definitions that agents look up by CVE, product, tag or free text, and that
// render into a safe-verification procedure.
//
// Design principles (fork extended-version, decorrelated track, plan § 2.10):
//
//   - Deterministic: lookup is a structured match over embedded YAML, never a
//     vector/semantic search, so a given query always returns the same chain.
//   - Safe verification only: every link carries a `verify` step that must
//     prove reachability without a destructive payload (out-of-band callback,
//     read-only probe, header inspection). Validation refuses a link with no
//     verify step, and the rendered document states the contract explicitly.
//   - Self-authored: the schema and the data are reimplemented from first
//     principles, never copied from an AGPL source.
package chains

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Severity is the coarse impact band of a chain.
type Severity string

const (
	SeverityCritical      Severity = "critical"
	SeverityHigh          Severity = "high"
	SeverityMedium        Severity = "medium"
	SeverityLow           Severity = "low"
	SeverityInformational Severity = "informational"
)

var validSeverities = map[Severity]bool{
	SeverityCritical:      true,
	SeverityHigh:          true,
	SeverityMedium:        true,
	SeverityLow:           true,
	SeverityInformational: true,
}

// Fingerprint is the cheap, non-intrusive signal set used to decide whether a
// chain is worth attempting against a target.
type Fingerprint struct {
	Paths   []string `yaml:"paths"`
	Signals []string `yaml:"signals"`
	Notes   string   `yaml:"notes"`
}

// Link is one exploitability step in a chain. Verify must describe a SAFE,
// detection-only confirmation, never a destructive payload.
type Link struct {
	Name    string `yaml:"name"`
	CVE     string `yaml:"cve"`
	Enables string `yaml:"enables"`
	Verify  string `yaml:"verify"`
}

// Chain is a single named, CVE-tied exploit-chain definition.
type Chain struct {
	ID          string      `yaml:"id"`
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	CVEs        []string    `yaml:"cves"`
	Product     string      `yaml:"product"`
	Affected    string      `yaml:"affected"`
	CVSS        float64     `yaml:"cvss"`
	Severity    Severity    `yaml:"severity"`
	Tags        []string    `yaml:"tags"`
	References  []string    `yaml:"references"`
	Fingerprint Fingerprint `yaml:"fingerprint"`
	Links       []Link      `yaml:"links"`
	Remediation string      `yaml:"remediation"`
}

// Parse decodes a single chain from YAML and validates it.
func Parse(data []byte) (Chain, error) {
	var c Chain
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Chain{}, fmt.Errorf("parse chain yaml: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Chain{}, err
	}
	return c, nil
}

// Validate reports whether the chain is well-formed and safe to publish.
func (c Chain) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("chain is missing id")
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("chain %q is missing name", c.ID)
	}
	if strings.TrimSpace(c.Description) == "" {
		return fmt.Errorf("chain %q is missing description", c.ID)
	}
	if c.CVSS < 0 || c.CVSS > 10 {
		return fmt.Errorf("chain %q has an out-of-range cvss %g", c.ID, c.CVSS)
	}
	if c.Severity != "" && !validSeverities[c.Severity] {
		return fmt.Errorf("chain %q has an unknown severity %q", c.ID, c.Severity)
	}
	if len(c.Links) == 0 {
		return fmt.Errorf("chain %q has no links", c.ID)
	}
	for i, l := range c.Links {
		if strings.TrimSpace(l.Name) == "" {
			return fmt.Errorf("chain %q link %d is missing name", c.ID, i+1)
		}
		if strings.TrimSpace(l.Verify) == "" {
			return fmt.Errorf("chain %q link %d has no verify step (safe verification is mandatory)", c.ID, i+1)
		}
	}
	return nil
}

// CVEsNormalized upper-cases and trims the CVE list for stable matching.
func (c Chain) CVEsNormalized() []string {
	out := make([]string, 0, len(c.CVEs))
	for _, v := range c.CVEs {
		out = append(out, strings.ToUpper(strings.TrimSpace(v)))
	}
	return out
}
