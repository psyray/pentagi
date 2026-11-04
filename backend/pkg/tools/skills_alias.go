package tools

import (
	"context"
	"fmt"
	"strings"

	"pentagi/pkg/skills"
)

const skillSuggestionLimit = 3

// SkillSearcher is the subset of the skill store the legacy knowledge tools
// dual-read. Keeping it narrow lets tests pass a fake without a database.
type SkillSearcher interface {
	Search(ctx context.Context, userID int64, query string, filter skills.Filter, limit int) ([]skills.SearchHit, error)
}

// appendSkillSuggestions adds a compact <relevant_skills> block to a legacy
// knowledge tool result. The legacy tools keep reading langchain_pg_embedding
// (agent-generated guides, answers and code) while curated references live in
// the skill store. Dual-reading keeps the old tool names useful without
// duplicating the corpus or changing the prompts.
func appendSkillSuggestions(ctx context.Context, searcher SkillSearcher, userID int64, query, result string) string {
	if searcher == nil || strings.TrimSpace(query) == "" {
		return result
	}

	hits, err := searcher.Search(ctx, userID, query, skills.Filter{
		Kinds: []string{skills.KindSkill, skills.KindReference, skills.KindMission},
		Limit: skillSuggestionLimit,
	}, skillSuggestionLimit)
	if err != nil || len(hits) == 0 {
		return result
	}

	var b strings.Builder
	b.WriteString(result)
	b.WriteString("\n\n<relevant_skills>\n")
	b.WriteString("Curated skills matching this query. Read one with read_skill before acting.\n")
	for _, hit := range hits {
		fmt.Fprintf(&b, "- %s: %s (slug %q)\n", hit.Document.Title, hit.Document.Description, hit.Document.Slug)
	}
	b.WriteString("</relevant_skills>")

	return b.String()
}
