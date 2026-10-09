package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"pentagi/pkg/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeQuerier implements only the query surface the engine needs; anything
// else panics, which is exactly what a test wants to see.
type fakeQuerier struct {
	database.Querier

	tasks     []database.Task
	subtasks  []database.Subtask
	msglogs   []database.Msglog
	screens   []database.Screenshot
	reports   map[int64]database.Report
	sections  map[int64]database.ReportSection
	nextSecID int64
	logs      []database.ReportLog

	toolcalls map[int64][]database.Toolcall
	termlogs  map[int64][]database.Termlog
	shots     map[int64][]database.Screenshot
}

func (q *fakeQuerier) GetFlow(_ context.Context, flowID int64) (database.Flow, error) {
	if q.reports == nil {
		return database.Flow{}, fmt.Errorf("flow %d not found", flowID)
	}
	return database.Flow{ID: flowID, Title: "Test engagement"}, nil
}

func (q *fakeQuerier) GetFlowTasks(_ context.Context, flowID int64) ([]database.Task, error) {
	return q.tasks, nil
}

func (q *fakeQuerier) GetFlowSubtasks(_ context.Context, flowID int64) ([]database.Subtask, error) {
	return q.subtasks, nil
}

func (q *fakeQuerier) GetFlowMsgLogs(_ context.Context, flowID int64) ([]database.Msglog, error) {
	return q.msglogs, nil
}

func (q *fakeQuerier) GetFlowScreenshots(_ context.Context, flowID int64) ([]database.Screenshot, error) {
	return q.screens, nil
}

func (q *fakeQuerier) GetSubtaskToolcalls(_ context.Context, subtaskID sql.NullInt64) ([]database.Toolcall, error) {
	return q.toolcalls[subtaskID.Int64], nil
}

func (q *fakeQuerier) GetSubtaskTermLogs(_ context.Context, subtaskID sql.NullInt64) ([]database.Termlog, error) {
	return q.termlogs[subtaskID.Int64], nil
}

func (q *fakeQuerier) GetSubtaskScreenshots(_ context.Context, subtaskID sql.NullInt64) ([]database.Screenshot, error) {
	return q.shots[subtaskID.Int64], nil
}

func (q *fakeQuerier) SetReportSectionSources(_ context.Context, arg database.SetReportSectionSourcesParams) (database.ReportSection, error) {
	row, ok := q.sections[arg.ID]
	if !ok {
		return database.ReportSection{}, fmt.Errorf("section %d not found", arg.ID)
	}
	row.SourceSubtaskIds = arg.SourceSubtaskIds
	q.sections[arg.ID] = row
	return row, nil
}

func (q *fakeQuerier) SetReportSectionEvidence(_ context.Context, arg database.SetReportSectionEvidenceParams) (database.ReportSection, error) {
	row, ok := q.sections[arg.ID]
	if !ok {
		return database.ReportSection{}, fmt.Errorf("section %d not found", arg.ID)
	}
	row.EvidenceRefs = arg.EvidenceRefs
	q.sections[arg.ID] = row
	return row, nil
}

func (q *fakeQuerier) AppendReportSectionWarning(_ context.Context, arg database.AppendReportSectionWarningParams) (database.ReportSection, error) {
	row, ok := q.sections[arg.ID]
	if !ok {
		return database.ReportSection{}, fmt.Errorf("section %d not found", arg.ID)
	}
	row.Warnings = append(row.Warnings, arg.ArrayAppend)
	q.sections[arg.ID] = row
	return row, nil
}

func (q *fakeQuerier) GetReport(_ context.Context, arg database.GetReportParams) (database.Report, error) {
	row, ok := q.reports[arg.ID]
	if !ok {
		return database.Report{}, fmt.Errorf("report %d not found", arg.ID)
	}
	return row, nil
}

func (q *fakeQuerier) GetReportByID(_ context.Context, reportID int64) (database.Report, error) {
	row, ok := q.reports[reportID]
	if !ok {
		return database.Report{}, fmt.Errorf("report %d not found", reportID)
	}
	return row, nil
}

