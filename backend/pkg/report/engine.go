package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/database/converter"
	"pentagi/pkg/graph/subscriptions"
	obs "pentagi/pkg/observability"
	"pentagi/pkg/providers"
	"pentagi/pkg/providers/pconfig"
	"pentagi/pkg/providers/provider"
	"pentagi/pkg/templates"

	"github.com/invopop/jsonschema"
	"github.com/sirupsen/logrus"
	"github.com/vxcontrol/langchaingo/llms"
	"github.com/vxcontrol/langchaingo/llms/streaming"
)

// planToolName is the single tool the planner node must call to emit the plan.
const planToolName = "report_plan"

// Node timeouts: a planner or writer that hangs must not pin the run forever.
const (
	planningTimeout = 15 * time.Minute
	writingTimeout  = 30 * time.Minute
)

const (
	planActionInclude = "include"
	planActionSkip    = "skip"
)

var reflector = &jsonschema.Reflector{
	DoNotReference: true,
	ExpandedStruct: true,
}

// plan is the payload of the single planner tool.
type plan struct {
	Title    string        `json:"title"`
	Estimate int           `json:"sections_estimate"`
	Sections []planSection `json:"sections"`
}

// UnmarshalJSON accepts the model's two known shapes for the sections field:
// the plain array and the (once seen) array stringified inside a JSON string.
func (p *plan) UnmarshalJSON(blob []byte) error {
	type alias struct {
		Title    string          `json:"title"`
		Estimate int             `json:"sections_estimate"`
		Sections json.RawMessage `json:"sections"`
	}

	parsed := alias{}
	if err := json.Unmarshal(blob, &parsed); err != nil {
		return err
	}

	p.Title = parsed.Title
	p.Estimate = parsed.Estimate
	p.Sections = nil

	payload := parsed.Sections
	if len(payload) > 0 && payload[0] == '"' {
		var asText string
		if err := json.Unmarshal(payload, &asText); err != nil {
			return fmt.Errorf("plan sections string payload: %w", err)
		}
		payload = []byte(asText)
	}
	if len(payload) > 0 && payload[0] != '[' && payload[0] != 'n' {
		return fmt.Errorf("plan sections neither array nor null")
	}
	if err := json.Unmarshal(payload, &p.Sections); err != nil {
		return fmt.Errorf("plan sections payload: %w", err)
	}
	return nil
}

type planSection struct {
	Key      string      `json:"key"`
	Action   string      `json:"action"`
	Children []planChild `json:"children,omitempty"`
}

type planChild struct {
	Key              string  `json:"key,omitempty"`
	Title            string  `json:"title"`
	Action           string  `json:"action,omitempty"`
	Summary          string  `json:"summary,omitempty"`
	Warning          string  `json:"warning,omitempty"`
	Instructions     string  `json:"instructions,omitempty"`
	Length           string  `json:"length,omitempty"`
	SourceSubtaskIDs []int64 `json:"source_subtask_ids,omitempty"`
}

func (p *plan) Validate() error {
	seen := make(map[string]struct{}, len(p.Sections))
	for _, section := range p.Sections {
		if section.Key == "" {
			return fmt.Errorf("plan section with an empty key")
		}
		if section.Action != planActionInclude && section.Action != planActionSkip {
			return fmt.Errorf("plan section %q: unknown action %q", section.Key, section.Action)
		}
		if _, dup := seen[section.Key]; dup {
			return fmt.Errorf("plan section %q duplicated", section.Key)
		}
		seen[section.Key] = struct{}{}
		for _, child := range section.Children {
			if strings.TrimSpace(child.Title) == "" {
				return fmt.Errorf("plan child of %q with an empty title", section.Key)
			}
		}
	}
	return nil
}

// renderOutline renders the document skeleton as plain text. The planner sees
// statuses plus one-line instructions; a writer sees only keys and titles so
// it does not read its neighbours as history.
func renderOutline(sections []database.ReportSection, withStatus bool) string {
	b := &strings.Builder{}

	var walk func(parentID sql.NullInt64, depth int)
	walk = func(parentID sql.NullInt64, depth int) {
		for _, st := range orderedSections(sections, parentID) {
			indent := strings.Repeat("  ", depth)
			fmt.Fprintf(b, "%s- key: %s\n", indent, st.Key)
			fmt.Fprintf(b, "%s  kind: %s\n", indent, st.Kind)
			fmt.Fprintf(b, "%s  title: %q\n", indent, st.Title)
			if withStatus {
				fmt.Fprintf(b, "%s  status: %s\n", indent, st.Status)
				if st.ParentID.Valid && st.Instructions.Valid && st.Instructions.String != "" {
					fmt.Fprintf(b, "%s  one_line: %s\n", indent, outlineOneLine(st.Instructions.String))
				}
			}
			walk(int64Null(st.ID), depth+1)
		}
	}
	walk(sql.NullInt64{}, 0)

	return strings.TrimRight(b.String(), "\n")
}

func outlineOneLine(text string) string {
	one := strings.Join(strings.Fields(text), " ")
	if len(one) > 160 {
		one = one[:160] + "..."
	}
	return one
}

