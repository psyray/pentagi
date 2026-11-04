package tools

import (
	"strings"
	"testing"

	"pentagi/pkg/skills"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func surfacingDetector(t *testing.T, slugs ...string) *skills.Detector {
	t.Helper()

	var b strings.Builder
	b.WriteString("rules:\n")
	for _, slug := range slugs {
		b.WriteString("  - slug: " + slug + "\n    keywords: [\"" + slug + "\"]\n")
	}

	detector, err := skills.ParseDetector([]byte(b.String()))
	require.NoError(t, err)

	return detector
}

func surfacingStore() *fakeSkillStore {
	docs := make([]skills.Document, 0, 4)
	for _, slug := range []string{"esc8", "jetdirect", "log4j", "ssrf"} {
		docs = append(docs, skills.Document{
			Slug: slug, Kind: skills.KindSkill, Status: skills.StatusCanonical,
			Title: "Skill " + slug, Description: "Do the " + slug + " thing.",
		})
	}

	return &fakeSkillStore{docs: docs}
}

func TestSkillSurfacer_AppendsAHintToAToolResult(t *testing.T) {
	surfacer := newSkillSurfacer(1, surfacingStore(), surfacingDetector(t, "esc8", "jetdirect"))

	block := surfacer.Surface(t.Context(), TerminalToolName, "80/tcp open http /certsrv/ esc8")

	require.NotEmpty(t, block)
	assert.Contains(t, block, "<relevant_skills>")
	assert.Contains(t, block, "`esc8` [skill/canonical]")
	assert.Contains(t, block, "read_skill")
	assert.Contains(t, block, "</relevant_skills>")
}

func TestSkillSurfacer_SurfacesEachSkillOncePerFlow(t *testing.T) {
	surfacer := newSkillSurfacer(1, surfacingStore(), surfacingDetector(t, "esc8"))

	first := surfacer.Surface(t.Context(), TerminalToolName, "esc8 found")
	require.NotEmpty(t, first)

	assert.Empty(t, surfacer.Surface(t.Context(), TerminalToolName, "esc8 again"))
}

func TestSkillSurfacer_IgnoresUnrelatedTools(t *testing.T) {
	store := surfacingStore()
	detector := surfacingDetector(t, "esc8")

	assert.Empty(t, newSkillSurfacer(1, store, detector).Surface(t.Context(), ReadSkillToolName, "esc8"))
	assert.Empty(t, newSkillSurfacer(1, store, detector).Surface(t.Context(), SearchSkillsToolName, "esc8"))
	assert.NotEmpty(t, newSkillSurfacer(1, store, detector).Surface(t.Context(), FileToolName, "esc8"))
	assert.NotEmpty(t, newSkillSurfacer(1, store, detector).Surface(t.Context(), WebSearchToolName, "esc8"))
}

func TestSkillSurfacer_CapsTheCards(t *testing.T) {
	surfacer := newSkillSurfacer(1, surfacingStore(),
		surfacingDetector(t, "esc8", "jetdirect", "log4j", "ssrf"))

	block := surfacer.Surface(t.Context(), TerminalToolName, "esc8 jetdirect log4j ssrf")

	assert.Equal(t, 3, strings.Count(block, "- `"), "at most three cards are surfaced")
	assert.NotContains(t, block, "`ssrf`")
}

func TestSkillSurfacer_StaysSilentWhenTheSkillIsNotStored(t *testing.T) {
	detector, err := skills.ParseDetector([]byte("rules:\n  - slug: missing\n    keywords: [\"esc8\"]\n"))
	require.NoError(t, err)

	surfacer := newSkillSurfacer(1, surfacingStore(), detector)

	assert.Empty(t, surfacer.Surface(t.Context(), TerminalToolName, "esc8"))

	surfacer.mu.Lock()
	_, seen := surfacer.seen["missing"]
	surfacer.mu.Unlock()
	assert.False(t, seen, "a missing skill is not marked as surfaced")
}
