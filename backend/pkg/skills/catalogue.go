package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const (
	// DefaultCatalogueLimit is how many skills a flow catalogue carries. The
	// plan targets 30 to 60 entries, about 1000 to 1600 tokens.
	DefaultCatalogueLimit = 40
	// DefaultCatalogueCharBudget caps the rendered catalogue. Roughly four
	// characters per token keeps it near 1500 tokens.
	DefaultCatalogueCharBudget = 6000

	// catalogueCandidateLimit is the number of search results considered
	// before ordering and truncation.
	catalogueCandidateLimit = 50
	// catalogueEntryLimits keep every line readable inside the budget.
	catalogueTitleLen       = 90
	catalogueDescriptionLen = 160
	catalogueTags           = 5
)

// CatalogueOptions selects and renders the skill catalogue injected into the
// system prompts of a flow.
type CatalogueOptions struct {
	Query         string
	Limit         int
	CharBudget    int
	IncludeDrafts bool
	Kinds         []string
}

// Catalogue is the set of skills a flow may use, frozen for its lifetime by
// the caller. Text is empty when the catalogue has nothing to offer.
type Catalogue struct {
	Entries   []Document
	Text      string
	Hash      string
	Truncated int
}

// BuildCatalogue selects the skills relevant to a flow and renders them into a
// compact prompt block. The selection is deterministic for a given store state
// so that the catalogue does not change between two calls of the same flow.
func (s *Store) BuildCatalogue(ctx context.Context, userID int64, opts CatalogueOptions) (*Catalogue, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}

	opts = normaliseCatalogueOptions(opts)
	filter := Filter{
		Kinds:    opts.Kinds,
		Statuses: catalogueStatuses(opts.IncludeDrafts),
	}

	var documents []Document
	if strings.TrimSpace(opts.Query) != "" {
		hits, err := s.Search(ctx, userID, opts.Query, filter, catalogueCandidateLimit)
		if err != nil {
			return nil, err
		}
		documents = make([]Document, 0, len(hits))
		for _, hit := range hits {
			documents = append(documents, hit.Document)
		}
	} else {
		listed, err := s.List(ctx, userID, filter)
		if err != nil {
			return nil, err
		}
		documents = listed
	}

	return RenderCatalogue(documents, opts), nil
}

// RenderCatalogue orders, caps and formats the candidate documents. It is pure
// so the prompt shape can be tested without a database.
func RenderCatalogue(documents []Document, opts CatalogueOptions) *Catalogue {
	opts = normaliseCatalogueOptions(opts)

	ordered := make([]Document, 0, len(documents))
	for _, doc := range documents {
		if strings.TrimSpace(doc.Slug) == "" {
			continue
		}
		if !catalogueKindAllowed(doc.Kind, opts.Kinds) {
			continue
		}
		if !catalogueStatusAllowed(doc.Status, opts.IncludeDrafts) {
			continue
		}
		ordered = append(ordered, doc)
	}

	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if rank := statusRank(left.Status) - statusRank(right.Status); rank != 0 {
			return rank < 0
		}
		if left.Quality != right.Quality {
			return left.Quality > right.Quality
		}
		if left.Title != right.Title {
			return left.Title < right.Title
		}
		return left.Slug < right.Slug
	})

	total := len(ordered)
	if total == 0 {
		return &Catalogue{}
	}
	if len(ordered) > opts.Limit {
		ordered = ordered[:opts.Limit]
	}

	header := "<available_skills>\n" +
		"Curated skills and references stored for this engagement. " +
		"They are reviewed procedures: read the matching one before improvising a technique.\n\n"
	footer := "\nUse read_skill(slug=\"...\") to load a full procedure, or one section of it. " +
		"Use materialize_skill(slug=\"...\") when the procedure ships scripts, wordlists or templates you need to run.\n" +
		"</available_skills>"

	remaining := opts.CharBudget - len(header) - len(footer)
	if remaining <= 0 {
		return &Catalogue{}
	}

	var b strings.Builder
	b.WriteString(header)

	written := 0
	for _, doc := range ordered {
		line := catalogueLine(doc)
		if len(line) > remaining {
			break
		}
		b.WriteString(line)
		remaining -= len(line)
		written++
	}

	catalogue := &Catalogue{
		Entries:   ordered[:written],
		Hash:      catalogueHash(ordered[:written]),
		Truncated: total - written,
	}

	if catalogue.Truncated > 0 {
		fmt.Fprintf(&b, "\n%d more matching skills are available through list_skills and search_skills.\n", catalogue.Truncated)
	}

	b.WriteString(footer)
	catalogue.Text = b.String()

	return catalogue
}

func catalogueLine(doc Document) string {
	label := strings.TrimSpace(doc.Description)
	if label == "" {
		label = strings.TrimSpace(doc.Title)
	}
	label = oneLine(label, catalogueDescriptionLen)
	if label == "" {
		label = strings.TrimSpace(doc.Title)
	}

	line := fmt.Sprintf("- `%s` [%s/%s]", doc.Slug, doc.Kind, doc.Status)
	if title := oneLine(doc.Title, catalogueTitleLen); title != "" && title != label {
		line += " " + title + "."
	}
	if label != "" {
		line += " " + label
	}
	if len(doc.Tags) > 0 {
		tags := doc.Tags
		if len(tags) > catalogueTags {
			tags = tags[:catalogueTags]
		}
		line += " [" + strings.Join(tags, ", ") + "]"
	}

	return line + "\n"
}

// oneLine collapses whitespace and truncates on a rune boundary, so a
// description written for humans stays on a single prompt line.
func oneLine(value string, limit int) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	joined := strings.Join(fields, " ")
	runes := []rune(joined)
	if len(runes) <= limit {
		return joined
	}
	return strings.TrimRight(string(runes[:limit]), " ,.;:") + "..."
}

func catalogueHash(documents []Document) string {
	var b strings.Builder
	for _, doc := range documents {
		fmt.Fprintf(&b, "%s|%d|%s\n", doc.Slug, doc.Version, doc.ContentHash)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:16]
}

func normaliseCatalogueOptions(opts CatalogueOptions) CatalogueOptions {
	if opts.Limit <= 0 {
		opts.Limit = DefaultCatalogueLimit
	}
	if opts.CharBudget <= 0 {
		opts.CharBudget = DefaultCatalogueCharBudget
	}
	if len(opts.Kinds) == 0 {
		opts.Kinds = []string{KindSkill, KindReference}
	}
	return opts
}

func catalogueStatuses(includeDrafts bool) []string {
	if includeDrafts {
		return []string{StatusCanonical, StatusReviewed, StatusDraft}
	}
	return []string{StatusCanonical, StatusReviewed}
}

func catalogueStatusAllowed(status string, includeDrafts bool) bool {
	for _, allowed := range catalogueStatuses(includeDrafts) {
		if status == allowed {
			return true
		}
	}
	return false
}

func catalogueKindAllowed(kind string, allowed []string) bool {
	for _, candidate := range allowed {
		if kind == candidate {
			return true
		}
	}
	return false
}

func statusRank(status string) int {
	switch status {
	case StatusCanonical:
		return 0
	case StatusReviewed:
		return 1
	case StatusDraft:
		return 2
	default:
		return 3
	}
}