// orderedSections returns the children of one parent (or the top level when
// parentID is null) in document order (position, then id).
func orderedSections(sections []database.ReportSection, parentID sql.NullInt64) []database.ReportSection {
	out := make([]database.ReportSection, 0, len(sections)/2)
	for _, st := range sections {
		if !sameParent(st.ParentID, parentID) {
			continue
		}
		out = append(out, st)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func sameParent(value, parent sql.NullInt64) bool {
	return value.Valid == parent.Valid && value.Int64 == parent.Int64
}

func int64Null(id int64) sql.NullInt64 {
	return sql.NullInt64{Valid: true, Int64: id}
}

// writeOrder returns the flat write order: top-level sections in document
// order, each followed by its children. Deferred nodes (Executive Summary)
// are reported separately so they are written after the body exists.
func writeOrder(sections []database.ReportSection, tpl *Template) (body, deferred []database.ReportSection) {
	byParent := make(map[int64][]database.ReportSection)
	for _, st := range sections {
		if st.ParentID.Valid {
			byParent[st.ParentID.Int64] = append(byParent[st.ParentID.Int64], st)
		}
	}
	for parent := range byParent {
		list := byParent[parent]
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Position != list[j].Position {
				return list[i].Position < list[j].Position
			}
			return list[i].ID < list[j].ID
		})
	}

	for _, st := range orderedSections(sections, sql.NullInt64{}) {
		if st.ParentID.Valid {
			continue
		}
		node := outlineNodeOf(tpl, st)
		switch st.Kind {
		case KindAggregate:
			body = append(body, st)
			body = append(body, byParent[st.ID]...)
		case KindComposed:
			body = append(body, st)
		default:
			if node != nil && node.Defer {
				deferred = append(deferred, st)
				continue
			}
			body = append(body, st)
		}
	}

	return body, deferred
}

func outlineNodeOf(tpl *Template, st database.ReportSection) *SectionNode {
	for i := range tpl.Sections {
		if tpl.Sections[i].Key == st.Key {
			return &tpl.Sections[i]
		}
	}
	return nil
}

// Deps is everything a report worker needs; the controller owns one instance.
type Deps struct {
	DB       database.Querier
	Cfg      *config.Config
	Provs    providers.ProviderController
	Prompter templates.Prompter
	Subs     subscriptions.SubscriptionsController
}

// worker runs one report: planner (unless already instantiated), then the
// write loop over the derived order, then finish. All state lives in the
// database; a server restart resumes from rows (LoadReports).
type worker struct {
	deps        Deps
	report      database.Report
	flowRow     database.Flow
	pub         subscriptions.FlowPublisher
	prv         provider.Provider
	tpl         *Template
	ctx         context.Context // run context: cancelled by Stop, bound to the LLM calls
	stateCtx    context.Context // surviving context for state writes (WithoutCancel)
	cancel      context.CancelFunc
	stopped     atomic.Bool
	digest      string
	controlUsed float64
	contentUsed float64
	tokensIn    int64
	tokensOut   int64
	doneCount   int32
	mx          sync.Mutex
	visionOnce  sync.Once
	vision      bool
	logger      *logrus.Entry
}

func newWorker(deps Deps, report database.Report) (*worker, error) {
	prvname := provider.ProviderName(report.ProviderName)
	prv, err := deps.Provs.GetProvider(context.Background(), prvname, report.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve provider %q for report %d: %w", prvname, report.ID, err)
	}

	tpl, err := GetTemplate(report.TemplateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load template %q: %w", report.TemplateKey, err)
	}

	flow, err := deps.DB.GetFlow(context.Background(), report.FlowID)
	if err != nil {
		return nil, fmt.Errorf("failed to load flow %d for report %d: %w", report.FlowID, report.ID, err)
	}

	w := &worker{
		deps:    deps,
		report:  report,
		flowRow: flow,
		pub:     deps.Subs.NewFlowPublisher(report.UserID, report.FlowID),
		prv:     prv,
		tpl:     tpl,
		ctx:     context.Background(),
		cancel:  func() {},
		logger: logrus.WithFields(logrus.Fields{
			"report_id": report.ID,
			"flow_id":   report.FlowID,
			"component": "report-worker",
		}),
	}
	// On resume the report row already carries the earlier session's
	// counters; the done count and token totals come from the rows so the
	// next refresh persists the whole run, not just its new slice.
	w.controlUsed = report.ControlUsedSeconds
	w.contentUsed = report.ContentUsedSeconds
	w.tokensIn = 0
	w.tokensOut = 0
	w.doneCount = 0
	if report.SectionsTotal > 0 {
		sections, err := deps.DB.GetReportSections(context.Background(), report.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to load report %d sections: %w", report.ID, err)
		}
		for _, st := range sections {
			if st.Status == SectionDone {
				w.doneCount++
			}
			w.tokensIn += st.TokensIn
			w.tokensOut += st.TokensOut
		}
	}

	return w, nil
}

// start wires the cancellable context and launches the run goroutine. Called
// once per worker lifecycle; LoadReports also goes through it.
func (w *worker) start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.ctx = ctx
	w.stateCtx = context.WithoutCancel(ctx)
	w.cancel = cancel

	go func() {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Errorf("report worker panic: %v", r)
				w.finish(StatusFailed, fmt.Sprintf("report worker panic: %v", r))
			}
		}()

		w.putLog(LogStatus, "report run started", nil)

		if w.report.SectionsTotal == 0 {
			if err := w.plan(ctx); err != nil {
				w.finish(StatusFailed, err.Error())
				return
			}
		}

		if err := w.writeAll(ctx); err != nil {
			if w.stopped.Load() || ctx.Err() != nil {
				w.setStopState()
				return
			}
			w.finish(StatusFailed, err.Error())
			return
		}

		if w.stopped.Load() || ctx.Err() != nil {
			w.setStopState()
			return
		}

		w.finish(StatusCompleted, "")
	}()
}

