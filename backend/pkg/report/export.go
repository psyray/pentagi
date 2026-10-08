package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"pentagi/pkg/database"
	obs "pentagi/pkg/observability"
)

// exportPlaceholders are the names a section body may reference; they resolve
// from the report data at read/export time (plan § 6.5.2). Unknown
// placeholders are left untouched, never swallowed.
var placeHolderRx = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

// BuildDocument assembles the full markdown document of one report: title,
// numbered sections in document order, variables resolved, composed sections
// computed. It is the single render path for reading and exporting (MD and
// the client-side PDF walk it).
func BuildDocument(
	ctx context.Context,
	db database.Querier,
	reportID int64,
) (string, error) {
	ctx, span := obs.Observer.NewSpan(ctx, obs.SpanKindInternal, "report.buildDocument")
	defer span.End()

	report, err := db.GetReportByID(ctx, reportID)
	if err != nil {
		return "", fmt.Errorf("failed to load report %d: %w", reportID, err)
	}
	sections, err := db.GetReportSections(ctx, reportID)
	if err != nil {
		return "", fmt.Errorf("failed to load report sections: %w", err)
	}
	tpl, tplErr := GetTemplate(report.TemplateKey)
	if tplErr != nil {
		tpl = &Template{Key: report.TemplateKey}
	}

	return composeDocument(report, tpl, sections), nil
}

func composeDocument(report database.Report, tpl *Template, sections []database.ReportSection) string {
	b := &strings.Builder{}
	title := strings.TrimSpace(report.Title)
	if title == "" {
		title = strings.TrimSpace(report.TemplateLabel)
	}
	fmt.Fprintf(b, "# %s\n\n", title)

	labels := resolveLabels(report)
	numberer := newNumberer(tpl, sections)
	for _, st := range orderedSections(sections, sql.NullInt64{}) {
		if st.Hidden {
			continue
		}
		writeSection(b, numberer, report, labels, st, sections)
	}

	return normalizeDocument(b.String())
}

// newNumberer assigns the document numbers for one render pass: top-level
// sections carry 1..N unless the template marks them unnumbered; children and
// deeper levels follow "N.M", "N.M.K".
func newNumberer(tpl *Template, sections []database.ReportSection) *numberer {
	numberer := &numberer{
		byID:     make(map[int64]string, len(sections)),
		topCount: 0,
	}

	for _, st := range orderedSections(sections, sql.NullInt64{}) {
		if st.Hidden {
			continue
		}
		unnumbered := false
		if tpl != nil {
			for i := range tpl.Sections {
				if tpl.Sections[i].Key == st.Key {
					unnumbered = tpl.Sections[i].Unnumbered()
				}
			}
		}

		var number string
		if unnumbered {
			number = ""
		} else {
			numberer.topCount++
			number = fmt.Sprintf("%d.", numberer.topCount)
		}
		numberer.byID[st.ID] = number
		assignChildren(numberer, sections, st.ID, number)
	}

	return numberer
}

func assignChildren(numberer *numberer, sections []database.ReportSection, parentID int64, parentNumber string) {
	for i, st := range orderedSections(sections, int64Null(parentID)) {
		var number string
		if parentNumber != "" {
			_ = i
			number = fmt.Sprintf("%s%d.", parentNumber, i+1)
		} else if parentNumber == "" {
			// Children of unnumbered front matter stay unnumbered.
			number = ""
		}
		numberer.byID[st.ID] = number
		assignChildren(numberer, sections, st.ID, number)
	}
}

type numberer struct {
	byID     map[int64]string
	topCount int
}

func (n *numberer) title(st database.ReportSection) string {
	prefix := n.byID[st.ID]
	if prefix == "" {
		return st.Title
	}
	return prefix + " " + st.Title
}

func writeSection(
	b *strings.Builder,
	numberer *numberer,
	report database.Report,
	labels map[string]string,
	st database.ReportSection,
	sections []database.ReportSection,
) {
	fmt.Fprintf(b, "## %s\n\n", numberer.title(st))

	switch st.Kind {
	case KindComposed, KindAggregate:
		// Computed at export; aggregate children are written by their owners.
		if st.Kind == KindComposed {
			b.WriteString(composeSectionBody(numberer, report, labels, st, sections))
		}
	default:
		switch st.Status {
		case SectionDone, SectionFailed:
			b.WriteString(resolvePlaceholders(st.Content, st))
			b.WriteString("\n\n")
		case SectionWriting:
			b.WriteString(resolvePlaceholders(st.Content, st))
			fmt.Fprintf(b, "\n\n*Writing section \\\"%s\\\".*\n\n", escapeTitle(st.Title))
		case SectionNotWritten:
			b.WriteString("*Not written.*\n\n")
		default:
			fmt.Fprintf(b, "*Not written yet.*\n\n")
		}
	}

	// Children follow, in document order.
	for _, child := range orderedSections(sections, int64Null(st.ID)) {
		if child.Hidden {
			continue
		}
		writeSection(b, numberer, report, labels, child, sections)
	}
}

