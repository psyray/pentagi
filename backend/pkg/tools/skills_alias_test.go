package tools

import (
	"context"
	"testing"

	"pentagi/pkg/skills"

	"github.com/stretchr/testify/assert"
)

type fakeSkillSearcher struct {
	hits     []skills.SearchHit
	err      error
	gotQuery string
}

func (f *fakeSkillSearcher) Search(_ context.Context, _ int64, query string, _ skills.Filter, _ int) ([]skills.SearchHit, error) {
	f.gotQuery = query
	return f.hits, f.err
}

func TestAppendSkillSuggestions(t *testing.T) {
	searcher := &fakeSkillSearcher{hits: []skills.SearchHit{{
		Document: skills.Document{
			Slug:        "adcs-esc8",
			Title:       "ADCS ESC8",
			Description: "Relay NTLM to ADCS web enrollment",
		},
	}}}

	got := appendSkillSuggestions(context.Background(), searcher, 1, "ADCS ESC8", "legacy result")

	assert.Contains(t, got, "legacy result")
	assert.Contains(t, got, "<relevant_skills>")
	assert.Contains(t, got, "adcs-esc8")
	assert.Equal(t, "ADCS ESC8", searcher.gotQuery)
}

func TestAppendSkillSuggestionsWithoutSearcher(t *testing.T) {
	assert.Equal(t, "legacy result", appendSkillSuggestions(context.Background(), nil, 1, "query", "legacy result"))
}

func TestAppendSkillSuggestionsWithoutHit(t *testing.T) {
	searcher := &fakeSkillSearcher{}

	assert.Equal(t, "legacy result", appendSkillSuggestions(context.Background(), searcher, 1, "query", "legacy result"))
}