// plan runs the planner node and applies the returned outline actions.
func (w *worker) plan(ctx context.Context) error {
	_, span := obs.Observer.NewSpan(ctx, obs.SpanKindInternal, "report.plan")
	defer span.End()

	if _, err := w.deps.DB.UpdateReportStatus(ctx, database.UpdateReportStatusParams{
		ID: w.report.ID, Status: StatusPlanning,
	}); err != nil {
		return fmt.Errorf("failed to persist planning status: %w", err)
	}
	w.publishRun()

	ctx, cancel := context.WithTimeout(ctx, planningTimeout)
	defer cancel()

	digest, stats, err := buildDigest(ctx, w.deps.DB, w.deps.Cfg.ReportMaxRecordBytes, w.report.FlowID)
	if err != nil {
		return err
	}
	w.digest = digest
	w.putLog(LogControl, fmt.Sprintf(
		"engagement record ready: %d tasks, %d subtasks, %d reporter notes, %d screenshots, %d bytes",
		stats.Tasks, stats.Subtasks, stats.ReporterLogs, stats.Screenshots, stats.Bytes), nil)

	sections, err := w.loadSections()
	if err != nil {
		return fmt.Errorf("failed to load report sections: %w", err)
	}

	system, err := w.deps.Prompter.RenderTemplate(templates.PromptTypeReportPlanner, map[string]any{
		"Outline":                renderOutline(sections, true),
		"Record":                 w.digest,
		"Language":               w.report.Language,
		"Title":                  w.report.Title,
		"AdditionalInstructions": nullableText(w.report.AdditionalInstructions),
		"Notes":                  w.loadNotes(),
	})
	if err != nil {
		return fmt.Errorf("failed to render report planner prompt: %w", err)
	}

	chain := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, system),
		llms.TextParts(llms.ChatMessageTypeHuman,
			"Produce the report plan through the "+planToolName+" tool now."),
	}
	tools := []llms.Tool{{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        planToolName,
			Description: "Emit the instantiated report outline for this engagement",
			Parameters:  reflector.Reflect(&plan{}),
			Strict:      true,
		},
	}}

	start := time.Now()
	resp, err := w.prv.CallWithTools(ctx, pconfig.OptionsTypeSimple, chain, tools, nil)
	controlSeconds := time.Since(start).Seconds()
	if err != nil {
		if w.stopped.Load() || ctx.Err() != nil {
			return err
		}
		return fmt.Errorf("planner call failed: %w", err)
	}
	w.recordNodeUsage(true, resp, controlSeconds)

	var got plan
	if err := decodePlanCommand(resp, planToolName, &got); err != nil {
		return err
	}

	if err := w.applyPlan(ctx, got, sections); err != nil {
		return fmt.Errorf("failed to apply plan: %w", err)
	}

	w.putLog(LogControl, fmt.Sprintf("plan applied: outline carries %d sections (planner estimate %d)",
		w.report.SectionsTotal, got.Estimate), nil)

	return nil
}

// decodePlanCommand extracts the report_plan tool payload from a response.
func decodePlanCommand(resp *llms.ContentResponse, toolName string, into *plan) error {
	if resp == nil || len(resp.Choices) < 1 {
		return fmt.Errorf("empty planner response")
	}
	raw := ""
	for _, call := range resp.Choices[0].ToolCalls {
		if call.FunctionCall == nil || call.FunctionCall.Name != toolName {
			continue
		}
		raw = call.FunctionCall.Arguments
		break
	}
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("planner returned no %s tool call", toolName)
	}
	if err := json.Unmarshal([]byte(raw), into); err != nil {
		return fmt.Errorf("failed to decode planner payload: %w", err)
	}
	return into.Validate()
}

