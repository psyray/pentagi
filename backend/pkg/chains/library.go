package chains

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
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

func newLibrary() *Library {
	return &Library{byID: map[string]Chain{}}
}

func (l *Library) add(c Chain) {
	l.byID[c.ID] = c
	l.chains = append(l.chains, c)
}

func (l *Library) sortChains() {
	sort.Slice(l.chains, func(i, j int) bool { return l.chains[i].ID < l.chains[j].ID })
}

// Load returns the library baked into the binary.
func Load() (*Library, error) {
	lib, err := readLibrary(defaultFS, defaultDir)
	if err != nil {
		return nil, err
	}
	if len(lib.chains) == 0 {
		return nil, fmt.Errorf("no chains found in %q", defaultDir)
	}
	return lib, nil
}

// LoadFS reads every *.yaml file in dir (non-recursive) and builds a library.
// A malformed or invalid chain fails the whole load: the library is only served
// when it is internally consistent, so a bad file is a startup error rather
// than a silent gap. An empty directory is an error.
func LoadFS(fsys fs.FS, dir string) (*Library, error) {
	lib, err := readLibrary(fsys, dir)
	if err != nil {
		return nil, err
	}
	if len(lib.chains) == 0 {
		return nil, fmt.Errorf("no chains found in %q", dir)
	}
	return lib, nil
}

// LoadDir reads every *.yaml file in a directory on disk. An empty or missing
// directory yields an empty library, which is how an absent overlay behaves; a
// path that exists but is not a directory is an error.
func LoadDir(dir string) (*Library, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return newLibrary(), nil
	}

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return newLibrary(), nil
		}
		return nil, fmt.Errorf("stat chains dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("chains path %q is not a directory", dir)
	}

	return readLibrary(os.DirFS(dir), ".")
}

// LoadWithOverlay returns the embedded library merged with the chains found in
// overlayDir (a directory on disk). An empty or missing overlayDir yields the
// embedded library unchanged. A chain id present in both is an error: an
// overlay may add chains and refine none silently.
func LoadWithOverlay(overlayDir string) (*Library, error) {
	base, err := Load()
	if err != nil {
		return nil, err
	}

	extra, err := LoadDir(overlayDir)
	if err != nil {
		return nil, err
	}

	return base.merge(extra)
}

// merge returns a new library carrying every chain of both, refusing a
// duplicate id.
func (l *Library) merge(other *Library) (*Library, error) {
	out := newLibrary()
	for _, c := range l.chains {
		out.add(c)
	}
	if other != nil {
		for _, c := range other.chains {
			if _, dup := out.byID[c.ID]; dup {
				return nil, fmt.Errorf("duplicate chain id %q (embedded and overlay)", c.ID)
			}
			out.add(c)
		}
	}
	out.sortChains()
	return out, nil
}

// readLibrary parses every *.yaml file in dir (non-recursive). An empty result
// is not an error here; the callers decide whether emptiness is acceptable.
func readLibrary(fsys fs.FS, dir string) (*Library, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read chains dir %q: %w", dir, err)
	}

	lib := newLibrary()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}

		file := path.Join(dir, e.Name())
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, fmt.Errorf("read chain %q: %w", file, err)
		}

		c, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("chain %q: %w", file, err)
		}
		if _, dup := lib.byID[c.ID]; dup {
			return nil, fmt.Errorf("duplicate chain id %q in %q", c.ID, dir)
		}

		lib.add(c)
	}
	lib.sortChains()
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
