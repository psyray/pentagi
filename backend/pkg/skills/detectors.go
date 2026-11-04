package skills

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// DetectorMaxMatches bounds how many slugs one detection returns. The caller
// still decides how many cards it surfaces.
const DetectorMaxMatches = 3

//go:embed detectors.yaml
var embeddedDetectors []byte

// DetectorRule maps observed output to a skill slug. A rule matches when any of
// its keywords is present (case-insensitive) or any of its regular expressions
// matches. Keywords are meant for banners, ports and error strings; regular
// expressions for patterns that need context.
type DetectorRule struct {
	Slug     string   `yaml:"slug"`
	Comment  string   `yaml:"comment,omitempty"`
	Keywords []string `yaml:"keywords,omitempty"`
	Regex    []string `yaml:"regex,omitempty"`
}

type detectorFile struct {
	Rules []DetectorRule `yaml:"rules"`
}

type compiledRule struct {
	slug   string
	needle []string
	regex  []*regexp.Regexp
}

// Detector turns raw tool output into the list of skills that match it. It is
// deterministic and cheap: no embedding, no model call, so it can run on every
// tool result.
type Detector struct {
	rules []compiledRule
}

// LoadDetector builds the embedded detector. An empty path uses the bundled
// rules; any other value replaces them with a YAML file, which lets an operator
// grow the rule set without rebuilding the image.
func LoadDetector(path string) (*Detector, error) {
	data := embeddedDetectors
	if strings.TrimSpace(path) != "" {
		read, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("skills: read detectors: %w", err)
		}
		data = read
	}

	return ParseDetector(data)
}

// ParseDetector compiles a rule file. Rules without any matcher are rejected so
// a typo cannot silently turn a rule into a catch-all.
func ParseDetector(data []byte) (*Detector, error) {
	var file detectorFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("skills: parse detectors: %w", err)
	}

	detector := &Detector{rules: make([]compiledRule, 0, len(file.Rules))}
	for _, rule := range file.Rules {
		slug := strings.TrimSpace(rule.Slug)
		if slug == "" {
			return nil, fmt.Errorf("skills: detector rule without a slug")
		}
		if len(rule.Keywords) == 0 && len(rule.Regex) == 0 {
			return nil, fmt.Errorf("skills: detector rule %q has no keyword or regex", slug)
		}

		compiled := compiledRule{slug: slug}
		for _, keyword := range rule.Keywords {
			keyword = strings.ToLower(strings.TrimSpace(keyword))
			if keyword != "" {
				compiled.needle = append(compiled.needle, keyword)
			}
		}
		for _, pattern := range rule.Regex {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("skills: detector rule %q has an invalid regex %q: %w", slug, pattern, err)
			}
			compiled.regex = append(compiled.regex, re)
		}
		detector.rules = append(detector.rules, compiled)
	}

	return detector, nil
}

// Match returns at most DetectorMaxMatches slugs, in rule order, without
// duplicates.
func (d *Detector) Match(text string) []string {
	if d == nil || len(d.rules) == 0 || strings.TrimSpace(text) == "" {
		return nil
	}

	// Lowercasing once is much cheaper than per rule, and the output of a scan
	// is usually large.
	lower := strings.ToLower(text)

	var matches []string
	seen := make(map[string]struct{}, DetectorMaxMatches)
	for _, rule := range d.rules {
		if len(matches) >= DetectorMaxMatches {
			break
		}
		if _, ok := seen[rule.slug]; ok {
			continue
		}
		if !rule.matches(lower, text) {
			continue
		}
		seen[rule.slug] = struct{}{}
		matches = append(matches, rule.slug)
	}

	return matches
}

func (r compiledRule) matches(lower, original string) bool {
	for _, keyword := range r.needle {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	for _, re := range r.regex {
		if re.MatchString(original) {
			return true
		}
	}
	return false
}