func (q *fakeQuerier) GetReportSections(_ context.Context, _ int64) ([]database.ReportSection, error) {
	out := make([]database.ReportSection, 0, len(q.sections))
	for id := int64(1); id <= q.nextSecID; id++ {
		if row, ok := q.sections[id]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func (q *fakeQuerier) GetReportLogs(_ context.Context, arg database.GetReportLogsParams) ([]database.ReportLog, error) {
	if int64(len(q.logs)) < arg.Limit {
		return q.logs, nil
	}
	return q.logs[:arg.Limit], nil
}

func (q *fakeQuerier) CreateReportSection(_ context.Context, arg database.CreateReportSectionParams) (database.ReportSection, error) {
	q.nextSecID++
	row := database.ReportSection{
		ID:           q.nextSecID,
		ReportID:     arg.ReportID,
		ParentID:     arg.ParentID,
		Position:     arg.Position,
		Key:          arg.Key,
		Kind:         arg.Kind,
		Title:        arg.Title,
		Status:       SectionQueued,
		Instructions: arg.Instructions,
	}
	q.sections[row.ID] = row
	return row, nil
}

func (q *fakeQuerier) SetReportSectionStatus(_ context.Context, arg database.SetReportSectionStatusParams) (database.ReportSection, error) {
	row, ok := q.sections[arg.ID]
	if !ok {
		return database.ReportSection{}, fmt.Errorf("section %d not found", arg.ID)
	}
	row.Status = arg.Status
	q.sections[arg.ID] = row
	return row, nil
}

func (q *fakeQuerier) SetReportPlan(_ context.Context, arg database.SetReportPlanParams) (database.Report, error) {
	row := q.reports[arg.ID]
	row.Status = arg.Status
	row.SectionsEstimate = arg.SectionsEstimate
	row.SectionsTotal = arg.SectionsTotal
	if arg.Title != "" {
		row.Title = arg.Title
	}
	q.reports[arg.ID] = row
	return row, nil
}

func (q *fakeQuerier) UpdateReportStatus(_ context.Context, arg database.UpdateReportStatusParams) (database.Report, error) {
	row := q.reports[arg.ID]
	row.Status = arg.Status
	q.reports[arg.ID] = row
	return row, nil
}

func (q *fakeQuerier) SetReportEstimate(_ context.Context, arg database.SetReportEstimateParams) (database.Report, error) {
	row := q.reports[arg.ID]
	row.SectionsEstimate = arg.SectionsEstimate
	q.reports[arg.ID] = row
	return row, nil
}

func (q *fakeQuerier) UpdateReportProgress(_ context.Context, arg database.UpdateReportProgressParams) (database.Report, error) {
	row := q.reports[arg.ID]
	row.SectionsDone = arg.SectionsDone
	q.reports[arg.ID] = row
	return row, nil
}

func (q *fakeQuerier) SyncReportSectionCounts(_ context.Context, reportID sql.NullInt64) (database.Report, error) {
	row := q.reports[reportID.Int64]
	total, done := 0, 0
	for _, st := range q.sections {
		if st.ReportID != reportID.Int64 || st.Status == SectionSkipped {
			continue
		}
		total++
		if st.Status == SectionDone {
			done++
		}
	}
	row.SectionsTotal = int32(total)
	row.SectionsDone = int32(done)
	q.reports[reportID.Int64] = row
	return row, nil
}

func (q *fakeQuerier) SetReportStartedAt(_ context.Context, id int64) (database.Report, error) {
	row := q.reports[id]
	row.StartedAt = sql.NullTime{Time: time.Now(), Valid: true}
	q.reports[id] = row
	return row, nil
}

func (q *fakeQuerier) CreateReportLog(_ context.Context, arg database.CreateReportLogParams) (database.ReportLog, error) {
	row := database.ReportLog{
		ID:        int64(len(q.logs) + 1),
		ReportID:  arg.ReportID,
		SectionID: arg.SectionID,
		Kind:      arg.Kind,
		Message:   arg.Message,
		Payload:   arg.Payload,
	}
	q.logs = append(q.logs, row)
	return row, nil
}

func (q *fakeQuerier) SetReportSectionPosition(_ context.Context, arg database.SetReportSectionPositionParams) (database.ReportSection, error) {
	row, ok := q.sections[arg.ID]
	if !ok {
		return database.ReportSection{}, fmt.Errorf("section %d not found", arg.ID)
	}
	row.Position = arg.Position
	q.sections[arg.ID] = row
	return row, nil
}

func (q *fakeQuerier) SetReportSectionHidden(_ context.Context, arg database.SetReportSectionHiddenParams) (database.ReportSection, error) {
	row, ok := q.sections[arg.ID]
	if !ok {
		return database.ReportSection{}, fmt.Errorf("section %d not found", arg.ID)
	}
	row.Hidden = arg.Hidden
	q.sections[arg.ID] = row
	return row, nil
}

func (q *fakeQuerier) UpdateReportSectionContent(_ context.Context, arg database.UpdateReportSectionContentParams) (database.ReportSection, error) {
	row, ok := q.sections[arg.ID]
	if !ok {
		return database.ReportSection{}, fmt.Errorf("section %d not found", arg.ID)
	}
	row.Content = arg.Content
	row.Status = arg.Status
	q.sections[arg.ID] = row
	return row, nil
}

func (q *fakeQuerier) UpdateReportSectionFields(_ context.Context, arg database.UpdateReportSectionFieldsParams) (database.ReportSection, error) {
	row, ok := q.sections[arg.ID]
	if !ok {
		return database.ReportSection{}, fmt.Errorf("section %d not found", arg.ID)
	}
	row.Title = arg.Title
	row.Instructions = arg.Instructions
	row.Warnings = arg.Warnings
	q.sections[arg.ID] = row
	return row, nil
}

func TestLoadDefaultTemplate_Valid(t *testing.T) {
	tpl, err := LoadTemplate("pentagi-default-pentest-report")
	require.NoError(t, err)

	assert.Equal(t, "pentagi-default-pentest-report", tpl.Key)
	assert.Equal(t, "PentAGI Penetration Testing Report", tpl.Label)
	require.NotEmpty(t, tpl.Sections)

	keys := make(map[string]struct{}, len(tpl.Sections))
	for _, node := range tpl.Sections {
		require.NotEmpty(t, node.Key)
		require.NotEmpty(t, node.Title)
		require.NotEqual(t, "", node.Kind, "section %s has no kind", node.Key)
		keys[node.Key] = struct{}{}
	}

	// The cover is front matter (unnumbered), the findings chapter carries
	// its alternatives.
	assert.Contains(t, keys, "cover-page")
	assert.Contains(t, keys, "executive-summary")
	assert.Contains(t, keys, "detailed-findings")
	assert.Contains(t, keys, "recommendations")

	for _, node := range tpl.Sections {
		if node.Key == "cover-page" {
			assert.True(t, node.Unnumbered(), "the cover page must be unnumbered")
		}
		if node.Key == "detailed-findings" {
			assert.Equal(t, KindAggregate, node.Kind)
			assert.NotEmpty(t, node.EmptyChildren, "the findings chapter carries its fallback children")
		}
		if node.Key == "executive-summary" {
			assert.True(t, node.Defer, "the executive summary is written after the body")
		}
	}
}

func TestLoadTemplate_MissingFile(t *testing.T) {
	_, err := LoadTemplate("no-such-template")
	assert.Error(t, err)
}

// testTemplate: one composed (unnumbered), one aggregate with one alternative,
// one composed numbered, one deferred llm.
func testTemplate() *Template {
	numbered := true
	return &Template{
		Key:      "test-template",
		Label:    "Test Template",
		Version:  1,
		Defaults: Lengths{Length: "100-200 words", ChildLength: "80-120 words"},
		Sections: []SectionNode{
			{Key: "cover-page", Kind: KindComposed, Title: "Cover Page"},
			{
				Key:               "detailed-findings",
				Kind:              KindAggregate,
				Title:             "Detailed Findings",
				ChildrenSource:    "findings",
				ChildInstructions: "Write the finding.",
				ChildLength:       "80-120 words",
				EmptyChildren:     []ChildNode{{Key: "no-findings-alt", Title: "No Findings Enumerated"}},
				Numbered:          &numbered,
			},
			{Key: "engagement-metrics", Kind: KindComposed, Title: "Engagement Metrics", Numbered: &numbered},
			{
				Key:          "executive-summary",
				Kind:         KindLLM,
				Title:        "Executive Summary",
				Defer:        true,
				Instructions: "Summarize.",
				Numbered:     &numbered,
			},
		},
	}
}

func TestWriteOrder_SplitsDeferredFromBody(t *testing.T) {
	tpl := testTemplate()

	sections := []database.ReportSection{
		{ID: 1, Key: "cover-page", Kind: KindComposed, Status: SectionQueued, Position: 1},
		{ID: 2, Key: "detailed-findings", Kind: KindAggregate, Status: SectionQueued, Position: 2},
		{ID: 3, Key: "child-finding", Kind: KindLLM, Status: SectionQueued, Position: 1,
			ParentID: int64Null(2)},
		{ID: 4, Key: "engagement-metrics", Kind: KindComposed, Status: SectionQueued, Position: 3},
		{ID: 5, Key: "executive-summary", Kind: KindLLM, Status: SectionQueued, Position: 4},
	}

	body, deferred := writeOrder(sections, tpl)

	got := make([]string, 0, len(body))
	for _, st := range body {
		got = append(got, st.Key)
	}
	// Children follow their aggregate parent; the deferred summary is out.
	assert.Equal(t, []string{"cover-page", "detailed-findings", "child-finding", "engagement-metrics"}, got)
	if len(deferred) != 1 {
		t.Fatalf("expected exactly one deferred section, got %d", len(deferred))
	}
	assert.Equal(t, "executive-summary", deferred[0].Key)
}

// applyPlan applies the planner decisions to the fake querier's rows.
func TestApplyPlan_InstantiatesChildrenAndSkipsAlternatives(t *testing.T) {
	tpl := testTemplate()

	q := &fakeQuerier{
		reports: map[int64]database.Report{}, sections: map[int64]database.ReportSection{},
	}
	report := database.Report{ID: 7, FlowID: 3, UserID: 1, Status: StatusPlanning, TemplateKey: tpl.Key, Language: "English"}
	q.reports[report.ID] = report

	// The skeleton: two top-level rows.
	for i, key := range []string{"cover-page", "detailed-findings"} {
		node := tpl.Sections[i]
		row := database.ReportSection{
			ID: int64(i + 1), ReportID: report.ID, Position: int32(i + 1),
			Key: key, Kind: node.Kind, Title: node.Title, Status: SectionQueued,
		}
		if key == "detailed-findings" {
			row.ParentID = sql.NullInt64{}
			node = tpl.Sections[i]
		}
		q.sections[row.ID] = row
	}
	q.nextSecID = int64(len(q.sections))

	// The alternative under the aggregate.
	altRow := database.ReportSection{
		ID: 3, ReportID: report.ID, ParentID: int64Null(2), Position: 1,
		Key: "no-findings-alt", Kind: KindLLM, Title: "No Findings Enumerated", Status: SectionQueued,
	}
	q.sections[altRow.ID] = altRow
	q.nextSecID = 3

	skeleton, err := q.GetReportSections(context.Background(), report.ID)
	require.NoError(t, err)

	w := &worker{
		deps:   Deps{DB: q},
		report: report,
		tpl:    tpl,
		ctx:    context.Background(),
	}

	got := plan{
		Title:    "Better title from the planner",
		Estimate: 4,
		Sections: []planSection{
			{Key: "cover-page", Action: planActionInclude},
			{
				Key:    "detailed-findings",
				Action: planActionInclude,
				Children: []planChild{
					{Title: "SQL injection on /users", Summary: "A confirmed injection.", SourceSubtaskIDs: []int64{11, 12}},
					{Title: "Weak TLS configuration", Summary: "TLS 1.0 accepted."},
				},
			},
		},
	}

	require.NoError(t, w.applyPlan(context.Background(), got, skeleton))

	// The planner children were created under the aggregate (IDs 4, 5) in
	// document order; the alternative moved behind them.
	child1 := q.sections[4]
	assert.Equal(t, "child-sql-injection-on-users", child1.Key)
	assert.Equal(t, "SQL injection on /users", child1.Title)
	assert.Equal(t, KindLLM, child1.Kind)
	assert.Equal(t, int32(1), child1.Position)
	assert.Contains(t, child1.Instructions.String, "Record summary: A confirmed injection.")
	assert.Equal(t, []int64{11, 12}, child1.SourceSubtaskIds)

	child2 := q.sections[5]
	assert.Equal(t, int32(2), child2.Position)

	alternative := q.sections[3]
	assert.Equal(t, SectionSkipped, alternative.Status)
	assert.Equal(t, int32(3), alternative.Position)

	// Top-level decisions and the report row.
	cover := q.sections[1]
	assert.Equal(t, SectionQueued, cover.Status)
	aggregate := q.sections[2]
	assert.Equal(t, SectionQueued, aggregate.Status)

	reportRow := q.reports[report.ID]
	assert.Equal(t, "Better title from the planner", reportRow.Title)
	assert.Equal(t, StatusGenerating, reportRow.Status)
	assert.Equal(t, int32(4), reportRow.SectionsEstimate)
	assert.Equal(t, int32(4), reportRow.SectionsTotal)
}

func TestComposeDocument_Rendering(t *testing.T) {
	numbered := true
	unnumbered := false
	tpl := &Template{
		Key:      "test-template",
		Label:    "Test Template",
		Defaults: Lengths{Length: "100-200 words"},
		Sections: []SectionNode{
			{Key: "cover-page", Kind: KindComposed, Title: "Cover Page", Numbered: &unnumbered},
			{
				Key:          "findings",
				Kind:         KindLLM,
				Title:        "Findings",
				Numbered:     &numbered,
				Instructions: "",
			},
			{Key: "engagement-metrics", Kind: KindComposed, Title: "Engagement Metrics", Numbered: &numbered},
		},
	}

	q := &fakeQuerier{
		reports: map[int64]database.Report{}, sections: map[int64]database.ReportSection{},
	}
	report := database.Report{
		ID: 5, FlowID: 1, UserID: 2, Status: StatusCompleted, Title: "The title",
		TemplateKey: tpl.Key, TemplateLabel: tpl.Label, Language: "English",
	}
	q.reports[report.ID] = report

	sections := []database.ReportSection{
		// A section with a variable placeholder resolved at render time.
		{ID: 1, ReportID: 5, Key: "cover-page", Kind: KindComposed, Title: "Cover Page",
			Position: 1},
		{
			ID: 2, ReportID: 5, Key: "findings", Kind: KindLLM, Title: "Findings",
			Status: SectionDone, Position: 2,
			Content:   "Impact: {{impact}} and 443 port notes.",
			Variables: rawVariables(map[string]any{"impact": 9.8}),
		},
		{ID: 3, ReportID: 5, Key: "badsection", Kind: KindLLM, Title: "Broken", Status: SectionFailed,
			Position: 3, Content: "partial"},
		{ID: 4, ReportID: 5, Key: "engagement-metrics", Kind: KindComposed, Title: "Engagement Metrics",
			Position: 4},
	}
	reported := report
	reported.SectionsTotal = 4
	q.reports[report.ID] = reported

	doc := composeDocument(report, tpl, sections)

	assert.Contains(t, doc, "# The title")
	// The cover is unnumbered, the body section carries number 1.
	assert.Contains(t, doc, "## Cover Page")
	assert.Contains(t, doc, "## 1. Findings")
	// The variable was resolved (JSON: "high" with quotes).
	assert.Contains(t, doc, "Impact: 9.8")
	// The metrics block is computed from the row data (numbered third: the
	// failed section still occupies its number).
	assert.Contains(t, doc, "## 3. Engagement Metrics")
	assert.Contains(t, doc, "Took")
}

func TestHasEvidenceSection(t *testing.T) {
	assert.True(t, hasEvidenceSection("Intro.\n\n### Evidence\n```\nnmap output\n```"))
	assert.True(t, hasEvidenceSection("### evidence\n..."), "the heading match is case-insensitive")
	assert.False(t, hasEvidenceSection("Intro without an evidence subsection."))
	assert.False(t, hasEvidenceSection(""), "an empty body never passes the gate")
}

func TestComposeDocument_NumbersChildrenUnderTheirParent(t *testing.T) {
	numbered := true
	tpl := &Template{
		Key: "test-template", Label: "Test Template",
		Sections: []SectionNode{
			{Key: "findings", Kind: KindLLM, Title: "Findings", Numbered: &numbered},
		},
	}
	report := database.Report{
		ID: 5, FlowID: 1, UserID: 2, Status: StatusCompleted, Title: "The title",
		TemplateKey: tpl.Key, TemplateLabel: tpl.Label, Language: "English",
	}
	sections := []database.ReportSection{
		{ID: 1, ReportID: 5, Key: "findings", Kind: KindLLM, Title: "Findings", Status: SectionDone, Position: 1},
		{ID: 2, ReportID: 5, ParentID: int64Null(1), Key: "child-a", Kind: KindLLM,
			Title: "Child A", Status: SectionDone, Position: 1, Content: "a"},
		{ID: 3, ReportID: 5, ParentID: int64Null(1), Key: "child-b", Kind: KindLLM,
			Title: "Child B", Status: SectionDone, Position: 2, Content: "b"},
	}

	doc := composeDocument(report, tpl, sections)

	assert.Contains(t, doc, "## 1. Findings")
	// Children carry the parent's number as a dotted prefix ("1.1."), not a
	// concatenation ("11.").
	assert.Contains(t, doc, "## 1.1. Child A")
	assert.Contains(t, doc, "## 1.2. Child B")
}

func rawVariables(values map[string]any) []byte {
	type variable struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
	}

	pairs := make([]variable, 0, len(values))
	for name, value := range values {
		pairs = append(pairs, variable{Name: name, Value: value})
	}
	blob, err := json.Marshal(pairs)
	if err != nil {
		panic(err)
	}
	return blob
}

func TestFilterSectionsByID(t *testing.T) {
	sections := []database.ReportSection{
		{ID: 1, Key: "a"},
		{ID: 2, Key: "b"},
		{ID: 3, Key: "c"},
	}

	filtered := filterSectionsByID(sections, []int64{3, 1})
	require.Len(t, filtered, 2)
	assert.Equal(t, int64(1), filtered[0].ID)
	assert.Equal(t, int64(3), filtered[1].ID)

	assert.Empty(t, filterSectionsByID(sections, []int64{9}))
	assert.Empty(t, filterSectionsByID(sections, nil))
}