// composeSectionBody computes the body of a composed section from the report
// data (and, for rankings, from the section contents). Every composed section
// stays honest to the document: it only aggregates what exists.
func composeSectionBody(
	numberer *numberer,
	report database.Report,
	labels map[string]string,
	st database.ReportSection,
	sections []database.ReportSection,
) string {
	switch st.Key {
	case "cover-page":
		return composeCover(report, labels)
	case "engagement-metrics":
		return composeMetrics(report, labels)
	case "risk-rankings":
		return composeRankings(numberer, labels, sections)
	case "document-version":
		return composeVersion(report, labels)
	case "closing":
		return composeClosing(report, labels)
	case "page-header-footer":
		return composeHeaderFooter(report, labels)
	default:
		return fmt.Sprintf("*Composed from the document when the report is exported (unknown composer %q).*\n\n", st.Key)
	}
}

func nullTimeOrNow(value sql.NullTime) time.Time {
	if value.Valid {
		return value.Time
	}
	return time.Now()
}

func composeCover(report database.Report, labels map[string]string) string {
	b := &strings.Builder{}
	branding := ResolveReportBranding(report)
	fmt.Fprintf(b, "**%s**\n\n", escapeTitle(branding.Title))
	if branding.Subtitle != "" {
		fmt.Fprintf(b, "*%s*\n\n", escapeTitle(branding.Subtitle))
	}
	b.WriteString(renderLabel(labels, "cover.engagement", map[string]string{
		"id":       fmt.Sprintf("%d", report.ID),
		"template": escapeTitle(report.TemplateLabel),
	}))
	b.WriteString("\n\n")
	b.WriteString(renderLabel(labels, "cover.generated", map[string]string{
		"date": nullTimeOrNow(report.CreatedAt).UTC().Format("2006-01-02 15:04 UTC"),
	}))
	return b.String() + "\n"
}

func composeMetrics(report database.Report, labels map[string]string) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "| %s | %s |\n|---|---|\n",
		renderLabel(labels, "metrics.metric", nil), renderLabel(labels, "metrics.value", nil))
	fmt.Fprintf(b, "| %s | %s |\n", renderLabel(labels, "metrics.took", nil), tookOf(report))
	fmt.Fprintf(b, "| %s | %s |\n", renderLabel(labels, "metrics.agent_time", nil), humanDuration(controlPlusContent(report)))
	fmt.Fprintf(b, "| %s | %s |\n", renderLabel(labels, "metrics.tokens", nil),
		renderLabel(labels, "metrics.tokens_value", map[string]string{
			"in":  humanTokens(report.TokensIn),
			"out": humanTokens(report.TokensOut),
		}))
	fmt.Fprintf(b, "| %s | %s |\n", renderLabel(labels, "metrics.sections", nil),
		renderLabel(labels, "metrics.sections_value", map[string]string{
			"done":     fmt.Sprintf("%d", report.SectionsDone),
			"estimate": fmt.Sprintf("%d", report.SectionsEstimate),
			"total":    fmt.Sprintf("%d", report.SectionsTotal),
		}))
	return b.String() + "\n"
}

func composeRankings(numberer *numberer, labels map[string]string, sections []database.ReportSection) string {
	b := &strings.Builder{}
	rows := rankingRows(numberer, sections)
	if len(rows) == 0 {
		return renderLabel(labels, "rankings.empty", nil) + "\n\n"
	}
	fmt.Fprintf(b, "| %s | %s | %s |\n|---|---|---|\n",
		renderLabel(labels, "rankings.rank", nil),
		renderLabel(labels, "rankings.where", nil),
		renderLabel(labels, "rankings.item", nil))
	for i, row := range rows {
		fmt.Fprintf(b, "| %d | %s | %s |\n", i+1, escapeTitle(row.where), escapeTitle(row.item))
	}
	return b.String() + "\n"
}