// applyPlan persists the planner decisions. The skeleton carries only template
// sections and their alternatives; the planner includes/skips what is there or
// attaches dynamic children under aggregates. A child whose key matches an
// existing sibling row (a template alternative) is included/skipped instead of
// inserted twice.
func (w *worker) applyPlan(ctx context.Context, got plan, sections []database.ReportSection) error {
	decision := make(map[string]*planSection, len(got.Sections))
	for i := range got.Sections {
		decision[got.Sections[i].Key] = &got.Sections[i]
	}

	// Dynamic children first: insert under each aggregate, then push the
	// template alternatives behind them (or mark them when the planner picked
	// one as the empty-state section).
	for i := range sections {
		st := &sections[i]
		if st.Kind != KindAggregate {
			continue
		}
		children := []planChild(nil)
		if entry, ok := decision[st.Key]; ok {
			children = entry.Children
		}

		alternatives := orderedSections(sections, int64Null(st.ID))
		var inserted int
		for _, child := range children {
			if _, exists := childByKey(alternatives, child.Key); exists {
				continue // alternatives are resolved by key below
			}
			if strings.TrimSpace(child.Key) == "" {
				child.Key = "child-" + outlineSlug(child.Title)
			}
			created, err := w.deps.DB.CreateReportSection(ctx, database.CreateReportSectionParams{
				ReportID:     w.report.ID,
				ParentID:     database.Int64ToNullInt64(&st.ID),
				Position:     int32(inserted + 1),
				Key:          child.Key,
				Kind:         KindLLM,
				Title:        child.Title,
				Instructions: nullInstructionText(planChildInstructions(child)),
			})
			if err != nil {
				return fmt.Errorf("failed to insert child %q under %s: %w", child.Title, st.Key, err)
			}
			if err := w.setSectionSources(ctx, created.ID, child.SourceSubtaskIDs); err != nil {
				return fmt.Errorf("failed to record sources for child %q: %w", child.Title, err)
			}
			inserted++
		}

		// Alternatives sit behind the dynamic children when they exist.
		if inserted > 0 {
			for ri, alt := range alternatives {
				if _, err := w.deps.DB.SetReportSectionPosition(ctx, database.SetReportSectionPositionParams{
					ID:       alt.ID,
					Position: int32(inserted + 1 + ri),
				}); err != nil {
					return fmt.Errorf("failed to reposition alternative %s: %w", alt.Key, err)
				}
			}
		}

		// Alternatives: skip when dynamic children exist or a different
		// alternative was chosen; include the one the planner picked (key
		// match) and default to the first one when the record supports no
		// children at all and the planner stayed silent.
		for ai, alt := range alternatives {
			skip := inserted > 0
			for _, child := range children {
				if child.Key == alt.Key && !skip {
					skip = child.Action == planActionSkip
				}
			}
			if !skip && len(children) == 0 && ai == 0 && len(alternatives) > 1 {
				// No planner opinion on alternatives: keep the first, skip the rest.
				continue
			}
			if !skip && len(children) == 0 && ai > 0 {
				skip = true
			}
			// The planner may skip one alternative explicitly by title/action.
			for _, child := range children {
				if child.Key == alt.Key && child.Action == planActionSkip {
					skip = true
				}
			}
			status := SectionSkipped
			if !skip {
				status = SectionQueued
			}
			if _, err := w.deps.DB.SetReportSectionStatus(ctx, database.SetReportSectionStatusParams{
				ID: alt.ID, Status: status,
			}); err != nil {
				return fmt.Errorf("failed to resolve alternative %q: %w", alt.Key, err)
			}
			if !skip {
				if err := w.setSectionSources(ctx, alt.ID, childSources(children, alt.Key)); err != nil {
					return fmt.Errorf("failed to record sources for alternative %q: %w", alt.Key, err)
				}
			}
		}
	}

	// Top-level decisions (skip includes composed sections until their phase).
	updated := make(map[int64]struct{}, len(got.Sections))
	for i := range sections {
		st := &sections[i]
		if st.ParentID.Valid {
			continue
		}
		entry, ok := decision[st.Key]
		if !ok || entry.Action != planActionSkip {
			continue
		}
		if _, err := w.deps.DB.SetReportSectionStatus(ctx, database.SetReportSectionStatusParams{
			ID: st.ID, Status: SectionSkipped,
		}); err != nil {
			return fmt.Errorf("failed to skip section %q: %w", st.Key, err)
		}
		updated[st.ID] = struct{}{}
	}

	sections, err := w.loadSections()
	if err != nil {
		return fmt.Errorf("failed to reload report sections: %w", err)
	}
	kept := 0
	for _, st := range sections {
		if st.Status != SectionSkipped {
			kept++
		}
	}

	estimate := got.Estimate
	if estimate <= 0 {
		estimate = kept
	}

	title := strings.TrimSpace(got.Title)
	params := database.SetReportPlanParams{
		ID:               w.report.ID,
		Status:           StatusGenerating,
		SectionsEstimate: int32(estimate),
		SectionsTotal:    int32(kept),
	}
	if title != "" && title != w.report.Title {
		params.Title = title
	} else {
		params.Title = w.report.Title
	}

	report, err := w.deps.DB.SetReportPlan(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to persist the plan: %w", err)
	}
	w.report = report
	w.doneCount = 0

	w.refreshProgress(ctx)
	w.publishRun()
	return nil
}

func childByKey(sections []database.ReportSection, key string) (database.ReportSection, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return database.ReportSection{}, false
	}
	for _, st := range sections {
		if st.Key == key {
			return st, true
		}
	}
	return database.ReportSection{}, false
}

func outlineSlug(title string) string {
	slug := strings.Builder{}
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			slug.WriteRune(r)
		case r == ' ', r == '-', r == '.':
			slug.WriteRune('-')
		}
		if slug.Len() >= 32 {
			break
		}
	}
	return strings.Trim(slug.String(), "-")
}

func childSources(children []planChild, key string) []int64 {
	for _, child := range children {
		if child.Key == key {
			return child.SourceSubtaskIDs
		}
	}
	return nil
}

// setSectionSources records the finding-to-subtask mapping the curator uses to
// assemble the section's evidence pack.
func (w *worker) setSectionSources(ctx context.Context, sectionID int64, sources []int64) error {
	if len(sources) == 0 {
		return nil
	}
	if _, err := w.deps.DB.SetReportSectionSources(ctx, database.SetReportSectionSourcesParams{
		ID: sectionID, SourceSubtaskIds: sources,
	}); err != nil {
		return err
	}
	return nil
}

func planChildInstructions(child planChild) string {
	parts := make([]string, 0, 3)
	if strings.TrimSpace(child.Length) != "" {
		parts = append(parts, "Length: "+strings.TrimSpace(child.Length))
	}
	if strings.TrimSpace(child.Summary) != "" {
		parts = append(parts, "Record summary: "+strings.TrimSpace(child.Summary))
	}
	if strings.TrimSpace(child.Instructions) != "" {
		parts = append(parts, strings.TrimSpace(child.Instructions))
	}
	return strings.Join(parts, "\n")
}

func (w *worker) loadSections() ([]database.ReportSection, error) {
	return w.deps.DB.GetReportSections(w.stateCtx, w.report.ID)
}

