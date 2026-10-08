package graph

import (
	"context"

	"pentagi/pkg/controller"
	"pentagi/pkg/database"
	"pentagi/pkg/graph/model"
)

// These helpers back the Report resolvers. They live in a hand-written file:
// gqlgen would park them inside schema.resolvers.go's "moved code" block on
// the next regeneration and the build would fail.

// validateReport checks the flows.read permission and the report ownership
// (the controller GetReport is user-scoped, so admins see only their own
// reports in v1).
func validateReport(
	ctx context.Context,
	reports controller.ReportController,
	reportID int64,
) (int64, error) {
	uid, _, err := validatePermission(ctx, "flows.read")
	if err != nil {
		return 0, err
	}

	if _, err := reports.GetReport(ctx, uid, reportID); err != nil {
		return 0, err
	}

	return uid, nil
}

// validateReportRow returns the owned report row on top of the check above.
func validateReportRow(
	ctx context.Context,
	reports controller.ReportController,
	reportID int64,
) (int64, *database.Report, error) {
	uid, _, err := validatePermission(ctx, "flows.read")
	if err != nil {
		return 0, nil, err
	}

	report, err := reports.GetReport(ctx, uid, reportID)
	if err != nil {
		return 0, nil, err
	}

	return uid, report, nil
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
