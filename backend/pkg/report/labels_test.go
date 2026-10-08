package report

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"pentagi/pkg/database"

	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func reportWithLabels(t *testing.T, labels map[string]string) database.Report {
	t.Helper()

	blob, err := json.Marshal(labels)
	require.NoError(t, err)

	return database.Report{
		ID:            7,
		Title:         "Rapport Paperwork",
		TemplateKey:   "pentagi-default-pentest-report",
		TemplateLabel: "PentAGI Penetration Testing Report",
		Labels:        pqtype.NullRawMessage{RawMessage: blob, Valid: true},
		CreatedAt:     sql.NullTime{Time: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC), Valid: true},
		SectionsDone:  3,
		SectionsTotal: 5,
	}
}

func composedSection(id int64, key, title string, position int32) database.ReportSection {
	return database.ReportSection{
		ID:       id,
		ReportID: 7,
		Key:      key,
		Kind:     KindComposed,
		Title:    title,
		Status:   SectionDone,
		Position: position,
	}
}

func TestResolveLabelsFallsBackToEnglish(t *testing.T) {
	empty := resolveLabels(database.Report{})
	assert.Equal(t, "Metric", empty["metrics.metric"])

	broken := resolveLabels(database.Report{Labels: pqtype.NullRawMessage{RawMessage: []byte("{"), Valid: true}})
	assert.Equal(t, "Metric", broken["metrics.metric"])

	partial := resolveLabels(reportWithLabels(t, map[string]string{
		"metrics.metric": "Métrique",
		"metrics.value":  "   ",
	}))
	assert.Equal(t, "Métrique", partial["metrics.metric"], "translated key wins")
	assert.Equal(t, "Value", partial["metrics.value"], "a blank value keeps the default")
	assert.Equal(t, "Took", partial["metrics.took"], "an absent key keeps the default")
}

func TestRenderLabelKeepsUnknownPlaceholders(t *testing.T) {
	labels := map[string]string{"x": "durée [[took]] sur [[missing]]"}

	assert.Equal(t, "durée 12m sur [[missing]]", renderLabel(labels, "x", map[string]string{"took": "12m"}))
	assert.Equal(t, "", renderLabel(labels, "absent", nil))
}

func TestComposeDocumentLocalizesComposedSections(t *testing.T) {
	report := reportWithLabels(t, map[string]string{
		"cover.generated":           "Généré le [[date]].",
		"header_footer.footer_text": "Généré par PentAGI · [[date]]",
		"header_footer.line":        "En-tête : \"[[header]]\" | Pied : \"[[footer]]\"",
		"metrics.sections":          "Sections écrites",
		"metrics.took":              "Durée",
	})
	sections := []database.ReportSection{
		composedSection(1, "cover-page", "Page de couverture", 1),
		composedSection(2, "engagement-metrics", "Métriques", 2),
		composedSection(3, "page-header-footer", "En-tête et pied de page", 3),
	}

	out := composeDocument(report, &Template{Key: report.TemplateKey}, sections)

	assert.Contains(t, out, "Généré le 2026-10-08 12:00 UTC.")
	assert.Contains(t, out, "| Durée |")
	assert.Contains(t, out, "| Sections écrites |")
	assert.Contains(t, out, "En-tête : \"Rapport Paperwork\"")
	assert.NotContains(t, out, "Generated ")
}

func TestRankingRowsMatchAggregateByKeyNotTitle(t *testing.T) {
	// The parent titles are translated; only the keys identify the aggregates.
	sections := []database.ReportSection{
		{ID: 10, ReportID: 7, Key: "detailed-findings", Kind: KindAggregate, Title: "Constats détaillés", Position: 1},
		{ID: 11, ReportID: 7, ParentID: sql.NullInt64{Int64: 10, Valid: true}, Key: "f1", Kind: KindLLM, Title: "Injection SQL", Status: SectionDone, Position: 1},
		{ID: 12, ReportID: 7, Key: "recommendations", Kind: KindAggregate, Title: "Recommandations", Position: 2},
		{ID: 13, ReportID: 7, ParentID: sql.NullInt64{Int64: 12, Valid: true}, Key: "r1", Kind: KindLLM, Title: "Restreindre l'exposition", Status: SectionDone, Position: 1},
		{ID: 14, ReportID: 7, Key: "executive-summary", Kind: KindLLM, Title: "Résumé", Status: SectionDone, Position: 3},
	}

	rows := rankingRows(&numberer{}, sections)

	require.Len(t, rows, 2)
	assert.Equal(t, rankingRow{where: "Constats détaillés", item: "Injection SQL"}, rows[0])
	assert.Equal(t, rankingRow{where: "Recommandations", item: "Restreindre l'exposition"}, rows[1])
}

func TestDecodeLabelsLenient(t *testing.T) {
	assert.Nil(t, decodeLabels(nil))
	assert.Nil(t, decodeLabels([]byte("123")))
	assert.Nil(t, decodeLabels([]byte("[1,2]")))
	assert.Nil(t, decodeLabels([]byte("{")))
	assert.Equal(t, map[string]string{"a": "b"}, decodeLabels([]byte(`{"a":"b"}`)))
	assert.Equal(t, map[string]string{"a": "b"}, decodeLabels([]byte(`"{\"a\":\"b\"}"`)))
}

func TestPlanDecodesTranslatedTitlesAndLabels(t *testing.T) {
	raw := `{"title":"Rapport","sections_estimate":3,"labels":"{\"metrics.took\":\"Durée\"}","sections":[{"key":"executive-summary","action":"include","title":"Résumé exécutif"}]}`

	var got plan
	require.NoError(t, json.Unmarshal([]byte(raw), &got))

	assert.Equal(t, "Durée", got.Labels["metrics.took"], "a stringified labels object is accepted")
	require.Len(t, got.Sections, 1)
	assert.Equal(t, "Résumé exécutif", got.Sections[0].Title)
}

// The planner prompt hardcodes the label keys it must translate; a key added to
// the Go set without the prompt (or vice versa) would silently stay English.
func TestPlannerPromptListsEveryLabelKey(t *testing.T) {
	raw, err := os.ReadFile("../templates/prompts/report_planner.tmpl")
	require.NoError(t, err)

	prompt := string(raw)
	for _, key := range labelKeySet() {
		assert.True(t, strings.Contains(prompt, key+" ="), "planner prompt is missing the label key %q", key)
	}
}
