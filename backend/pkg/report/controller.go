package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/database/converter"
	obs "pentagi/pkg/observability"
	"pentagi/pkg/providers/provider"

	"github.com/sirupsen/logrus"
	"github.com/sqlc-dev/pqtype"
)

const defaultTemplateKey = "pentagi-default-pentest-report"

// ErrReportBusy is returned when a section mutation is refused because the
// run is still generating (v1: editing waits for the run to end; the v2
// backlog replaces the rejection with a rewrite queue, plan § 6.9).
var ErrReportBusy = errors.New("report is generating")

// ErrReportStopped is returned when a stopped run is expected but not active.
var ErrReportStopped = errors.New("report is not running")

// ErrForbidden is the ownership guard (same semantics as the graph layer).
var ErrForbidden = errors.New("forbidden")

// CreateParams is the CreateReport mutation payload; provider resolution and
// budget defaults happen in the controller.
type CreateParams struct {
	TemplateKey  string
	ProviderName string
	Title        string
	// Empty falls back to the flow's language, then to English.
	Language               string
	AdditionalInstructions string
	// 0 = use the configured default; values > 0 override the env defaults.
	ControlBudgetSeconds int
	ContentBudgetSeconds int
}

// Controller owns the report workers and the user-facing operations; the
// resolver layer sees controller.ReportController.
type Controller struct {
	deps    Deps
	mx      sync.Mutex
	workers map[int64]*worker
}

func NewController(deps Deps) *Controller {
	return &Controller{
		deps:    deps,
		workers: make(map[int64]*worker),
	}
}

// Enabled reports whether the feature is switched on (exposed in Settings).
func (c *Controller) Enabled() bool {
	return c.deps.Cfg.ReportEnabled
}

// templateLabel resolves the human label of one template key, falling back to
// the key itself.
func templateLabel(key string) string {
	if tpl, err := GetTemplate(key); err == nil && tpl.Label != "" {
		return tpl.Label
	}
	return key
}