// loadNotes returns the operator notes as one text block for the prompts.
func (w *worker) loadNotes() string {
	logs, err := w.deps.DB.GetReportLogs(w.ctx, database.GetReportLogsParams{
		ReportID: w.report.ID, Limit: 200,
	})
	if err != nil {
		w.logger.WithError(err).Warn("failed to load report notes")
		return ""
	}

	var b strings.Builder
	for _, log := range logs {
		if log.Kind != LogNote {
			continue
		}
		fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(log.Message))
	}
	return strings.TrimRight(b.String(), "\n")
}

// writeAll walks the derived write order (body phase, then deferred) and
// produces the not-yet-written sections; budget exhaustion marks the rest.
func (w *worker) writeAll(ctx context.Context) error {
	if _, err := w.deps.DB.UpdateReportStatus(ctx, database.UpdateReportStatusParams{
		ID: w.report.ID, Status: StatusGenerating,
	}); err != nil {
		return fmt.Errorf("failed to persist generating status: %w", err)
	}
	w.publishRun()

	sections, err := w.loadSections()
	if err != nil {
		return fmt.Errorf("failed to load report sections: %w", err)
	}
	body, deferred := writeOrder(sections, w.tpl)

	if err := w.writeList(ctx, body); err != nil {
		return err
	}
	if w.stopped.Load() || ctx.Err() != nil || w.contentBudgetExhausted() {
		return nil
	}
	if err := w.writeList(ctx, deferred); err != nil {
		return err
	}

	return nil
}

// writeList writes one batch of sections in document order. Composed sections
// are done as their bodies are computed at export; aggregate parents resolve
// as headings over their children.
func (w *worker) writeList(ctx context.Context, list []database.ReportSection) error {
	for i := range list {
		st := list[i]
		if st.Hidden || st.Status == SectionDone || st.Status == SectionSkipped ||
			st.Status == SectionNotWritten || st.Status == SectionFailed {
			continue
		}

		switch st.Kind {
		case KindComposed, KindAggregate:
			if _, err := w.deps.DB.SetReportSectionStatus(ctx, database.SetReportSectionStatusParams{
				ID: st.ID, Status: SectionDone,
			}); err != nil {
				return fmt.Errorf("failed to mark section %d done: %w", st.ID, err)
			}
			w.bumpDone()
			w.publishRun()
			w.publishSection(st.ID)

		default:
			if w.contentBudgetExhausted() {
				if err := w.markNotWritten(ctx, list[i:]); err != nil {
					return err
				}
				w.putLog(LogControl, "content node budget exhausted: remaining sections are not written", nil)
				return nil
			}
			if err := w.writeSection(ctx, st); err != nil {
				if w.stopped.Load() || ctx.Err() != nil {
					return nil
				}
				w.putLog(LogError, fmt.Sprintf("section %q failed: %v", st.Title, err),
					map[string]any{"section_id": st.ID, "key": st.Key})
				if _, setErr := w.deps.DB.SetReportSectionStatus(ctx, database.SetReportSectionStatusParams{
					ID: st.ID, Status: SectionFailed,
				}); setErr != nil {
					return fmt.Errorf("failed to mark section %d failed: %w", st.ID, setErr)
				}
				w.publishSection(st.ID)
				// One failed section does not sink the report.
				continue
			}
		}

		if w.stopped.Load() || ctx.Err() != nil {
			return nil
		}
	}
	return nil
}

// markNotWritten closes the batch: everything not written yet (and not already
// resolved) becomes not_written, the run completes partially.
func (w *worker) markNotWritten(ctx context.Context, remaining []database.ReportSection) error {
	for i := range remaining {
		st := remaining[i]
		if st.Hidden || st.Status == SectionDone || st.Status == SectionSkipped ||
			st.Status == SectionNotWritten || st.Status == SectionFailed || st.Kind == KindComposed {
			continue
		}
		status := SectionNotWritten
		if st.Kind == KindAggregate {
			status = SectionDone // heading over nothing keeps the outline stable
		}
		if _, err := w.deps.DB.SetReportSectionStatus(ctx, database.SetReportSectionStatusParams{
			ID: st.ID, Status: status,
		}); err != nil {
			return fmt.Errorf("failed to mark section %d not written: %w", st.ID, err)
		}
		if st.Kind == KindAggregate {
			w.publishSection(st.ID)
		}
	}
	w.publishRun()
	return nil
}