type rankingRow struct {
	where string
	item  string
}

// rankingRows walks the document for the ranking material: v1 reads the
// severity mentions out of finding titles (a cheap, explicit heuristic; the
// richer ranking is carried by the findings themselves). It matches the parent
// aggregates by KEY, never by their title text, so a translated report keeps
// working (plan § 6.11).
func rankingRows(numberer *numberer, sections []database.ReportSection) []rankingRow {
	rows := make([]rankingRow, 0, 8)
	_ = numberer
	for i := range sections {
		st := &sections[i]
		if st.Hidden || st.Status != SectionDone || st.Kind != KindLLM {
			continue
		}
		parent := findSection(sections, st.ParentID)
		if parent == nil || (parent.Key != "detailed-findings" && parent.Key != "recommendations") {
			continue
		}
		rows = append(rows, rankingRow{where: parent.Title, item: st.Title})
	}
	return rows
}

// findSection returns the section with the given id, or nil.
func findSection(sections []database.ReportSection, id sql.NullInt64) *database.ReportSection {
	if !id.Valid {
		return nil
	}
	for i := range sections {
		if sections[i].ID == id.Int64 {
			return &sections[i]
		}
	}
	return nil
}

func findTitle(sections []database.ReportSection, id sql.NullInt64) string {
	if found := findSection(sections, id); found != nil {
		return found.Title
	}
	return ""
}

func composeVersion(report database.Report, labels map[string]string) string {
	template := fmt.Sprintf("%s (%s)", report.TemplateKey, escapeTitle(report.TemplateLabel))
	return renderLabel(labels, "version.document_version", nil) + "\n" +
		renderLabel(labels, "version.template", map[string]string{"template": template}) + "\n" +
		renderLabel(labels, "version.generated", map[string]string{
			"date": nullTimeOrNow(report.CreatedAt).UTC().Format("2006-01-02"),
		}) + "\n\n"
}

func composeClosing(report database.Report, labels map[string]string) string {
	return renderLabel(labels, "closing.end", map[string]string{
		"agent_time": humanDuration(controlPlusContent(report)),
		"tokens_in":  humanTokens(report.TokensIn),
		"tokens_out": humanTokens(report.TokensOut),
		"took":       tookOf(report),
	}) + "\n\n"
}

func composeHeaderFooter(report database.Report, labels map[string]string) string {
	branding := ResolveReportBranding(report)
	return renderLabel(labels, "header_footer.line", map[string]string{
		"footer": escapeTitle(branding.Footer),
		"header": escapeTitle(branding.Header),
	}) + "\n\n"
}

func tookOf(report database.Report) string {
	if report.FinishedAt.Valid && report.CreatedAt.Valid {
		return humanDuration(report.FinishedAt.Time.Sub(report.CreatedAt.Time).Seconds())
	}
	return humanDuration(controlPlusContent(report))
}

func controlPlusContent(report database.Report) float64 {
	return report.ControlUsedSeconds + report.ContentUsedSeconds
}

func humanTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func escapeTitle(text string) string {
	text = strings.ReplaceAll(text, "\\", "\\\\")
	return strings.ReplaceAll(text, "\"", "\\\"")
}

// resolvePlaceholders substitutes section variables ({"name","value"} pairs,
// JSON-typed) into the body. Unreferenced names are left as written.
func resolvePlaceholders(content string, st database.ReportSection) string {
	if len(st.Variables) == 0 {
		return content
	}

	var pairs []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(st.Variables, &pairs); err != nil {
		return content
	}

	byName := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		if pair.Name == "" {
			continue
		}
		if len(pair.Value) == 0 {
			byName[pair.Name] = ""
			continue
		}
		byName[pair.Name] = string(pair.Value)
	}

	return placeHolderRx.ReplaceAllStringFunc(content, func(match string) string {
		name := placeHolderRx.FindStringSubmatch(match)[1]
		if value, ok := byName[name]; ok {
			return value
		}
		return match
	})
}

// normalizeDocument trims the doubled blank lines the writer blocks leave.
func normalizeDocument(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	blanks := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blanks++
			if blanks > 2 {
				continue
			}
		} else {
			blanks = 0
		}
		out = append(out, line)
	}

	now := strings.TrimSpace(strings.Join(out, "\n")) + "\n"
	return now
}