// CreateReport validates the request, persists the skeleton outline and
// starts the run worker.
func (c *Controller) CreateReport(
	ctx context.Context,
	userID, flowID int64,
	params CreateParams,
) (*database.Report, error) {
	ctx, span := obs.Observer.NewSpan(ctx, obs.SpanKindInternal, "report.controller.create")
	defer span.End()

	if !c.Enabled() {
		return nil, fmt.Errorf("reports are disabled (REPORT_ENABLED=false)")
	}

	flow, err := c.deps.DB.GetFlow(ctx, flowID)
	if err != nil {
		return nil, fmt.Errorf("failed to load flow %d: %w", flowID, err)
	}
	if flow.UserID != userID {
		return nil, fmt.Errorf("%w: flow %d belongs to another user", ErrForbidden, flowID)
	}

	key := strings.TrimSpace(params.TemplateKey)
	if key == "" {
		key = defaultTemplateKey
	}
	tpl, err := GetTemplate(key)
	if err != nil {
		return nil, fmt.Errorf("unknown report template %q: %w", key, err)
	}
	label := templateLabel(key)

	prvname := strings.TrimSpace(params.ProviderName)
	prvtype := provider.ProviderType(flow.ModelProviderType)
	if prvname != "" && prvname != flow.ModelProviderName {
		// The requested provider must resolve; its type is authoritative so
		// restarts do not re-resolve.
		prv, err := c.deps.Provs.GetProvider(ctx, provider.ProviderName(prvname), userID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve provider %q: %w", prvname, err)
		}
		prvtype = prv.Type()
	} else {
		prvname = flow.ModelProviderName
	}

	ctrlBudget := c.deps.Cfg.ReportControlBudgetSec
	if params.ControlBudgetSeconds > 0 {
		ctrlBudget = params.ControlBudgetSeconds
	}
	contentBudget := c.deps.Cfg.ReportContentBudgetSec
	if params.ContentBudgetSeconds > 0 {
		contentBudget = params.ContentBudgetSeconds
	}

	language := strings.TrimSpace(params.Language)
	if language == "" {
		language = strings.TrimSpace(flow.Language)
	}
	if language == "" {
		language = "English"
	}

	existing, err := c.deps.DB.GetFlowReports(ctx, database.GetFlowReportsParams{
		FlowID: flowID, UserID: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to count flow reports: %w", err)
	}
	if active := countActive(existing); active >= c.deps.Cfg.ReportMaxConcurrentPerFlow {
		return nil, fmt.Errorf("flow %d already carries %d generating reports (max %d)",
			flowID, active, c.deps.Cfg.ReportMaxConcurrentPerFlow)
	}

	report, err := c.deps.DB.CreateReport(ctx, database.CreateReportParams{
		FlowID:                 flowID,
		UserID:                 userID,
		Status:                 StatusCreated,
		Title:                  strings.TrimSpace(params.Title),
		TemplateKey:            tpl.Key,
		TemplateLabel:          label,
		ProviderName:           prvname,
		ProviderType:           database.ProviderType(prvtype),
		Language:               language,
		AdditionalInstructions: nullString(params.AdditionalInstructions),
		ControlBudgetSeconds:   int32(ctrlBudget),
		ContentBudgetSeconds:   int32(contentBudget),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create report: %w", err)
	}

	estimate, err := c.persistSkeleton(ctx, tpl, report.ID)
	if err != nil {
		return nil, err
	}
	if _, err := c.deps.DB.SetReportEstimate(ctx, database.SetReportEstimateParams{
		ID: report.ID, SectionsEstimate: int32(estimate),
	}); err != nil {
		logrus.WithContext(ctx).WithError(err).Warn("failed to persist report estimate")
	}

	log := logrus.WithFields(logrus.Fields{
		"report_id": report.ID, "flow_id": flowID, "template": tpl.Key, "component": "report",
	})
	log.Info("report created")

	c.putLog(ctx, report, 0, LogControl,
		fmt.Sprintf("report created: template %s (%s), %d sections of outline", tpl.Key, label, estimate), nil)

	w, err := newWorker(c.deps, report)
	if err != nil {
		return nil, err
	}

	c.registerWorker(report.ID, w)

	c.deps.Subs.NewFlowPublisher(userID, flowID).ReportCreated(ctx, converter.ConvertReport(report))
	w.start()

	return &report, nil
}

// persistSkeleton inserts the template outline (top-level sections and their
// alternatives) on a fresh report; returns how many sections it created.
func (c *Controller) persistSkeleton(ctx context.Context, tpl *Template, reportID int64) (int, error) {
	total := 0
	for i, node := range tpl.Sections {
		row, err := c.deps.DB.CreateReportSection(ctx, database.CreateReportSectionParams{
			ReportID:     reportID,
			ParentID:     sql.NullInt64{},
			Position:     int32(i + 1),
			Key:          node.Key,
			Kind:         node.Kind,
			Title:        node.Title,
			Instructions: nullString(node.Instructions),
		})
		if err != nil {
			return total, fmt.Errorf("failed to insert skeleton section %s: %w", node.Key, err)
		}
		total++
		_ = row

		for ai, alt := range node.EmptyChildren {
			if _, err := c.deps.DB.CreateReportSection(ctx, database.CreateReportSectionParams{
				ReportID:     reportID,
				ParentID:     database.Int64ToNullInt64(&row.ID),
				Position:     int32(ai + 1),
				Key:          alt.Key,
				Kind:         KindLLM,
				Title:        alt.Title,
				Instructions: nullString(alt.Instructions),
			}); err != nil {
				return total, fmt.Errorf("failed to insert alternative %s: %w", alt.Key, err)
			}
			total++
		}
	}
	return total, nil
}

// LoadReports rebuilds workers for the reports that were mid-run at restart
// (planning/generating); each resumes from its persisted rows.
func (c *Controller) LoadReports(ctx context.Context) error {
	if !c.Enabled() {
		return nil
	}

	if err := c.recoverInterrupted(ctx); err != nil {
		return err
	}

	active, err := c.deps.DB.GetActiveReports(ctx)
	if err != nil {
		return fmt.Errorf("failed to load active reports: %w", err)
	}

	for _, report := range active {
		w, err := newWorker(c.deps, report)
		if err != nil {
			logrus.WithContext(ctx).WithError(err).
				Errorf("failed to load report %d worker", report.ID)
			continue
		}

		c.registerWorker(report.ID, w)

		w.start()
		logrus.WithContext(ctx).WithFields(logrus.Fields{
			"report_id": report.ID, "flow_id": report.FlowID, "component": "report-worker",
		}).Info("report worker resumed from DB")
	}

	return nil
}

// recoverInterrupted resets sections stuck in writing back to queued (their
// partial content is replaced when the writer runs again).
func (c *Controller) recoverInterrupted(ctx context.Context) error {
	active, err := c.deps.DB.GetActiveReports(ctx)
	if err != nil {
		return fmt.Errorf("failed to scan active reports: %w", err)
	}
	for _, report := range active {
		sections, err := c.deps.DB.GetReportSections(ctx, report.ID)
		if err != nil {
			continue
		}
		for i := range sections {
			if sections[i].Status != SectionWriting {
				continue
			}
			if _, err := c.deps.DB.SetReportSectionStatus(ctx, database.SetReportSectionStatusParams{
				ID: sections[i].ID, Status: SectionQueued,
			}); err != nil {
				logrus.WithContext(ctx).WithError(err).
					Warnf("failed to reset section %d of report %d", sections[i].ID, report.ID)
			}
		}
	}
	return nil
}

// ListFlowReports returns the reports of one flow.
func (c *Controller) ListFlowReports(ctx context.Context, userID, flowID int64) ([]database.Report, error) {
	rows, err := c.deps.DB.GetFlowReports(ctx, database.GetFlowReportsParams{
		FlowID: flowID, UserID: userID,
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// GetReport returns one report owned by the user.
func (c *Controller) GetReport(ctx context.Context, userID, reportID int64) (*database.Report, error) {
	report, err := c.deps.DB.GetReport(ctx, database.GetReportParams{ID: reportID, UserID: userID})
	if err != nil {
		return nil, err
	}
	return &report, nil
}

// PutNote appends an operator note; the workers inject it into every section
// still to come ("Passed to the sections still to come.").
func (c *Controller) PutNote(ctx context.Context, userID, reportID int64, note string) error {
	report, err := c.GetReport(ctx, userID, reportID)
	if err != nil {
		return err
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return nil
	}

	c.putLog(ctx, *report, 0, LogNote, note, nil)
	return nil
}

// StopReport cancels a generating run; the worker observes the cancel and
// finalizes (writing section failed, queued sections not written).
func (c *Controller) StopReport(ctx context.Context, userID, reportID int64) error {
	report, err := c.GetReport(ctx, userID, reportID)
	if err != nil {
		return err
	}
	if !isRunActive(report) {
		return ErrReportStopped
	}

	c.mx.Lock()
	w := c.workers[reportID]
	c.mx.Unlock()

	if w != nil {
		w.Stop()
		return nil
	}

	// No live worker (reload after a crash): finalize the rows here.
	if _, err := c.deps.DB.FinishReport(ctx, database.FinishReportParams{
		ID: report.ID, Status: StatusCanceled, Error: nullString("stopped by operator"),
	}); err != nil {
		return fmt.Errorf("failed to mark report canceled: %w", err)
	}
	c.deps.Subs.NewFlowPublisher(userID, report.FlowID).
		ReportUpdated(ctx, converter.ConvertReport(*report))
	return nil
}

// DeleteReport stops the run (if any) and removes the report rows.
func (c *Controller) DeleteReport(ctx context.Context, userID, reportID int64) error {
	report, err := c.GetReport(ctx, userID, reportID)
	if err != nil {
		return err
	}

	c.mx.Lock()
	w := c.workers[reportID]
	delete(c.workers, reportID)
	c.mx.Unlock()

	if w != nil {
		w.Stop()
		if isRunActive(report) {
			// Give the worker a beat to observe the cancel before rows vanish.
			time.Sleep(50 * time.Millisecond)
		}
	}

	if err := c.deps.DB.DeleteReport(ctx, database.DeleteReportParams{ID: reportID, UserID: userID}); err != nil {
		return fmt.Errorf("failed to delete report %d: %w", reportID, err)
	}

	c.deps.Subs.NewFlowPublisher(userID, report.FlowID).
		ReportDeleted(ctx, converter.ConvertReport(*report))
	return nil
}

// ResumeReport relaunches a completed, failed or canceled report: the sections
// left unwritten (or failed) are re-queued and a fresh worker writes them. An
// optional contentBudgetOverride grants fresh content seconds, so a run that
// stopped on budget (or a report whose sections were added by hand) can make
// progress; without it a budget-exhausted report would re-mark everything.
func (c *Controller) ResumeReport(
	ctx context.Context, userID, reportID int64, contentBudgetOverride *int,
) (*database.Report, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("reports are disabled (REPORT_ENABLED=false)")
	}

	report, err := c.GetReport(ctx, userID, reportID)
	if err != nil {
		return nil, err
	}

	if !c.claimRun(reportID) {
		return nil, ErrReportBusy
	}
	// claimRun registered a placeholder; replace it once the worker is built, or
	// release the claim if anything below fails.
	claimed := true
	defer func() {
		if claimed {
			c.releaseRun(reportID)
		}
	}()

	if contentBudgetOverride != nil && *contentBudgetOverride > 0 {
		budget := int32(math.Ceil(report.ContentUsedSeconds)) + int32(*contentBudgetOverride)
		if _, err := c.deps.DB.SetReportContentBudget(ctx, database.SetReportContentBudgetParams{
			ID: reportID, ContentBudgetSeconds: budget,
		}); err != nil {
			return nil, fmt.Errorf("failed to extend the report content budget: %w", err)
		}
		report, err = c.GetReport(ctx, userID, reportID)
		if err != nil {
			return nil, err
		}
	}

	if err := c.requeueUnwritten(ctx, reportID); err != nil {
		return nil, err
	}

	resumed, err := c.deps.DB.ResetReportForResume(ctx, database.ResetReportForResumeParams{
		ID: reportID, Status: StatusCreated,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to reset report %d for resume: %w", reportID, err)
	}

	w, err := newWorker(c.deps, resumed)
	if err != nil {
		return nil, err
	}

	publish := c.deps.Subs.NewFlowPublisher(resumed.UserID, resumed.FlowID)
	c.registerWorker(reportID, w)
	claimed = false

	// Publish the reset row BEFORE the run starts: a no-op resume can finish in
	// milliseconds and its generating/completed frames must not be overtaken by
	// this one, which would leave the client stuck on the active "created" state.
	publish.ReportUpdated(ctx, converter.ConvertReport(resumed))

	w.runSections(nil)

	return &resumed, nil
}

// WriteReportSection writes a single section through the writer (the manual
// "write this section"): used after a hand-added section, or a section left
// unwritten by a budget stop. The report must not carry a live run.
func (c *Controller) WriteReportSection(
	ctx context.Context, userID, reportID, sectionID int64,
) (*database.ReportSection, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("reports are disabled (REPORT_ENABLED=false)")
	}

	_, err := c.GetReport(ctx, userID, reportID)
	if err != nil {
		return nil, err
	}

	if !c.claimRun(reportID) {
		return nil, ErrReportBusy
	}
	claimed := true
	defer func() {
		if claimed {
			c.releaseRun(reportID)
		}
	}()

	if _, err := c.deps.DB.GetReportSection(ctx, database.GetReportSectionParams{
		ID: sectionID, ReportID: reportID,
	}); err != nil {
		return nil, err
	}

	st, err := c.deps.DB.SetReportSectionStatus(ctx, database.SetReportSectionStatusParams{
		ID: sectionID, Status: SectionQueued,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to queue section %d: %w", sectionID, err)
	}

	report, err := c.deps.DB.ResetReportForResume(ctx, database.ResetReportForResumeParams{
		ID: reportID, Status: StatusCreated,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to reset report %d: %w", reportID, err)
	}

	w, err := newWorker(c.deps, report)
	if err != nil {
		return nil, err
	}

	c.registerWorker(reportID, w)
	claimed = false

	publish := c.deps.Subs.NewFlowPublisher(report.UserID, report.FlowID)
	publish.ReportUpdated(ctx, converter.ConvertReport(report))
	c.publish(ctx, userID, reportID, sectionID)

	w.runSections([]int64{sectionID})

	return &st, nil
}

// requeueUnwritten resets the sections that were not written (not_written) or
// failed back to queued so a resumed run rewrites them.
func (c *Controller) requeueUnwritten(ctx context.Context, reportID int64) error {
	sections, err := c.deps.DB.GetReportSections(ctx, reportID)
	if err != nil {
		return fmt.Errorf("failed to load sections of report %d: %w", reportID, err)
	}
	for i := range sections {
		st := sections[i]
		if st.Status != SectionNotWritten && st.Status != SectionFailed {
			continue
		}
		if _, err := c.deps.DB.SetReportSectionStatus(ctx, database.SetReportSectionStatusParams{
			ID: st.ID, Status: SectionQueued,
		}); err != nil {
			return fmt.Errorf("failed to requeue section %d: %w", st.ID, err)
		}
	}
	return nil
}

// claimRun reserves the report id for a new worker, unless one is already live.
// A nil sentinel marks the slot while the worker is being built.
func (c *Controller) claimRun(reportID int64) bool {
	c.mx.Lock()
	defer c.mx.Unlock()
	if _, ok := c.workers[reportID]; ok {
		return false
	}
	c.workers[reportID] = nil
	return true
}

// releaseRun drops a claim that did not end in a registered worker.
func (c *Controller) releaseRun(reportID int64) {
	c.mx.Lock()
	if c.workers[reportID] == nil {
		delete(c.workers, reportID)
	}
	c.mx.Unlock()
}

// registerWorker stores a live worker and arms its self-removal: the entry is
// dropped when the run ends, so a finished report can be resumed or rewritten
// again later (a stale entry would read as "report is generating" forever).
func (c *Controller) registerWorker(reportID int64, w *worker) {
	w.onDone = func() { c.forgetWorker(reportID, w) }
	c.mx.Lock()
	c.workers[reportID] = w
	c.mx.Unlock()
}

// forgetWorker drops the worker for a report unless a newer one took its place.
func (c *Controller) forgetWorker(reportID int64, w *worker) {
	c.mx.Lock()
	if cur, ok := c.workers[reportID]; ok && cur == w {
		delete(c.workers, reportID)
	}
	c.mx.Unlock()
}

func isRunActive(report *database.Report) bool {
	switch report.Status {
	case StatusCreated, StatusPlanning, StatusGenerating:
		return true
	}
	return false
}

func countActive(reports []database.Report) int {
	n := 0
	for i := range reports {
		if isRunActive(&reports[i]) {
			n++
		}
	}
	return n
}

// putLog writes one log row and publishes it (helper without a live worker).
func (c *Controller) putLog(ctx context.Context, report database.Report, sectionID int64, kind string, message string, payload map[string]any) {
	row, err := c.deps.DB.CreateReportLog(ctx, database.CreateReportLogParams{
		ReportID:  report.ID,
		SectionID: database.Int64ToNullInt64(nz(sectionID)),
		Kind:      kind,
		Message:   message,
		Payload:   payloadToNullJSON(payload),
	})
	if err != nil {
		logrus.WithContext(ctx).WithError(err).Warn("failed to persist report log")
		return
	}

	if converted, err := converter.ConvertReportLog(row); err == nil {
		c.deps.Subs.NewFlowPublisher(report.UserID, report.FlowID).ReportLogAdded(ctx, converted)
	}
}

func nz(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

// sectionIsEditable guards section mutations while the run is generating.
func (c *Controller) sectionIsEditable(ctx context.Context, userID, reportID int64) (*database.Report, error) {
	report, err := c.GetReport(ctx, userID, reportID)
	if err != nil {
		return nil, err
	}
	if isRunActive(report) {
		return nil, ErrReportBusy
	}
	return report, nil
}

// UpdateReportSectionFields applies the editor mutations on one section
// (title, instructions, variables as JSON text, warnings). While the run is
// generating the edit is accepted and the section is flagged for regeneration
// (the live worker's drain loop rewrites it) instead of being refused.
func (c *Controller) UpdateReportSectionFields(
	ctx context.Context,
	userID, reportID, sectionID int64,
	title string, // the editor always sends the title block
	instructions *string,
	variablesJSON *string,
	warnings []string,
) (*database.ReportSection, error) {
	report, err := c.GetReport(ctx, userID, reportID)
	if err != nil {
		return nil, err
	}
	if _, err := c.deps.DB.GetReportSection(ctx, database.GetReportSectionParams{
		ID: sectionID, ReportID: reportID,
	}); err != nil {
		return nil, err
	}

	updated, err := c.deps.DB.UpdateReportSectionFields(ctx, database.UpdateReportSectionFieldsParams{
		ID:           sectionID,
		Title:        title,
		Instructions: nullString(nullableOf(instructions)),
		Variables:    parseVariablesJSON(variablesJSON),
		Warnings:     warnings,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update section %d: %w", sectionID, err)
	}

	if isRunActive(report) {
		if _, err := c.deps.DB.SetReportSectionRegenRequested(ctx, database.SetReportSectionRegenRequestedParams{
			ID: sectionID, RegenRequested: true,
		}); err != nil {
			return nil, fmt.Errorf("failed to request section %d regeneration: %w", sectionID, err)
		}
	}

	c.publish(ctx, userID, reportID, sectionID)
	return &updated, nil
}

// SetReportSectionContent writes the section content by hand (the editor); a
// not-written section becomes done once its content is set. While the run is
// generating the content is stored but the section is flagged for regeneration
// (a section the writer currently holds keeps its writing status and is
// rewritten by the drain loop).
func (c *Controller) SetReportSectionContent(
	ctx context.Context,
	userID, reportID, sectionID int64,
	content string,
) (*database.ReportSection, error) {
	report, err := c.GetReport(ctx, userID, reportID)
	if err != nil {
		return nil, err
	}
	st, err := c.deps.DB.GetReportSection(ctx, database.GetReportSectionParams{
		ID: sectionID, ReportID: reportID,
	})
	if err != nil {
		return nil, err
	}

	status := SectionDone
	if isRunActive(report) {
		status = SectionQueued
		if st.Status == SectionWriting {
			status = SectionWriting
		}
	}

	updated, err := c.deps.DB.UpdateReportSectionContent(ctx, database.UpdateReportSectionContentParams{
		ID: sectionID, Content: content, Status: status,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to set section %d content: %w", sectionID, err)
	}

	if isRunActive(report) {
		if _, err := c.deps.DB.SetReportSectionRegenRequested(ctx, database.SetReportSectionRegenRequestedParams{
			ID: sectionID, RegenRequested: true,
		}); err != nil {
			return nil, fmt.Errorf("failed to request section %d regeneration: %w", sectionID, err)
		}
	}

	c.publish(ctx, userID, reportID, sectionID)
	return &updated, nil
}

// HideReportSection toggles the editorial hidden flag (document + export).
func (c *Controller) HideReportSection(
	ctx context.Context, userID, reportID, sectionID int64, hidden bool,
) (*database.ReportSection, error) {
	if _, err := c.sectionIsEditable(ctx, userID, reportID); err != nil {
		return nil, err
	}
	if _, err := c.deps.DB.GetReportSection(ctx, database.GetReportSectionParams{
		ID: sectionID, ReportID: reportID,
	}); err != nil {
		return nil, err
	}

	updated, err := c.deps.DB.SetReportSectionHidden(ctx, database.SetReportSectionHiddenParams{
		ID: sectionID, Hidden: hidden,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to hide section %d: %w", sectionID, err)
	}
	c.publish(ctx, userID, reportID, sectionID)
	return &updated, nil
}

// MoveReportSection swaps the section one step up or down inside its parent.
func (c *Controller) MoveReportSection(
	ctx context.Context, userID, reportID, sectionID int64, up bool,
) error {
	if _, err := c.sectionIsEditable(ctx, userID, reportID); err != nil {
		return err
	}
	st, err := c.deps.DB.GetReportSection(ctx, database.GetReportSectionParams{
		ID: sectionID, ReportID: reportID,
	})
	if err != nil {
		return err
	}

	sections, err := c.deps.DB.GetReportSections(ctx, reportID)
	if err != nil {
		return err
	}
	siblings := orderedSections(sections, st.ParentID)
	for i := range siblings {
		if siblings[i].ID != sectionID {
			continue
		}
		var other *database.ReportSection
		if up && i > 0 {
			other = &siblings[i-1]
		}
		if !up && i < len(siblings)-1 {
			other = &siblings[i+1]
		}
		if other == nil {
			return nil // boundary: nothing to swap
		}
		if _, err := c.deps.DB.SetReportSectionPosition(ctx, database.SetReportSectionPositionParams{
			ID: other.ID, Position: st.Position,
		}); err != nil {
			return fmt.Errorf("failed to move section %d: %w", sectionID, err)
		}
		if _, err := c.deps.DB.SetReportSectionPosition(ctx, database.SetReportSectionPositionParams{
			ID: st.ID, Position: other.Position,
		}); err != nil {
			return fmt.Errorf("failed to move section %d: %w", sectionID, err)
		}
		c.publish(ctx, userID, reportID, sectionID)
		c.publish(ctx, userID, reportID, other.ID)
		break
	}
	return nil
}

// AddReportSection inserts a hand-made llm section (append at the document
// tail, or right after the given sibling, or as the new child of a parent).
func (c *Controller) AddReportSection(
	ctx context.Context, userID, reportID int64, afterSectionID, parentID *int64, title string,
) (*database.ReportSection, error) {
	if _, err := c.sectionIsEditable(ctx, userID, reportID); err != nil {
		return nil, err
	}

	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("section title is required")
	}

	params := database.CreateReportSectionParams{
		ReportID: reportID,
		Key:      "user-" + outlineSlug(title),
		Kind:     KindLLM,
		Title:    title,
	}

	switch {
	case afterSectionID != nil:
		after, err := c.deps.DB.GetReportSection(ctx, database.GetReportSectionParams{
			ID: *afterSectionID, ReportID: reportID,
		})
		if err != nil {
			return nil, err
		}
		params.ParentID = after.ParentID
		params.Position = after.Position + 1
		if err := c.shiftPositions(ctx, reportID, after.ParentID, params.Position); err != nil {
			return nil, err
		}

	case parentID != nil:
		sections, err := c.deps.DB.GetReportSections(ctx, reportID)
		if err != nil {
			return nil, err
		}
		children := orderedSections(sections, int64Null(*parentID))
		params.ParentID = int64Null(*parentID)
		if len(children) == 0 {
			params.Position = 1
		} else {
			params.Position = children[len(children)-1].Position + 1
		}

	default:
		max, err := c.deps.DB.GetMaxReportSectionPosition(ctx, reportID)
		if err != nil {
			return nil, err
		}
		fallback := int32(0)
		if max.Valid {
			fallback = max.Int32
		}
		params.Position = fallback + 1
	}

	row, err := c.deps.DB.CreateReportSection(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to add section: %w", err)
	}

	// The insert shifted every sibling at or past the new position (and, for the
	// append/parent cases, none), so publish the whole group to keep client
	// positions in sync.
	changed := []int64{row.ID}
	if all, err := c.deps.DB.GetReportSections(ctx, reportID); err == nil {
		for _, sibling := range all {
			if sibling.ID != row.ID && sameParent(sibling.ParentID, params.ParentID) {
				changed = append(changed, sibling.ID)
			}
		}
	}
	c.publishAll(ctx, userID, reportID, changed)

	return &row, nil
}

// DeleteReportSection removes a section and its subtree (the DB cascades).
func (c *Controller) DeleteReportSection(ctx context.Context, userID, reportID, sectionID int64) error {
	if _, err := c.sectionIsEditable(ctx, userID, reportID); err != nil {
		return err
	}
	st, err := c.deps.DB.GetReportSection(ctx, database.GetReportSectionParams{
		ID: sectionID, ReportID: reportID,
	})
	if err != nil {
		return err
	}
	if err := c.deps.DB.DeleteReportSection(ctx, database.DeleteReportSectionParams{ID: sectionID, ReportID: reportID}); err != nil {
		return fmt.Errorf("failed to delete section %d: %w", sectionID, err)
	}

	// Pull the following siblings back so positions stay contiguous, then
	// republish the group (the removed row itself is gone).
	if err := c.deps.DB.CompactReportSectionPositions(ctx, database.CompactReportSectionPositionsParams{
		ReportID: reportID, ParentID: st.ParentID, Position: st.Position,
	}); err != nil {
		return fmt.Errorf("failed to compact section positions: %w", err)
	}

	changed := []int64{}
	if all, err := c.deps.DB.GetReportSections(ctx, reportID); err == nil {
		for _, sibling := range all {
			if sameParent(sibling.ParentID, st.ParentID) {
				changed = append(changed, sibling.ID)
			}
		}
	}
	c.publishAll(ctx, userID, reportID, changed)

	return nil
}

// BrandingParams is the updateReportBranding payload. The four texts are always
// applied (empty clears the override). A nil Logo keeps the stored one, an
// empty Logo clears it, anything else is decoded and validated.
type BrandingParams struct {
	Title    *string
	Subtitle *string
	Header   *string
	Footer   *string
	Logo     *string
	LogoName string
}

// SetBranding persists the per-report export branding (plan § 6.6 / P6). The
// branding does not take part in the generation, so it is editable whatever the
// run status.
func (c *Controller) SetBranding(
	ctx context.Context, userID, reportID int64, params BrandingParams,
) (*database.Report, error) {
	if _, err := c.GetReport(ctx, userID, reportID); err != nil {
		return nil, err
	}
	if err := ValidateBrandingTexts(params.Title, params.Subtitle, params.Header, params.Footer); err != nil {
		return nil, err
	}

	// Decode the logo before touching the row: an invalid upload must not leave
	// the texts half-saved.
	var decoded *BrandingLogo
	if params.Logo != nil && strings.TrimSpace(*params.Logo) != "" {
		logo, err := DecodeBrandingLogo(*params.Logo, params.LogoName)
		if err != nil {
			return nil, err
		}
		decoded = logo
	}

	updated, err := c.deps.DB.SetReportBrandingText(ctx, database.SetReportBrandingTextParams{
		ID:               reportID,
		BrandingFooter:   brandingTextOrNull(params.Footer),
		BrandingHeader:   brandingTextOrNull(params.Header),
		BrandingSubtitle: brandingTextOrNull(params.Subtitle),
		BrandingTitle:    brandingTextOrNull(params.Title),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to save the report branding: %w", err)
	}

	if params.Logo == nil {
		return &updated, nil
	}
	if decoded == nil {
		if err := c.deps.DB.ClearReportBrandingLogo(ctx, reportID); err != nil {
			return nil, fmt.Errorf("failed to clear the report logo: %w", err)
		}
		return &updated, nil
	}

	if err := c.deps.DB.SetReportBrandingLogo(ctx, database.SetReportBrandingLogoParams{
		ReportID:  reportID,
		Data:      decoded.Bytes,
		Name:      decoded.Name,
		MediaType: decoded.MediaType,
	}); err != nil {
		return nil, fmt.Errorf("failed to save the report logo: %w", err)
	}

	return &updated, nil
}

// ErrNoBrandingLogo reports that the report carries no logo.
var ErrNoBrandingLogo = errors.New("report has no branding logo")

// BrandingLogo returns the stored logo bytes for the download endpoint.
func (c *Controller) BrandingLogo(
	ctx context.Context, userID, reportID int64,
) (name, mediaType string, data []byte, err error) {
	report, err := c.GetReport(ctx, userID, reportID)
	if err != nil {
		return "", "", nil, err
	}

	logo, err := c.deps.DB.GetReportBrandingLogo(ctx, report.ID)
	if err != nil {
		return "", "", nil, ErrNoBrandingLogo
	}
	return logo.Name, logo.MediaType, logo.Data, nil
}

// BrandingLogoInfo tells whether a logo exists without reading its bytes.
func (c *Controller) BrandingLogoInfo(
	ctx context.Context, reportID int64,
) (name, mediaType string, err error) {
	info, err := c.deps.DB.GetReportBrandingLogoInfo(ctx, reportID)
	if err != nil {
		return "", "", ErrNoBrandingLogo
	}
	return info.Name, info.MediaType, nil
}

func (c *Controller) shiftPositions(
	ctx context.Context, reportID int64, parent sql.NullInt64, from int32,
) error {
	return c.deps.DB.ShiftReportSectionPositions(ctx, database.ShiftReportSectionPositionsParams{
		ReportID: reportID,
		ParentID: parent,
		Position: from,
	})
}

// publish re-emits one section (and its report row) to the subscribers.
func (c *Controller) publish(ctx context.Context, userID, reportID, sectionID int64) {
	c.publishAll(ctx, userID, reportID, []int64{sectionID})
}

// publishAll re-emits several sections and the report row once. A position shift
// moves a whole sibling group, so every moved section must be published or a
// client keeps stale positions and orders the new section against them.
func (c *Controller) publishAll(ctx context.Context, userID, reportID int64, sectionIDs []int64) {
	report, err := c.deps.DB.GetReport(ctx, database.GetReportParams{ID: reportID, UserID: userID})
	if err != nil {
		return
	}
	pub := c.deps.Subs.NewFlowPublisher(userID, report.FlowID)
	for _, sectionID := range sectionIDs {
		if row, err := c.deps.DB.GetReportSection(ctx, database.GetReportSectionParams{
			ID: sectionID, ReportID: reportID,
		}); err == nil {
			pub.ReportSectionUpdated(ctx, converter.ConvertReportSection(row))
		}
	}
	pub.ReportUpdated(ctx, converter.ConvertReport(report))
}

func nullableOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// parseVariablesJSON validates the editor payload (a JSON array of
// {"name","value"} pairs) and returns the raw message for the DB column.
func parseVariablesJSON(text *string) json.RawMessage {
	if text == nil || strings.TrimSpace(*text) == "" {
		return json.RawMessage("[]")
	}

	var parsed []map[string]any
	if err := json.Unmarshal([]byte(*text), &parsed); err != nil {
		return nil
	}
	return json.RawMessage(*text)
}

func payloadToNullJSON(payload map[string]any) pqtype.NullRawMessage {
	if len(payload) == 0 {
		return pqtype.NullRawMessage{}
	}
	blob, err := json.Marshal(payload)
	if err != nil {
		return pqtype.NullRawMessage{}
	}
	return pqtype.NullRawMessage{Valid: true, RawMessage: blob}
}

func nullString(text string) sql.NullString {
	if strings.TrimSpace(text) == "" {
		return sql.NullString{}
	}
	return sql.NullString{Valid: true, String: text}
}

func nullInstructionText(text string) sql.NullString {
	return nullString(text)
}

func nullableText(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

// TemplateInfo is one selectable template in the Create-report dialog.
type TemplateInfo struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// ListTemplates returns the bundled templates (v1: one default; user-defined
// templates arrive with the Settings interface, plan § 6.9).
func (c *Controller) ListTemplates() ([]TemplateInfo, error) {
	keys, err := ListTemplates()
	if err != nil {
		return nil, err
	}

	out := make([]TemplateInfo, 0, len(keys))
	for _, key := range keys {
		label := key
		if tpl, err := GetTemplate(key); err == nil && tpl.Label != "" {
			label = tpl.Label
		}
		out = append(out, TemplateInfo{Key: key, Label: label})
	}
	return out, nil
}

// ListSections returns the section rows of one report in document order.
func (c *Controller) ListSections(ctx context.Context, reportID int64) ([]database.ReportSection, error) {
	return c.deps.DB.GetReportSections(ctx, reportID)
}

// ListLogs returns the generation console log rows (oldest first).
func (c *Controller) ListLogs(ctx context.Context, reportID int64, limit int) ([]database.ReportLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	return c.deps.DB.GetReportLogs(ctx, database.GetReportLogsParams{
		ReportID: reportID, Limit: int64(limit),
	})
}

// Document assembles the export/reading markdown of one report.
func (c *Controller) Document(ctx context.Context, reportID int64) (string, error) {
	return BuildDocument(ctx, c.deps.DB, reportID)
}

// EvidenceRefs returns the evidence the report's sections cited, deduplicated
// across sections (plan § 6.10). The archive bundles what it holds.
func (c *Controller) EvidenceRefs(ctx context.Context, reportID int64) ([]EvidenceRef, error) {
	sections, err := c.deps.DB.GetReportSections(ctx, reportID)
	if err != nil {
		return nil, fmt.Errorf("failed to load report %d sections: %w", reportID, err)
	}

	all := make([]EvidenceRef, 0, len(sections))
	for i := range sections {
		refs, err := DecodeEvidenceRefs(sections[i].EvidenceRefs)
		if err != nil {
			// A malformed manifest must not sink the archive: skip that section.
			continue
		}
		all = append(all, refs...)
	}

	return DedupeEvidenceRefs(all), nil
}