// writeSection streams one section body, throttling persistence and live
// updates every REPORT_STREAM_INTERVAL_MS; the final snapshot marks it done.
func (w *worker) writeSection(ctx context.Context, st database.ReportSection) error {
	node := w.sectionNode(st)
	if node == nil {
		return fmt.Errorf("section %s has no template node", st.Key)
	}

	sectionContext := w.sectionContext(st)
	evidenceText, evidenceCandidates := w.evidenceFor(ctx, st)
	shots := screenshotCandidates(evidenceCandidates)
	attachScreenshots := len(shots) > 0 && w.visionCapable(ctx)

	if w.digest == "" {
		digest, _, err := buildDigest(ctx, w.deps.DB, w.deps.Cfg.ReportMaxRecordBytes, w.report.FlowID)
		if err != nil {
			return fmt.Errorf("failed to build record for %s: %w", st.Key, err)
		}
		w.digest = digest
	}

	system, err := w.deps.Prompter.RenderTemplate(templates.PromptTypeReportWriter, map[string]any{
		"Language":               w.report.Language,
		"LengthHint":             node.LengthHint(w.tpl, st.ParentID.Valid),
		"AdditionalInstructions": nullableText(w.report.AdditionalInstructions),
		"Notes":                  w.loadNotes(),
		"Outline":                w.outlineText(),
		"Record":                 w.digest,
		"SectionContext":         sectionContext,
		"Evidence":               evidenceText,
		"ScreenshotsAttached":    attachScreenshots,
		"SectionKey":             st.Key,
		"SectionTitle":           st.Title,
		"SectionInstructions":    node.Instructions,
	})
	if err != nil {
		return fmt.Errorf("failed to render writer prompt for %s: %w", st.Key, err)
	}

	if _, err := w.deps.DB.SetReportSectionStatus(ctx, database.SetReportSectionStatusParams{
		ID: st.ID, Status: SectionWriting,
	}); err != nil {
		return fmt.Errorf("failed to mark section %d writing: %w", st.ID, err)
	}
	w.putLog(LogSection, fmt.Sprintf("Writing section %q", st.Title), map[string]any{
		"section_id": st.ID, "key": st.Key, "title": st.Title,
	})
	w.publishSection(st.ID)

	humanParts := []llms.ContentPart{
		llms.TextContent{Text: fmt.Sprintf("Write the body of the section %q now, then stop.", st.Title)},
	}
	if attachScreenshots {
		attached := 0
		for _, ref := range shots {
			if attached >= maxScreenshotsPerSection {
				break
			}
			if url := screenshotDataURL(w.deps.Cfg.DataDir, w.report.FlowID, ref.ScreenshotName); url != "" {
				humanParts = append(humanParts, llms.ImageURLContent{URL: url})
				attached++
			}
		}
		if attached > 0 {
			w.putLog(LogControl, fmt.Sprintf("attached %d screenshot(s) to section %q", attached, st.Title), map[string]any{
				"section_id": st.ID, "key": st.Key, "screenshots": attached,
			})
		}
	}

	chain := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextContent{Text: system}}},
		{Role: llms.ChatMessageTypeHuman, Parts: humanParts},
	}

	var (
		mx    sync.Mutex // guards snap and the run counters read by flush
		snap  strings.Builder
		start = time.Now()
	)
	flush := func(force bool) {
		mx.Lock()
		content := snap.String()
		mx.Unlock()
		if !force && strings.TrimSpace(content) == "" {
			return
		}
		row, err := w.deps.DB.UpdateReportSectionContent(w.stateCtx, database.UpdateReportSectionContentParams{
			ID:              st.ID,
			Content:         content,
			Status:          SectionWriting,
			TokensIn:        w.tokensIn,
			TokensOut:       w.tokensOut,
			DurationSeconds: time.Since(start).Seconds(),
		})
		if err != nil {
			w.logger.WithError(err).Warn("failed to persist section progress")
			return
		}
		_ = row
		w.publishSection(st.ID)
	}
	interval := time.Duration(w.deps.Cfg.ReportStreamIntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 1500 * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(ctx, writingTimeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	flushDone := make(chan struct{})
	var flushWG sync.WaitGroup
	flushWG.Add(1)
	go func() {
		defer flushWG.Done()
		for {
			select {
			case <-ticker.C:
				flush(false)
			case <-flushDone:
				return
			}
		}
	}()

	streamCb := func(_ context.Context, chunk streaming.Chunk) error {
		if chunk.Type == streaming.ChunkTypeText {
			mx.Lock()
			snap.WriteString(chunk.Content)
			mx.Unlock()
		}
		return nil
	}

	prevIn, prevOut := w.tokensIn, w.tokensOut
	resp, err := w.prv.CallEx(ctx, pconfig.OptionsTypeSimple, chain, streamCb)
	close(flushDone)
	contentSeconds := time.Since(start).Seconds()

	if err != nil {
		flush(true)
		if w.stopped.Load() || ctx.Err() != nil {
			return err
		}
		return fmt.Errorf("writer call failed: %w", err)
	}
	w.recordNodeUsage(false, resp, contentSeconds)

	if resp == nil || len(resp.Choices) < 1 {
		return fmt.Errorf("empty writer response")
	}
	content := resp.Choices[0].Content

	row, err := w.deps.DB.UpdateReportSectionContent(ctx, database.UpdateReportSectionContentParams{
		ID:              st.ID,
		Content:         content,
		Status:          SectionDone,
		TokensIn:        w.tokensIn - prevIn,
		TokensOut:       w.tokensOut - prevOut,
		DurationSeconds: contentSeconds,
	})
	if err != nil {
		return fmt.Errorf("failed to persist final section content: %w", err)
	}
	_ = row
	w.persistEvidence(st, content, evidenceCandidates)
	w.bumpDone()
	w.putLog(LogSection, fmt.Sprintf("Section %q written (%s)", st.Title,
		humanDuration(contentSeconds)), map[string]any{
		"section_id": st.ID, "key": st.Key, "status": SectionDone,
	})
	w.publishSection(st.ID)
	w.publishRun()

	return nil
}

// sectionNode resolves the writer configuration for a section: either its
// template node or (for children) a synthesized node borrowing the parent's
// child instructions.
func (w *worker) sectionNode(st database.ReportSection) *SectionNode {
	if node := outlineNodeOf(w.tpl, st); node != nil && !st.ParentID.Valid {
		return node
	}

	sections, err := w.loadSections()
	if err != nil {
		return nil
	}

	if st.ParentID.Valid {
		for i := range sections {
			parent := &sections[i]
			if parent.ID != st.ParentID.Int64 {
				continue
			}
			parentNode := outlineNodeOf(w.tpl, *parent)
			if parentNode == nil {
				return nil
			}

			child := &SectionNode{
				Key:   st.Key,
				Kind:  KindLLM,
				Title: st.Title,
				Instructions: stringsOr(parentNode.ChildInstructions,
					fmt.Sprintf("Write the section %q from the engagement record (part of the %q chapter).",
						st.Title, parent.Title)),
			}
			if length := w.plannedLength(st); length != "" {
				child.ChildLength = length
			}
			if st.Instructions.Valid && st.Instructions.String != "" {
				child.Instructions = strings.Join([]string{
					strings.TrimSpace(child.Instructions), strings.TrimSpace(st.Instructions.String),
				}, "\n")
			}
			return child
		}
		return nil
	}

	// Manually added top-level sections: template node does not exist.
	return &SectionNode{
		Key:   st.Key,
		Kind:  KindLLM,
		Title: st.Title,
		Instructions: stringsOr(nullableText(st.Instructions),
			fmt.Sprintf("Write the section %q from the engagement record.", st.Title)),
		Length: w.plannedLength(st),
	}
}

// plannedLength reads the planner length hint from the section instructions
// ("Length: ..." line written by planChildInstructions).
func (w *worker) plannedLength(st database.ReportSection) string {
	if !st.Instructions.Valid {
		return ""
	}
	for _, line := range strings.Split(st.Instructions.String, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Length: ") {
			return strings.TrimPrefix(line, "Length: ")
		}
	}
	return ""
}

