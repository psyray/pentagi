package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// EvalCase is one labelled query of the golden set. Expected holds the slugs of
// the documents that answer the query; any of them counts as a hit.
type EvalCase struct {
	ID       string   `yaml:"id" json:"id"`
	Query    string   `yaml:"query" json:"query"`
	Category string   `yaml:"category,omitempty" json:"category,omitempty"`
	Expected []string `yaml:"expected" json:"expected"`
	Kinds    []string `yaml:"kinds,omitempty" json:"kinds,omitempty"`
	Statuses []string `yaml:"statuses,omitempty" json:"statuses,omitempty"`
}

// EvalDataset is the golden set file. Statuses is the default publication scope
// of the run, so the same file can measure the dev corpus (drafts included) and
// the production view (reviewed and canonical only).
type EvalDataset struct {
	Corpus   string     `yaml:"corpus,omitempty" json:"corpus,omitempty"`
	Statuses []string   `yaml:"statuses,omitempty" json:"statuses,omitempty"`
	Cases    []EvalCase `yaml:"cases" json:"cases"`
}

// EvalOptions tunes the run.
type EvalOptions struct {
	// Ks is the list of cut-offs reported as recall@k. Defaults to 3 and 5.
	Ks []int
	// Limit is how many hits are read per query. Defaults to 10, which keeps
	// room to measure the rank of a document that missed the reported cut-offs.
	Limit int
	// Statuses overrides the dataset scope when set.
	Statuses []string
}

// EvalCaseResult is the outcome of one case.
type EvalCaseResult struct {
	ID       string   `json:"id"`
	Category string   `json:"category"`
	Query    string   `json:"query"`
	Rank     int      `json:"rank"`
	Hits     int      `json:"hits"`
	Empty    bool     `json:"empty"`
	TopSlugs []string `json:"topSlugs,omitempty"`
}

// EvalCategoryReport aggregates the cases of one category.
type EvalCategoryReport struct {
	Category  string          `json:"category"`
	Cases     int             `json:"cases"`
	Recall    map[int]float64 `json:"recall"`
	MRR       float64         `json:"mrr"`
	EmptyRate float64         `json:"emptyRate"`
}

// EvalReport is the result of a golden set run.
type EvalReport struct {
	Corpus     string               `json:"corpus,omitempty"`
	Cases      int                  `json:"cases"`
	Ks         []int                `json:"ks"`
	Recall     map[int]float64      `json:"recall"`
	MRR        float64              `json:"mrr"`
	EmptyRate  float64              `json:"emptyRate"`
	Categories []EvalCategoryReport `json:"categories"`
	Results    []EvalCaseResult     `json:"results"`
}

// LoadEvalDataset parses and validates a golden set.
func LoadEvalDataset(data []byte) (*EvalDataset, error) {
	var dataset EvalDataset
	if err := yaml.Unmarshal(data, &dataset); err != nil {
		return nil, fmt.Errorf("skills: parse eval dataset: %w", err)
	}
	if len(dataset.Cases) == 0 {
		return nil, fmt.Errorf("%w: the eval dataset has no case", ErrInvalidInput)
	}

	seen := make(map[string]struct{}, len(dataset.Cases))
	for i := range dataset.Cases {
		current := &dataset.Cases[i]
		current.ID = strings.TrimSpace(current.ID)
		current.Query = strings.TrimSpace(current.Query)
		if current.ID == "" {
			return nil, fmt.Errorf("%w: eval case %d has no id", ErrInvalidInput, i+1)
		}
		if _, exists := seen[current.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate eval case id %q", ErrInvalidInput, current.ID)
		}
		seen[current.ID] = struct{}{}
		if current.Query == "" {
			return nil, fmt.Errorf("%w: eval case %q has no query", ErrInvalidInput, current.ID)
		}
		if len(current.Expected) == 0 {
			return nil, fmt.Errorf("%w: eval case %q has no expected document", ErrInvalidInput, current.ID)
		}
		for _, slug := range current.Expected {
			if strings.TrimSpace(slug) == "" {
				return nil, fmt.Errorf("%w: eval case %q has an empty expected slug", ErrInvalidInput, current.ID)
			}
		}
	}

	return &dataset, nil
}

// LoadEvalFile reads a golden set from disk.
func LoadEvalFile(path string) (*EvalDataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("skills: read eval dataset: %w", err)
	}
	return LoadEvalDataset(data)
}

