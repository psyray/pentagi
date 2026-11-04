package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func catalogueFixture() []Document {
	return []Document{
		{Slug: "draft-ssrf", Kind: KindSkill, Status: StatusDraft, Title: "SSRF callback",
			Description: "Start an out of band listener.", Tags: []string{"ssrf"}, Quality: 90},
		{Slug: "adcs-esc8", Kind: KindSkill, Status: StatusCanonical, Title: "ADCS ESC8",
			Description: "Relay HTTP enrollment with NTLM.\nUse certipy.", Tags: []string{"adcs", "esc8"}, Quality: 10},
		{Slug: "kerberoast", Kind: KindSkill, Status: StatusReviewed, Title: "Kerberoasting",
			Description: "Request service tickets and crack them.", Tags: []string{"ad"}, Quality: 80},
		{Slug: "cve-2026-1111", Kind: KindReference, Status: StatusReviewed, Title: "CVE-2026-1111",
			Description: "Digest of a published vulnerability.", ContentHash: "abc", Version: 3},
	}
}

func TestRenderCatalogue_OrdersCanonicalFirstThenQuality(t *testing.T) {
	catalogue := RenderCatalogue(catalogueFixture(), CatalogueOptions{})

	require.NotEmpty(t, catalogue.Entries)
	slugs := make([]string, 0, len(catalogue.Entries))
	for _, doc := range catalogue.Entries {
		slugs = append(slugs, doc.Slug)
	}

	assert.Equal(t, []string{"adcs-esc8", "kerberoast", "cve-2026-1111"}, slugs)
	assert.Contains(t, catalogue.Text, "`adcs-esc8` [skill/canonical]")
	assert.Contains(t, catalogue.Text, "Relay HTTP enrollment with NTLM. Use certipy.")
	assert.Contains(t, catalogue.Text, "[adcs, esc8]")
	assert.Contains(t, catalogue.Text, "read_skill")
	assert.Contains(t, catalogue.Text, "materialize_skill")
	assert.NotContains(t, catalogue.Text, "\nUse certipy")
}

func TestRenderCatalogue_ExcludesDraftsUnlessAsked(t *testing.T) {
	catalogue := RenderCatalogue(catalogueFixture(), CatalogueOptions{})
	assert.NotContains(t, catalogue.Text, "draft-ssrf")
	assert.Zero(t, catalogue.Truncated)

	catalogue = RenderCatalogue(catalogueFixture(), CatalogueOptions{IncludeDrafts: true})
	assert.Contains(t, catalogue.Text, "draft-ssrf")
}

func TestRenderCatalogue_StopsAtTheCharacterBudget(t *testing.T) {
	docs := make([]Document, 0, 40)
	for i := 0; i < 40; i++ {
		docs = append(docs, Document{
			Slug:        "skill-" + strings.Repeat("x", 5),
			Kind:        KindSkill,
			Status:      StatusCanonical,
			Title:       "Title",
			Description: strings.Repeat("a long description ", 8),
		})
	}

	catalogue := RenderCatalogue(docs, CatalogueOptions{CharBudget: 1200})

	require.NotEmpty(t, catalogue.Entries)
	assert.Less(t, len(catalogue.Entries), len(docs))
	assert.Equal(t, len(docs)-len(catalogue.Entries), catalogue.Truncated)
	assert.Contains(t, catalogue.Text, "more matching skills are available")
	assert.LessOrEqual(t, len(catalogue.Text), 1400)
}

func TestRenderCatalogue_CapsTheEntryCount(t *testing.T) {
	docs := make([]Document, 0, 5)
	for i := 0; i < 5; i++ {
		docs = append(docs, Document{Slug: "s" + strings.Repeat("x", i+1), Kind: KindSkill, Status: StatusCanonical})
	}

	catalogue := RenderCatalogue(docs, CatalogueOptions{Limit: 2})

	assert.Len(t, catalogue.Entries, 2)
	assert.Equal(t, 3, catalogue.Truncated)
}

func TestRenderCatalogue_EmptyInputProducesNoBlock(t *testing.T) {
	catalogue := RenderCatalogue(nil, CatalogueOptions{})

	assert.Empty(t, catalogue.Entries)
	assert.Empty(t, catalogue.Text)
	assert.Empty(t, catalogue.Hash)
}

func TestRenderCatalogue_HashIsStableAndTracksVersions(t *testing.T) {
	docs := catalogueFixture()

	first := RenderCatalogue(docs, CatalogueOptions{})
	second := RenderCatalogue(docs, CatalogueOptions{})
	require.NotEmpty(t, first.Hash)
	assert.Equal(t, first.Hash, second.Hash)

	updated := catalogueFixture()
	updated[1].Version = 4
	assert.NotEqual(t, first.Hash, RenderCatalogue(updated, CatalogueOptions{}).Hash)
}

func TestRenderCatalogue_BudgetTooSmallForTheBlockEmitsNothing(t *testing.T) {
	catalogue := RenderCatalogue(catalogueFixture(), CatalogueOptions{CharBudget: 50})

	assert.Empty(t, catalogue.Text)
	assert.Empty(t, catalogue.Entries)
}
