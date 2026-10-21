package chains

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// maxDetections caps how many chains a single tool output may surface, so a
// noisy command cannot flood the agent context.
const maxDetections = 3

//go:embed detectors.yaml
var detectorsYAML []byte

// DetectRule maps high-precision output signatures to a curated chain id.
type DetectRule struct {
	Chain  string   `yaml:"chain"`
	AnyOf  []string `yaml:"any_of"`
	AllOf  []string `yaml:"all_of"`
	Reason string   `yaml:"reason"`
}

// match reports whether the rule fires against a lowercased output. all_of is a
// context guard (every token present); any_of is the signal (at least one).
func (r DetectRule) match(lowerOutput string) bool {
	for _, tok := range r.AllOf {
		if tok = strings.ToLower(strings.TrimSpace(tok)); tok == "" {
			continue
		}
		if !strings.Contains(lowerOutput, tok) {
			return false
		}
	}
	if len(r.AnyOf) == 0 {
		return len(r.AllOf) > 0
	}
	for _, tok := range r.AnyOf {
		if tok = strings.ToLower(strings.TrimSpace(tok)); tok == "" {
			continue
		}
		if strings.Contains(lowerOutput, tok) {
			return true
		}
	}
	return false
}

// Detection is one matched rule, resolved against the library.
type Detection struct {
	ChainID string
	Reason  string
	Chain   Chain
}

var (
	detectOnce sync.Once
	detectDet  []DetectRule
	detectLib  *Library
	detectErr  error
)

// loadDetection lazily loads the embedded detectors and the embedded library.
// Rules pointing at an unknown chain id are dropped (forward-compatible: a rule
// added ahead of its chain degrades to a no-op instead of failing).
func loadDetection() ([]DetectRule, *Library, error) {
	detectOnce.Do(func() {
		var rules []DetectRule
		if err := yaml.Unmarshal(detectorsYAML, &rules); err != nil {
			detectErr = fmt.Errorf("parse detectors: %w", err)
			return
		}
		lib, err := Load()
		if err != nil {
			detectErr = err
			return
		}
		kept := make([]DetectRule, 0, len(rules))
		for _, r := range rules {
			r.Chain = strings.TrimSpace(r.Chain)
			if r.Chain == "" {
				continue
			}
			if _, ok := lib.ByID(r.Chain); !ok {
				continue
			}
			if len(r.AllOf) == 0 && len(r.AnyOf) == 0 {
				continue
			}
			kept = append(kept, r)
		}
		detectDet, detectLib = kept, lib
	})
	return detectDet, detectLib, detectErr
}

// Detect scans a tool output for high-precision fingerprints and returns the
// matching chains, in rule order and deduplicated by chain id. It is
// deterministic and independent of any model decision. Returns nil on any load
// error so a broken library can never break a tool result.
func Detect(output string) []Detection {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	rules, lib, err := loadDetection()
	if err != nil || lib == nil {
		return nil
	}
	lower := strings.ToLower(output)
	seen := map[string]bool{}
	var out []Detection
	for _, r := range rules {
		if seen[r.Chain] || !r.match(lower) {
			continue
		}
		c, ok := lib.ByID(r.Chain)
		if !ok {
			continue
		}
		seen[r.Chain] = true
		out = append(out, Detection{ChainID: r.Chain, Reason: r.Reason, Chain: c})
		if len(out) >= maxDetections {
			break
		}
	}
	return out
}

// RenderDetections renders a compact, prescriptive block to append to a tool
// result. The safe-verification steps are inlined so the guarantee does not
// depend on a follow-up tool call; the tool is still pointed to for the rest.
// Returns "" when there is nothing to surface.
func RenderDetections(dets []Detection) string {
	if len(dets) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n<detected_techniques source=\"deterministic-signature-match\" ")
	b.WriteString("note=\"the command output matches technique class(es) covered by the curated library; ")
	b.WriteString("this is a detection and a safe-verification procedure, not an instruction to exploit\">\n")
	for _, d := range dets {
		fmt.Fprintf(&b, "- %s (%s)", d.Chain.Name, d.ChainID)
		if d.Chain.Severity != "" {
			fmt.Fprintf(&b, " [%s]", d.Chain.Severity)
		}
		if d.Reason != "" {
			fmt.Fprintf(&b, " : %s", strings.TrimSpace(d.Reason))
		}
		b.WriteString("\n")
		for i, l := range d.Chain.Links {
			fmt.Fprintf(&b, "   %d. %s -> SAFE verify: %s\n", i+1, strings.TrimSpace(l.Name), strings.TrimSpace(l.Verify))
		}
	}
	b.WriteString("Call search_exploit_chain (id=<one of the above>) for the full procedure (fingerprint, proof steps, remediation).\n")
	b.WriteString("</detected_techniques>\n")
	return b.String()
}