// Evaluate runs the golden set against the store. It only reads, so it is safe
// on a live instance; a run costs one embedding call per case.
func (s *Store) Evaluate(ctx context.Context, userID int64, dataset *EvalDataset, opts EvalOptions) (*EvalReport, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	if dataset == nil || len(dataset.Cases) == 0 {
		return nil, fmt.Errorf("%w: the eval dataset has no case", ErrInvalidInput)
	}

	opts = normaliseEvalOptions(opts)
	report := &EvalReport{
		Corpus: dataset.Corpus,
		Ks:     opts.Ks,
	}

	for _, current := range dataset.Cases {
		statuses := current.Statuses
		if len(statuses) == 0 {
			statuses = opts.Statuses
		}
		if len(statuses) == 0 {
			statuses = dataset.Statuses
		}

		hits, err := s.Search(ctx, userID, current.Query, Filter{
			Kinds:    current.Kinds,
			Statuses: statuses,
			Limit:    opts.Limit,
		}, opts.Limit)
		if err != nil {
			return nil, fmt.Errorf("skills: eval case %q: %w", current.ID, err)
		}

		result := EvalCaseResult{
			ID:       current.ID,
			Category: evalCategory(current),
			Query:    current.Query,
			Hits:     len(hits),
			Empty:    len(hits) == 0,
			Rank:     evalRank(current.Expected, hits),
			TopSlugs: evalTopSlugs(hits, 5),
		}
		report.Results = append(report.Results, result)
	}

	report.Cases = len(report.Results)
	report.Recall, report.MRR, report.EmptyRate = summariseEval(report.Results, opts.Ks)
	report.Categories = summariseEvalCategories(report.Results, opts.Ks)

	return report, nil
}

// Pass reports whether recall at the largest k reaches the target.
func (r *EvalReport) Pass(target float64) bool {
	if len(r.Ks) == 0 {
		return false
	}
	return r.Recall[r.Ks[len(r.Ks)-1]] >= target
}

// JSON renders the report for the CLI and for BenchDash.
func (r *EvalReport) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func normaliseEvalOptions(opts EvalOptions) EvalOptions {
	if len(opts.Ks) == 0 {
		opts.Ks = []int{3, 5}
	}

	cleaned := make([]int, 0, len(opts.Ks))
	for _, k := range opts.Ks {
		if k > 0 {
			cleaned = append(cleaned, k)
		}
	}
	if len(cleaned) == 0 {
		cleaned = []int{3, 5}
	}
	sort.Ints(cleaned)
	opts.Ks = cleaned

	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	largest := cleaned[len(cleaned)-1]
	if opts.Limit < largest {
		opts.Limit = largest
	}

	return opts
}

// evalRank returns the 1-based rank of the first expected document, or 0.
func evalRank(expected []string, hits []SearchHit) int {
	if len(expected) == 0 {
		return 0
	}

	wanted := make(map[string]struct{}, len(expected))
	for _, slug := range expected {
		wanted[strings.ToLower(strings.TrimSpace(slug))] = struct{}{}
	}

	for i, hit := range hits {
		if _, ok := wanted[strings.ToLower(strings.TrimSpace(hit.Document.Slug))]; ok {
			return i + 1
		}
	}

	return 0
}

func evalTopSlugs(hits []SearchHit, limit int) []string {
	if limit <= 0 || len(hits) == 0 {
		return nil
	}
	if len(hits) < limit {
		limit = len(hits)
	}

	slugs := make([]string, 0, limit)
	for _, hit := range hits[:limit] {
		slugs = append(slugs, hit.Document.Slug)
	}
	return slugs
}

func evalCategory(current EvalCase) string {
	if strings.TrimSpace(current.Category) != "" {
		return current.Category
	}
	return "uncategorized"
}

func summariseEval(results []EvalCaseResult, ks []int) (map[int]float64, float64, float64) {
	recall := make(map[int]float64, len(ks))
	if len(results) == 0 {
		return recall, 0, 0
	}

	var mrr, empty float64
	for _, result := range results {
		if result.Rank > 0 {
			mrr += 1 / float64(result.Rank)
		}
		if result.Empty {
			empty++
		}
	}
	for _, k := range ks {
		hits := 0
		for _, result := range results {
			if result.Rank > 0 && result.Rank <= k {
				hits++
			}
		}
		recall[k] = float64(hits) / float64(len(results))
	}

	return recall, mrr / float64(len(results)), empty / float64(len(results))
}

func summariseEvalCategories(results []EvalCaseResult, ks []int) []EvalCategoryReport {
	byCategory := make(map[string][]EvalCaseResult)
	for _, result := range results {
		byCategory[result.Category] = append(byCategory[result.Category], result)
	}

	categories := make([]string, 0, len(byCategory))
	for category := range byCategory {
		categories = append(categories, category)
	}
	sort.Strings(categories)

	reports := make([]EvalCategoryReport, 0, len(categories))
	for _, category := range categories {
		members := byCategory[category]
		recall, mrr, empty := summariseEval(members, ks)
		reports = append(reports, EvalCategoryReport{
			Category:  category,
			Cases:     len(members),
			Recall:    recall,
			MRR:       mrr,
			EmptyRate: empty,
		})
	}

	return reports
}
