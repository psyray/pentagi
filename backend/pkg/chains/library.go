package chains

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed data/*.yaml
var defaultFS embed.FS

// defaultDir is the directory inside the embedded FS that holds the chains.
const defaultDir = "data"

// Library is an immutable, deduplicated set of chains loaded from YAML.
type Library struct {
	chains []Chain
	byID   map[string]Chain
}

// Load returns the library baked into the binary.
func Load() (*Library, error) {
	return LoadFS(defaultFS, defaultDir)
}

// LoadFS reads every *.yaml file in dir (non-recursive) and builds a library.
// A malformed or invalid chain fails the whole load: the library is only served
// when it is internally consistent, so a bad file is a startup error rather
// than a silent gap.
func LoadFS(fsys fs.FS, dir string) (*Library, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read chains dir %q: %w", dir, err)
	}

	lib := &Library{byID: map[string]Chain{}}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}

		path := dir + "/" + e.Name()
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil, fmt.Errorf("read chain %q: %w", path, err)
		}

		c, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("chain %q: %w", path, err)
		}
		if _, dup := lib.byID[c.ID]; dup {
			return nil, fmt.Errorf("duplicate chain id %q", c.ID)
		}

		lib.byID[c.ID] = c
		lib.chains = append(lib.chains, c)
	}
	if len(lib.chains) == 0 {
		return nil, fmt.Errorf("no chains found in %q", dir)
	}

	sort.Slice(lib.chains, func(i, j int) bool { return lib.chains[i].ID < lib.chains[j].ID })
	return lib, nil
}

// All returns every chain, sorted by id.
func (l *Library) All() []Chain {
	return append([]Chain(nil), l.chains...)
}

// ByID returns the chain with the given id (case-insensitive).
func (l *Library) ByID(id string) (Chain, bool) {
	key := strings.TrimSpace(id)
	if c, ok := l.byID[key]; ok {
		return c, true
	}
	for k, v := range l.byID {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return Chain{}, false
}

// ByCVE returns every chain referencing the given CVE (case-insensitive).
func (l *Library) ByCVE(cve string) []Chain {
	want := strings.ToUpper(strings.TrimSpace(cve))
	if want == "" {
		return nil
	}
	return l.filter(func(c Chain) bool {
		for _, v := range c.CVEsNormalized() {
			if v == want {
				return true
			}
		}
		return false
	})
}

// ByProduct returns every chain whose product, affected or name field mentions
// the given substring (case-insensitive).
func (l *Library) ByProduct(product string) []Chain {
	needle := strings.ToLower(strings.TrimSpace(product))
	if needle == "" {
		return nil
	}
	return l.filter(func(c Chain) bool {
		return strings.Contains(strings.ToLower(c.Product), needle) ||
			strings.Contains(strings.ToLower(c.Affected), needle) ||
			strings.Contains(strings.ToLower(c.Name), needle)
	})
}

// ByTag returns every chain carrying the given tag (case-insensitive).
func (l *Library) ByTag(tag string) []Chain {
	want := strings.ToLower(strings.TrimSpace(tag))
	if want == "" {
		return nil
	}
	return l.filter(func(c Chain) bool {
		for _, t := range c.Tags {
			if strings.EqualFold(strings.TrimSpace(t), want) {
				return true
			}
		}
		return false
	})
}

// Search is a deterministic, order-stable lookup over id, name, product,
// affected, tags, CVEs and description. Every whitespace-separated token of the
// query must match somewhere (AND semantics); results keep id order.
func (l *Library) Search(query string) []Chain {
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 {
		return nil
	}
	return l.filter(func(c Chain) bool {
		hay := strings.ToLower(strings.Join([]string{
			c.ID, c.Name, c.Description, c.Product, c.Affected,
			strings.Join(c.Tags, " "), strings.Join(c.CVEsNormalized(), " "),
		}, " "))
		for _, tok := range tokens {
			if !strings.Contains(hay, tok) {
				return false
			}
		}
		return true
	})
}

func (l *Library) filter(pred func(Chain) bool) []Chain {
	out := make([]Chain, 0)
	for _, c := range l.chains {
		if pred(c) {
			out = append(out, c)
		}
	}
	return out
}