func (w *worker) outlineText() string {
	sections, err := w.loadSections()
	if err != nil {
		return ""
	}
	return renderOutline(sections, false)
}

// visionCapable probes the report's provider once and caches the answer, so the
// writers know whether to attach screenshot images (plan § 6.10, P7.4).
func (w *worker) visionCapable(ctx context.Context) bool {
	w.visionOnce.Do(func() {
		ok, err := probeVision(ctx, w.prv)
		if err != nil {
			w.logger.WithError(err).Info("vision probe: the model did not answer with an image")
		}
		w.vision = ok
		state := "will not"
		if ok {
			state = "will"
		}
		w.putLog(LogControl, fmt.Sprintf("vision probe: screenshots %s be analysed", state), map[string]any{
			"vision": ok,
		})
	})

	return w.vision
}

// screenshotCandidates keeps the screenshot evidence a section can attach.
func screenshotCandidates(refs []EvidenceRef) []EvidenceRef {
	out := make([]EvidenceRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Kind == EvidenceKindScreenshot && ref.ScreenshotName != "" {
			out = append(out, ref)
		}
	}
	return out
}

// evidenceFor assembles the section's evidence pack from the subtasks the
// planner mapped it to. It returns the prompt text and the candidate list used
// later to resolve the writer's citations into the archive manifest.
func (w *worker) evidenceFor(ctx context.Context, st database.ReportSection) (string, []EvidenceRef) {
	if len(st.SourceSubtaskIds) == 0 {
		return "", nil
	}
	pack, candidates, err := buildEvidencePack(ctx, w.deps.DB, st.SourceSubtaskIds, w.evidenceCaps())
	if err != nil {
		w.logger.WithError(err).Warn("failed to build the evidence pack")
		return "", nil
	}
	if pack != "" {
		w.putLog(LogControl, fmt.Sprintf("evidence pack for %q (%d candidates)", st.Title, len(candidates)), map[string]any{
			"section_id": st.ID, "key": st.Key, "candidates": len(candidates), "bytes": len(pack),
		})
	}
	return pack, candidates
}

func (w *worker) evidenceCaps() EvidenceCaps {
	caps := EvidenceCaps{}
	if w.deps.Cfg != nil {
		caps.ItemBytes = w.deps.Cfg.ReportEvidenceItemBytes
		caps.MaxItems = w.deps.Cfg.ReportEvidenceMaxItems
		caps.PackBytes = w.deps.Cfg.ReportEvidenceMaxBytes
	}
	return caps
}

// persistEvidence resolves the writer's [Ex] citations into the manifest the
// archive bundles; a section that cited nothing keeps an empty manifest.
func (w *worker) persistEvidence(st database.ReportSection, content string, candidates []EvidenceRef) {
	if len(candidates) == 0 {
		return
	}
	refs := resolveCitations(content, candidates)
	blob, err := encodeEvidenceRefs(refs)
	if err != nil {
		w.logger.WithError(err).Warn("failed to encode the evidence manifest")
		return
	}
	if _, err := w.deps.DB.SetReportSectionEvidence(w.stateCtx, database.SetReportSectionEvidenceParams{
		ID: st.ID, EvidenceRefs: blob,
	}); err != nil {
		w.logger.WithError(err).Warn("failed to persist the evidence manifest")
		return
	}
	if len(refs) > 0 {
		w.putLog(LogSection, fmt.Sprintf("Section %q cites %d evidence items", st.Title, len(refs)), map[string]any{
			"section_id": st.ID, "key": st.Key, "cited": len(refs),
		})
	}
}

// sectionContext is the per-section extra context (v1 keeps it empty for
// template nodes: the record plus instructions carry everything; planner
// children carry their summary in the instructions).
func (w *worker) sectionContext(st database.ReportSection) string {
	if st.ParentID.Valid {
		return ""
	}
	return ""
}

// recordNodeUsage accumulates one node's usage into the run counters and
// persists the progress row.
func (w *worker) recordNodeUsage(control bool, resp *llms.ContentResponse, seconds float64) {
	if resp == nil {
		return
	}

	usage := pconfig.NewCallUsage(nil)
	if len(resp.Choices) > 0 {
		usage = w.prv.GetUsage(resp.Choices[0].GenerationInfo)
	}
	for _, choice := range resp.Choices[1:] {
		usage.Merge(w.prv.GetUsage(choice.GenerationInfo))
	}

	w.mx.Lock()
	if control {
		w.controlUsed += seconds
	} else {
		w.contentUsed += seconds
	}
	w.tokensIn += usage.Input
	w.tokensOut += usage.Output
	w.mx.Unlock()

	w.refreshProgress(w.ctx)
}

