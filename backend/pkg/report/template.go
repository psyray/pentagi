package report

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"

	"pentagi/pkg/database"

	"gopkg.in/yaml.v3"
)

//go:embed templates/*.yaml
var templateFS embed.FS

// A Template is the document outline of one report run (see
// templates/pentagi-default-pentest-report.yaml for the full field semantics).
type Template struct {
	Key      string        `yaml:"key" json:"key"`
	Label    string        `yaml:"label" json:"label"`
	Version  int           `yaml:"version" json:"version"`
	Defaults Lengths       `yaml:"defaults" json:"defaults"`
	Sections []SectionNode `yaml:"sections" json:"sections"`
}

type Lengths struct {
	Length      string `yaml:"length" json:"length"`
	ChildLength string `yaml:"child_length" json:"child_length"`
}

// SectionNode is one outline node of the template skeleton. Aggregate nodes
// carry the child vocabulary (source + fallback alternatives); children the
// planner instantiates become report_sections rows under the parent.
type SectionNode struct {
	Key               string      `yaml:"key" json:"key"`
	Kind              string      `yaml:"kind" json:"kind"`
	Title             string      `yaml:"title" json:"title"`
	Numbered          *bool       `yaml:"numbered,omitempty" json:"numbered"`
	Defer             bool        `yaml:"defer,omitempty" json:"defer"`
	Length            string      `yaml:"length,omitempty" json:"length"`
	ChildLength       string      `yaml:"child_length,omitempty" json:"child_length"`
	Instructions      string      `yaml:"instructions,omitempty" json:"instructions"`
	ChildInstructions string      `yaml:"child_instructions,omitempty" json:"child_instructions"`
	ChildrenSource    string      `yaml:"children_source,omitempty" json:"children_source"`
	EmptyChildren     []ChildNode `yaml:"empty_children,omitempty" json:"empty_children"`
}

type ChildNode struct {
	Key          string `yaml:"key" json:"key"`
	Title        string `yaml:"title" json:"title"`
	Instructions string `yaml:"instructions,omitempty" json:"instructions"`
}

// Validate enforces the loader invariants: unique top-level keys, known kinds,
// aggregate nodes carry a children source or alternatives, every node has a
// title (except composed nodes, which may fall back to the compose label).
func (t *Template) Validate() error {
	if strings.TrimSpace(t.Key) == "" {
		return fmt.Errorf("template key is empty")
	}
	if t.Label == "" {
		return fmt.Errorf("template %s: label is empty", t.Key)
	}
	seen := make(map[string]struct{}, len(t.Sections))
	for _, node := range t.Sections {
		if node.Key == "" {
			return fmt.Errorf("template %s: a section has an empty key", t.Key)
		}
		if _, dup := seen[node.Key]; dup {
			return fmt.Errorf("template %s: duplicate section key %q", t.Key, node.Key)
		}
		seen[node.Key] = struct{}{}
		if !KnownSectionKind(node.Kind) {
			return fmt.Errorf("template %s: section %s: unknown kind %q", t.Key, node.Key, node.Kind)
		}
		if node.Title == "" {
			return fmt.Errorf("template %s: section %s: title is empty", t.Key, node.Key)
		}
		if node.Kind == KindAggregate {
			altKeys := make(map[string]struct{}, len(node.EmptyChildren))
			for _, alt := range node.EmptyChildren {
				if alt.Key == "" || alt.Title == "" {
					return fmt.Errorf(
						"template %s: section %s: empty_children entries need key and title", t.Key, node.Key)
				}
				if _, dup := altKeys[alt.Key]; dup {
					return fmt.Errorf(
						"template %s: section %s: duplicate empty_children key %q", t.Key, node.Key, alt.Key)
				}
				altKeys[alt.Key] = struct{}{}
			}
			if len(node.EmptyChildren) == 0 && node.ChildrenSource == "" {
				return fmt.Errorf(
					"template %s: section %s: aggregate without children_source or empty_children", t.Key, node.Key)
			}
		}
	}

	return nil
}

