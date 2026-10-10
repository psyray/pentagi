package graph

import (
	"context"

	"pentagi/pkg/controller"
	"pentagi/pkg/database"
	"pentagi/pkg/database/converter"
	"pentagi/pkg/graph/model"
	"pentagi/pkg/report"
)

// These helpers back the Report resolvers. They live in a hand-written file:
// gqlgen would park them inside schema.resolvers.go's "moved code" block on
// the next regeneration and the build would fail.

// validateReportPerm checks a report permission and the report ownership (the
// controller GetReport is user-scoped, so a user only reaches their own
// reports; admins included, as in v1).
func validateReportPerm(
	ctx context.Context,
	reports controller.ReportController,
	reportID int64,
	perm string,
) (int64, *database.Report, error) {
	uid, _, err := validatePermission(ctx, perm)
	if err != nil {
		return 0, nil, err
	}

	report, err := reports.GetReport(ctx, uid, reportID)
	if err != nil {
		return 0, nil, err
	}

	return uid, report, nil
}

// validateReport checks the reports.read permission and the report ownership.
func validateReport(
	ctx context.Context,
	reports controller.ReportController,
	reportID int64,
) (int64, error) {
	uid, _, err := validateReportPerm(ctx, reports, reportID, "reports.read")
	return uid, err
}

// validateReportRow returns the owned report row under reports.read.
func validateReportRow(
	ctx context.Context,
	reports controller.ReportController,
	reportID int64,
) (int64, *database.Report, error) {
	return validateReportPerm(ctx, reports, reportID, "reports.read")
}

// validateReportEdit checks reports.edit, the permission behind every content
// mutation (sections, branding, notes, stop).
func validateReportEdit(
	ctx context.Context,
	reports controller.ReportController,
	reportID int64,
) (int64, *database.Report, error) {
	return validateReportPerm(ctx, reports, reportID, "reports.edit")
}

// validateReportDelete checks reports.delete, the permission behind the report
// removal (a distinct right from editing).
func validateReportDelete(
	ctx context.Context,
	reports controller.ReportController,
	reportID int64,
) (int64, *database.Report, error) {
	return validateReportPerm(ctx, reports, reportID, "reports.delete")
}

// attachAttachments fills the Attachments field of converted sections from one
// report's attachment rows (backlog v2, L6). The field lives on the generated
// model and a list needs a single query, grouped here by section id. A nil slice
// would violate the non-null `[ReportAttachment!]!`, so every section gets at
// least an empty slice. A load failure degrades to empty lists rather than
// blanking the document.
func attachAttachments(
	ctx context.Context,
	reports controller.ReportController,
	uid, reportID int64,
	sections []*model.ReportSection,
) {
	if len(sections) == 0 {
		return
	}

	bySection := make(map[int64][]*model.ReportAttachment, len(sections))
	if rows, err := reports.ListReportAttachments(ctx, uid, reportID); err == nil {
		for i := range rows {
			bySection[rows[i].SectionID] = append(bySection[rows[i].SectionID], converter.ConvertReportAttachment(rows[i]))
		}
	}

	for _, section := range sections {
		attachments := bySection[section.ID]
		if attachments == nil {
			attachments = []*model.ReportAttachment{}
		}
		section.Attachments = attachments
	}
}

// deref returns the string value or an empty string.
func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// derefInt returns the int value or zero.
func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// optionalString returns nil for an empty value, so an unset branding override
// reads as null rather than an empty string.
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// brandingModel maps the controller's branding view onto the GraphQL type.
func brandingModel(view controller.BrandingView) *model.ReportBranding {
	return &model.ReportBranding{
		Title:            optionalString(view.Title),
		Subtitle:         optionalString(view.Subtitle),
		Header:           optionalString(view.Header),
		Footer:           optionalString(view.Footer),
		ResolvedTitle:    view.ResolvedTitle,
		ResolvedSubtitle: view.ResolvedSubtitle,
		ResolvedHeader:   view.ResolvedHeader,
		ResolvedFooter:   view.ResolvedFooter,
		HasLogo:          view.HasLogo,
		LogoName:         optionalString(view.LogoName),
		LogoMediaType:    optionalString(view.LogoMediaType),
	}
}

// reportTemplateModel maps a listed template (plus its branding) onto the
// GraphQL type (backlog v2, L3). A bundled template (id 0) carries no branding.
func reportTemplateModel(info report.TemplateInfo, branding report.TemplateBrandingView) *model.ReportTemplate {
	out := &model.ReportTemplate{
		ID:      info.ID,
		Key:     info.Key,
		Label:   info.Label,
		Version: int(info.Version),
		Yaml:    info.Yaml,
		Enabled: info.Enabled,
		Source:  model.ReportTemplateSource(info.Source),
		UserID:  info.UserID,
		Branding: &model.ReportTemplateBranding{
			Title:         optionalString(branding.Title),
			Subtitle:      optionalString(branding.Subtitle),
			Header:        optionalString(branding.Header),
			Footer:        optionalString(branding.Footer),
			HasLogo:       branding.HasLogo,
			LogoName:      optionalString(branding.LogoName),
			LogoMediaType: optionalString(branding.LogoMediaType),
		},
	}
	if !info.CreatedAt.IsZero() {
		created := info.CreatedAt
		out.CreatedAt = &created
	}
	if !info.UpdatedAt.IsZero() {
		updated := info.UpdatedAt
		out.UpdatedAt = &updated
	}
	return out
}

// reportTemplateView builds the GraphQL view of one stored template row,
// loading its branding.
func reportTemplateView(
	ctx context.Context,
	reports controller.ReportController,
	uid int64,
	row database.ReportTemplate,
) *model.ReportTemplate {
	branding, _ := reports.GetReportTemplateBranding(ctx, uid, row.ID)
	return reportTemplateModel(report.TemplateInfoOf(row), branding)
}

// templateBrandingModel maps a template's branding view onto the GraphQL type.
func templateBrandingModel(view report.TemplateBrandingView) *model.ReportTemplateBranding {
	return &model.ReportTemplateBranding{
		Title:         optionalString(view.Title),
		Subtitle:      optionalString(view.Subtitle),
		Header:        optionalString(view.Header),
		Footer:        optionalString(view.Footer),
		HasLogo:       view.HasLogo,
		LogoName:      optionalString(view.LogoName),
		LogoMediaType: optionalString(view.LogoMediaType),
	}
}