func (w *worker) bumpDone() {
	w.doneCount++
}

// refreshProgress persists the running counters (done, tokens, agent time).
func (w *worker) refreshProgress(ctx context.Context) {
	w.mx.Lock()
	params := database.UpdateReportProgressParams{
		ID:                 w.report.ID,
		SectionsDone:       w.doneCount,
		TokensIn:           w.tokensIn,
		TokensOut:          w.tokensOut,
		ControlUsedSeconds: w.controlUsed,
		ContentUsedSeconds: w.contentUsed,
	}
	w.mx.Unlock()

	if _, err := w.deps.DB.UpdateReportProgress(w.stateCtx, params); err != nil {
		w.logger.WithError(err).Warn("failed to persist report progress")
	}
}

func (w *worker) contentBudgetExhausted() bool {
	// The effective budget was fixed at creation (report row), not at boot:
	// the env default only fills the blanks the creation request leaves.
	return w.report.ContentBudgetSeconds > 0 && w.contentUsed >= float64(w.report.ContentBudgetSeconds)
}

func (w *worker) controlBudgetExhausted() bool {
	return w.report.ControlBudgetSeconds > 0 && w.controlUsed >= float64(w.report.ControlBudgetSeconds)
}

// finish closes the run: final status persist + logs + live events.
func (w *worker) finish(status string, errText string) {
	if status == StatusCompleted {
		sections, err := w.loadSections()
		if err == nil {
			for i := range sections {
				st := &sections[i]
				if st.Status == SectionQueued || st.Status == SectionWriting {
					next := SectionNotWritten
					if st.Kind == KindAggregate || st.Kind == KindComposed {
						next = SectionDone
					}
					if _, err := w.deps.DB.SetReportSectionStatus(w.ctx, database.SetReportSectionStatusParams{
						ID: st.ID, Status: next,
					}); err != nil {
						w.logger.WithError(err).Warn("failed to resolve leftover section")
					}
				}
			}
		}
	}

	report, err := w.deps.DB.FinishReport(w.stateCtx, database.FinishReportParams{
		ID: w.report.ID, Status: status, Error: nullString(errText),
	})
	if err != nil {
		w.logger.WithError(err).Error("failed to persist final report status")
		return
	}
	w.report = report

	message := fmt.Sprintf("report finished: %s", status)
	if errText != "" {
		message += fmt.Sprintf(" (%s)", errText)
	}
	w.putLog(LogStatus, message, map[string]any{"status": status, "error": errText})
	w.refreshProgress(w.ctx)
	w.publishRun()
}

// setStopState closes a stopped run: a writing section keeps its partial
// content as failed, everything still queued is not written.
func (w *worker) setStopState() {
	sections, err := w.loadSections()
	if err == nil {
		for i := range sections {
			st := &sections[i]
			next := ""
			switch st.Status {
			case SectionWriting:
				next = SectionFailed
			case SectionQueued, "":
				next = SectionNotWritten
			}
			if next == "" {
				continue
			}
			if _, err := w.deps.DB.SetReportSectionStatus(w.stateCtx, database.SetReportSectionStatusParams{
				ID: st.ID, Status: next,
			}); err != nil {
				w.logger.WithError(err).Warn("failed to set stop state on section")
			}
			w.publishSection(st.ID)
		}
	}

	w.finish(StatusCanceled, "stopped by operator")
}

// Stop cancels the run; the write loop observes it and finalizes.
func (w *worker) Stop() {
	w.stopped.Store(true)
	w.cancel()
}

func (w *worker) putLog(kind string, message string, payload map[string]any) {
	row, err := w.deps.DB.CreateReportLog(w.stateCtx, database.CreateReportLogParams{
		ReportID: w.report.ID,
		Kind:     kind,
		Message:  message,
		Payload:  payloadToNullJSON(payload),
	})
	if err != nil {
		w.logger.WithError(err).Warn("failed to persist report log")
		return
	}
	if w.pub == nil {
		return
	}

	converted, err := converter.ConvertReportLog(row)
	if err != nil {
		return
	}
	w.pub.ReportLogAdded(w.ctx, converted)
}

// stringsOr returns the first non-empty string of the pair.
func stringsOr(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func humanDuration(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second)).Round(time.Second)
	return d.String()
}

// publishRun emits the fresh report row to the flow subscribers.
func (w *worker) publishRun() {
	if w.pub == nil {
		return
	}
	row, err := w.deps.DB.GetReport(w.stateCtx, database.GetReportParams{
		ID: w.report.ID, UserID: w.report.UserID,
	})
	if err != nil {
		w.logger.WithError(err).Warn("failed to reload report for publish")
		return
	}
	w.pub.ReportUpdated(w.stateCtx, converter.ConvertReport(row))
}

// publishSection emits one fresh section row (writing progress or final).
func (w *worker) publishSection(sectionID int64) {
	if w.pub == nil {
		return
	}
	row, err := w.deps.DB.GetReportSection(w.stateCtx, database.GetReportSectionParams{
		ID: sectionID, ReportID: w.report.ID,
	})
	if err != nil {
		w.logger.WithError(err).Warn("failed to reload section for publish")
		return
	}
	w.pub.ReportSectionUpdated(w.stateCtx, converter.ConvertReportSection(row))
}
