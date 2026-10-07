package controller

import (
	"context"
	"database/sql"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/graph/subscriptions"
	"pentagi/pkg/providers"
	"pentagi/pkg/report"
	"pentagi/pkg/templates"
)

// ReportController is the resolver-facing surface of the reports subsystem
// (fork extended-version section 6).
type ReportController interface {
	Enabled() bool
	CreateReport(
		ctx context.Context,
		userID, flowID int64,
		params CreateReportParams,
	) (*database.Report, error)
	// LoadReports resumes the runs that were mid-flight at process start.
	LoadReports(ctx context.Context) error
	ListFlowReports(ctx context.Context, userID, flowID int64) ([]database.Report, error)
	ListReportTemplates() ([]report.TemplateInfo, error)
	GetReport(ctx context.Context, userID, reportID int64) (*database.Report, error)
	ListReportSections(ctx context.Context, userID, reportID int64) ([]database.ReportSection, error)
	ListReportLogs(ctx context.Context, userID, reportID int64, limit int) ([]database.ReportLog, error)
	Document(ctx context.Context, userID, reportID int64) (string, error)

	PutNote(ctx context.Context, userID, reportID int64, note string) error
	Stop(ctx context.Context, userID, reportID int64) error
	Delete(ctx context.Context, userID, reportID int64) error

	UpdateSection(
		ctx context.Context, userID, reportID, sectionID int64,
		title string,
		instructions, variables *string,
		warnings []string,
	) (*database.ReportSection, error)
	SetSectionContent(ctx context.Context, userID, reportID, sectionID int64, content string) (*database.ReportSection, error)
	MoveSection(ctx context.Context, userID, reportID, sectionID int64, up bool) error
	HideSection(ctx context.Context, userID, reportID, sectionID int64, hidden bool) (*database.ReportSection, error)
	AddSection(
		ctx context.Context, userID, reportID int64,
		afterSectionID, parentID *int64, title string,
	) (*database.ReportSection, error)
	DeleteSection(ctx context.Context, userID, reportID, sectionID int64) error
}

// CreateReportParams carries the mutation payload (zero budget values mean
// "keep the server default").
type CreateReportParams struct {
	TemplateKey            string
	ProviderName           string
	Title                  string
	AdditionalInstructions string
	ControlBudgetSeconds   int
	ContentBudgetSeconds   int
}

type reportControllerAdapter struct {
	ctrl *report.Controller
}

// NewReportController builds the reports subsystem and wraps it in the
// resolver-facing controller.
func NewReportController(
	db database.Querier,
	cfg *config.Config,
	provs providers.ProviderController,
	prompter templates.Prompter,
	subs subscriptions.SubscriptionsController,
) ReportController {
	ctrl := report.NewController(report.Deps{
		DB:       db,
		Cfg:      cfg,
		Provs:    provs,
		Prompter: prompter,
		Subs:     subs,
	})
	return &reportControllerAdapter{ctrl: ctrl}
}

func (a *reportControllerAdapter) Enabled() bool {
	return a.ctrl.Enabled()
}

func (a *reportControllerAdapter) CreateReport(
	ctx context.Context,
	userID, flowID int64,
	params CreateReportParams,
) (*database.Report, error) {
	created, err := a.ctrl.CreateReport(ctx, userID, flowID, report.CreateParams{
		TemplateKey:            params.TemplateKey,
		ProviderName:           params.ProviderName,
		Title:                  params.Title,
		AdditionalInstructions: params.AdditionalInstructions,
		ControlBudgetSeconds:   params.ControlBudgetSeconds,
		ContentBudgetSeconds:   params.ContentBudgetSeconds,
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (a *reportControllerAdapter) LoadReports(ctx context.Context) error {
	return a.ctrl.LoadReports(ctx)
}

func (a *reportControllerAdapter) ListFlowReports(
	ctx context.Context, userID, flowID int64,
) ([]database.Report, error) {
	return a.ctrl.ListFlowReports(ctx, userID, flowID)
}

func (a *reportControllerAdapter) ListReportTemplates() ([]report.TemplateInfo, error) {
	return a.ctrl.ListTemplates()
}

func (a *reportControllerAdapter) GetReport(
	ctx context.Context, userID, reportID int64,
) (*database.Report, error) {
	return a.ctrl.GetReport(ctx, userID, reportID)
}

func (a *reportControllerAdapter) ListReportSections(
	ctx context.Context, userID, reportID int64,
) ([]database.ReportSection, error) {
	if _, err := a.ctrl.GetReport(ctx, userID, reportID); err != nil {
		return nil, err
	}
	return a.ctrl.ListSections(ctx, reportID)
}

func (a *reportControllerAdapter) ListReportLogs(
	ctx context.Context, userID, reportID int64, limit int,
) ([]database.ReportLog, error) {
	if _, err := a.ctrl.GetReport(ctx, userID, reportID); err != nil {
		return nil, err
	}
	return a.ctrl.ListLogs(ctx, reportID, limit)
}

func (a *reportControllerAdapter) Document(
	ctx context.Context, userID, reportID int64,
) (string, error) {
	if _, err := a.ctrl.GetReport(ctx, userID, reportID); err != nil {
		return "", err
	}
	return a.ctrl.Document(ctx, reportID)
}

func (a *reportControllerAdapter) PutNote(ctx context.Context, userID, reportID int64, note string) error {
	return a.ctrl.PutNote(ctx, userID, reportID, note)
}

func (a *reportControllerAdapter) Stop(ctx context.Context, userID, reportID int64) error {
	return a.ctrl.StopReport(ctx, userID, reportID)
}

func (a *reportControllerAdapter) Delete(ctx context.Context, userID, reportID int64) error {
	return a.ctrl.DeleteReport(ctx, userID, reportID)
}

func (a *reportControllerAdapter) UpdateSection(
	ctx context.Context, userID, reportID, sectionID int64,
	title string,
	instructions, variables *string,
	warnings []string,
) (*database.ReportSection, error) {
	return a.ctrl.UpdateReportSectionFields(ctx, userID, reportID, sectionID, title, instructions, variables, warnings)
}

func (a *reportControllerAdapter) SetSectionContent(
	ctx context.Context, userID, reportID, sectionID int64, content string,
) (*database.ReportSection, error) {
	return a.ctrl.SetReportSectionContent(ctx, userID, reportID, sectionID, content)
}

func (a *reportControllerAdapter) MoveSection(
	ctx context.Context, userID, reportID, sectionID int64, up bool,
) error {
	return a.ctrl.MoveReportSection(ctx, userID, reportID, sectionID, up)
}

func (a *reportControllerAdapter) HideSection(
	ctx context.Context, userID, reportID, sectionID int64, hidden bool,
) (*database.ReportSection, error) {
	return a.ctrl.HideReportSection(ctx, userID, reportID, sectionID, hidden)
}

func (a *reportControllerAdapter) AddSection(
	ctx context.Context, userID, reportID int64,
	afterSectionID, parentID *int64, title string,
) (*database.ReportSection, error) {
	return a.ctrl.AddReportSection(ctx, userID, reportID, afterSectionID, parentID, title)
}

func (a *reportControllerAdapter) DeleteSection(
	ctx context.Context, userID, reportID, sectionID int64,
) error {
	return a.ctrl.DeleteReportSection(ctx, userID, reportID, sectionID)
}

var _ = sql.NullString{} // (kept: database/sql import used by generated code nearby)