// Unnumbered reports whether the section is excluded from derived numbering
// (front matter like the cover page).
func (n *SectionNode) Unnumbered() bool {
	return n.Numbered != nil && !*n.Numbered
}

// LengthHint returns the writing length of the section, falling back to the
// template defaults, then to a sane inline default.
func (n *SectionNode) LengthHint(t *Template, isChild bool) string {
	switch {
	case isChild:
		if n.ChildLength != "" {
			return n.ChildLength
		}
		return t.Defaults.ChildLength
	default:
		if n.Length != "" {
			return n.Length
		}
		return t.Defaults.Length
	}
}

// ListTemplates returns the keys of every bundled template, sorted.
func ListTemplates() ([]string, error) {
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		return nil, fmt.Errorf("failed to read bundled templates: %w", err)
	}

	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		tpl, err := LoadTemplate(strings.TrimSuffix(name, ".yaml"))
		if err != nil {
			return nil, err
		}
		keys = append(keys, tpl.Key)
	}
	sort.Strings(keys)

	return keys, nil
}

// GetTemplate returns the bundled template by the generic key ("bundled/<key>"
// from the model selects are stripped to <key> here).
func GetTemplate(key string) (*Template, error) {
	key = strings.TrimPrefix(key, "bundled/")
	return LoadTemplate(key)
}

// BundledYAML returns the raw YAML of a bundled template by key (the file base
// name), so the Settings editor can show a built-in template read-only.
func BundledYAML(key string) (string, error) {
	key = strings.TrimPrefix(key, "bundled/")
	raw, err := templateFS.ReadFile("templates/" + key + ".yaml")
	if err != nil {
		return "", fmt.Errorf("template %s not found: %w", key, err)
	}
	return string(raw), nil
}

// LoadTemplate loads and validates one bundled template by file base name.
func LoadTemplate(file string) (*Template, error) {
	raw, err := templateFS.ReadFile("templates/" + file + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("template %s not found: %w", file, err)
	}

	return ParseTemplate(raw, file)
}

// ParseTemplate unmarshals and validates a template document. fallbackKey names
// the template when the YAML leaves the key empty (a bundled file's base name).
func ParseTemplate(raw []byte, fallbackKey string) (*Template, error) {
	var tpl Template
	if err := yaml.Unmarshal(raw, &tpl); err != nil {
		return nil, fmt.Errorf("failed to parse template %s: %w", fallbackKey, err)
	}
	if tpl.Key == "" {
		tpl.Key = fallbackKey
	}
	if err := tpl.Validate(); err != nil {
		return nil, err
	}

	return &tpl, nil
}

// TemplateFromRow parses a stored user-defined template row.
func TemplateFromRow(row database.ReportTemplate) (*Template, error) {
	return ParseTemplate([]byte(row.Yaml), row.Key)
}

// ResolveTemplate loads the template a report run must follow, DB-first
// (backlog v2 L3): the caller's own template wins over a global one of the same
// key, and a missing, disabled or unparsable DB template degrades to the
// bundled template. A bad row must never stop a report (R1/R2).
func ResolveTemplate(ctx context.Context, q database.Querier, userID int64, key string) (*Template, error) {
	key = strings.TrimPrefix(key, "bundled/")
	if q == nil || userID == 0 {
		return GetTemplate(key)
	}

	row, err := q.GetReportTemplateByKey(ctx, database.GetReportTemplateByKeyParams{
		Key:    key,
		UserID: sql.NullInt64{Int64: userID, Valid: true},
	})
	if err != nil {
		return GetTemplate(key)
	}

	tpl, err := TemplateFromRow(row)
	if err != nil {
		// A stored template that no longer parses must not break a report that
		// already snapshotted its key: fall back to the bundled one.
		return GetTemplate(key)
	}

	return tpl, nil
}
