package report

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"pentagi/pkg/database"
	obs "pentagi/pkg/observability"
)

// TemplateSource tells where a listed template comes from: a bundled file, the
// caller's own row, or a global (admin-managed) row.
type TemplateSource string

const (
	TemplateSourceBundled TemplateSource = "bundled"
	TemplateSourceUser    TemplateSource = "user"
	TemplateSourceGlobal  TemplateSource = "global"
)

// TemplateInfo is one template as the Settings page and the Create-report
// picker list it. ID is 0 for a bundled template (nothing to edit); a bundled
// row is always enabled and cannot be deleted.
type TemplateInfo struct {
	ID        int64          `json:"id"`
	Key       string         `json:"key"`
	Label     string         `json:"label"`
	Version   int32          `json:"version"`
	Yaml      string         `json:"yaml"`
	Enabled   bool           `json:"enabled"`
	Source    TemplateSource `json:"source"`
	UserID    *int64         `json:"user_id,omitempty"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
}

// TemplateParams is the create/update payload. The YAML is the source of truth:
// key, label and version are read back from it, so the editor only owns one
// document.
type TemplateParams struct {
	Yaml    string
	Enabled bool
}

// TemplateBrandingParams is the template branding payload, mirroring the
// per-report BrandingParams: a nil Logo keeps the stored one, an empty Logo
// clears it.
type TemplateBrandingParams struct {
	Title    *string
	Subtitle *string
	Header   *string
	Footer   *string
	Logo     *string
	LogoName string
}

// TemplateBrandingView is the resolver-facing branding of one template.
type TemplateBrandingView struct {
	Title         string
	Subtitle      string
	Header        string
	Footer        string
	HasLogo       bool
	LogoName      string
	LogoMediaType string
}

// ListTemplates merges the DB templates visible to the caller (own + global)
// with the bundled ones, deduplicated by key with the DB winning (backlog v2
// L3). A user's own template shadows a global of the same key, which in turn
// shadows the bundled one.
func (c *Controller) ListTemplates(ctx context.Context, userID int64) ([]TemplateInfo, error) {
	rows, err := c.deps.DB.ListReportTemplates(ctx, sql.NullInt64{Int64: userID, Valid: userID != 0})
	if err != nil {
		return nil, fmt.Errorf("failed to list report templates: %w", err)
	}

	out := make([]TemplateInfo, 0, len(rows)+1)
	seen := make(map[string]struct{}, len(rows)+1)
	for _, row := range rows {
		out = append(out, TemplateInfoOf(row))
		seen[row.Key] = struct{}{}
	}

	keys, err := ListTemplates()
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		if _, dup := seen[key]; dup {
			continue
		}
		tpl, err := GetTemplate(key)
		if err != nil {
			continue
		}
		out = append(out, TemplateInfo{
			Key:     tpl.Key,
			Label:   tpl.Label,
			Version: int32(tpl.Version),
			Enabled: true,
			Source:  TemplateSourceBundled,
		})
	}

	return out, nil
}

// TemplateInfoOf reads a stored template row into the list shape (L3).
func TemplateInfoOf(row database.ReportTemplate) TemplateInfo {
	info := TemplateInfo{
		ID:        row.ID,
		Key:       row.Key,
		Label:     row.Label,
		Version:   row.Version,
		Yaml:      row.Yaml,
		Enabled:   row.Enabled,
		Source:    TemplateSourceGlobal,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if row.UserID.Valid {
		info.Source = TemplateSourceUser
		uid := row.UserID.Int64
		info.UserID = &uid
	}
	return info
}

// GetReportTemplate returns one template row the caller may see (their own or a
// global one).
func (c *Controller) GetReportTemplate(ctx context.Context, userID, templateID int64) (*database.ReportTemplate, error) {
	row, err := c.deps.DB.GetReportTemplate(ctx, templateID)
	if err != nil {
		return nil, err
	}
	if row.UserID.Valid && row.UserID.Int64 != userID {
		return nil, fmt.Errorf("%w: template %d belongs to another user", ErrForbidden, templateID)
	}
	return &row, nil
}

// CreateReportTemplate stores a new template. A global (user_id NULL) template
// needs the admin flag; a personal one belongs to the caller. The YAML must
// parse and validate, and its key must not collide with another of the same
// scope.
func (c *Controller) CreateReportTemplate(
	ctx context.Context,
	userID int64,
	isAdmin bool,
	params TemplateParams,
	global bool,
) (*database.ReportTemplate, error) {
	if global && !isAdmin {
		return nil, fmt.Errorf("%w: a global template needs reports.admin", ErrForbidden)
	}

	tpl, err := ParseTemplate([]byte(params.Yaml), "")
	if err != nil {
		return nil, err
	}

	var owner sql.NullInt64
	if !global {
		owner = sql.NullInt64{Int64: userID, Valid: true}
	}

	row, err := c.deps.DB.CreateReportTemplate(ctx, database.CreateReportTemplateParams{
		UserID:  owner,
		Key:     tpl.Key,
		Label:   tpl.Label,
		Version: int32(tpl.Version),
		Yaml:    params.Yaml,
		Enabled: params.Enabled,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("a template with the key %q already exists", tpl.Key)
		}
		return nil, fmt.Errorf("failed to create report template: %w", err)
	}

	return &row, nil
}

// UpdateReportTemplate rewrites a template the caller owns (or a global one when
// they are admin). The YAML is re-validated; the key may change as long as it
// stays unique within its scope.
func (c *Controller) UpdateReportTemplate(
	ctx context.Context,
	userID, templateID int64,
	isAdmin bool,
	params TemplateParams,
) (*database.ReportTemplate, error) {
	row, err := c.GetReportTemplate(ctx, userID, templateID)
	if err != nil {
		return nil, err
	}
	if err := c.authorizeTemplateRow(*row, userID, isAdmin); err != nil {
		return nil, err
	}

	tpl, err := ParseTemplate([]byte(params.Yaml), "")
	if err != nil {
		return nil, err
	}

	updated, err := c.deps.DB.UpdateReportTemplate(ctx, database.UpdateReportTemplateParams{
		ID:               templateID,
		UserID:           sql.NullInt64{Int64: userID, Valid: true},
		Key:              tpl.Key,
		Label:            tpl.Label,
		Version:          int32(tpl.Version),
		Yaml:             params.Yaml,
		Enabled:          params.Enabled,
		BrandingTitle:    row.BrandingTitle,
		BrandingSubtitle: row.BrandingSubtitle,
		BrandingHeader:   row.BrandingHeader,
		BrandingFooter:   row.BrandingFooter,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("a template with the key %q already exists", tpl.Key)
		}
		return nil, fmt.Errorf("failed to update report template: %w", err)
	}

	return &updated, nil
}

// SetReportTemplateEnabled toggles one template the caller owns (or a global
// one when they are admin). A disabled template leaves the Create picker but
// still resolves existing reports.
func (c *Controller) SetReportTemplateEnabled(
	ctx context.Context,
	userID, templateID int64,
	isAdmin bool,
	enabled bool,
) (*database.ReportTemplate, error) {
	row, err := c.GetReportTemplate(ctx, userID, templateID)
	if err != nil {
		return nil, err
	}
	if err := c.authorizeTemplateRow(*row, userID, isAdmin); err != nil {
		return nil, err
	}

	updated, err := c.deps.DB.SetReportTemplateEnabled(ctx, database.SetReportTemplateEnabledParams{
		ID: templateID, Enabled: enabled,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to toggle report template: %w", err)
	}
	return &updated, nil
}

// DeleteReportTemplate removes a template the caller owns (or a global one when
// they are admin). Reports that snapshotted its key keep rendering through the
// bundled fallback.
func (c *Controller) DeleteReportTemplate(ctx context.Context, userID, templateID int64, isAdmin bool) error {
	row, err := c.GetReportTemplate(ctx, userID, templateID)
	if err != nil {
		return err
	}
	if err := c.authorizeTemplateRow(*row, userID, isAdmin); err != nil {
		return err
	}

	if err := c.deps.DB.DeleteReportTemplate(ctx, database.DeleteReportTemplateParams{
		ID:     templateID,
		UserID: sql.NullInt64{Int64: userID, Valid: true},
	}); err != nil {
		return fmt.Errorf("failed to delete report template: %w", err)
	}
	return nil
}

// authorizeTemplateRow guards a mutation: a personal template belongs to its
// owner, a global one is admin-only.
func (c *Controller) authorizeTemplateRow(row database.ReportTemplate, userID int64, isAdmin bool) error {
	if !row.UserID.Valid {
		if !isAdmin {
			return fmt.Errorf("%w: a global template needs reports.admin", ErrForbidden)
		}
		return nil
	}
	if row.UserID.Int64 != userID {
		return fmt.Errorf("%w: template %d belongs to another user", ErrForbidden, row.ID)
	}
	return nil
}

// TemplateBranding returns the branding a report inherits from its template,
// nil when the template is bundled, missing or carries no branding (L3). Used
// by the GraphQL branding view and the export path.
func (c *Controller) TemplateBranding(ctx context.Context, userID int64, key string) *TemplateBranding {
	ctx, span := obs.Observer.NewSpan(ctx, obs.SpanKindInternal, "report.controller.templateBranding")
	defer span.End()

	return templateBrandingFor(ctx, c.deps.DB, userID, key)
}

// SetTemplateBranding persists the branding texts and (tri-state) logo of one
// template the caller may edit.
func (c *Controller) SetTemplateBranding(
	ctx context.Context,
	userID, templateID int64,
	isAdmin bool,
	params TemplateBrandingParams,
) (*database.ReportTemplate, error) {
	row, err := c.GetReportTemplate(ctx, userID, templateID)
	if err != nil {
		return nil, err
	}
	if err := c.authorizeTemplateRow(*row, userID, isAdmin); err != nil {
		return nil, err
	}
	if err := ValidateBrandingTexts(params.Title, params.Subtitle, params.Header, params.Footer); err != nil {
		return nil, err
	}

	// Decode the logo before touching the row: an invalid upload must not leave
	// half-applied branding behind.
	var decoded *BrandingLogo
	if params.Logo != nil && strings.TrimSpace(*params.Logo) != "" {
		decoded, err = DecodeBrandingLogo(*params.Logo, params.LogoName)
		if err != nil {
			return nil, err
		}
	}

	updated, err := c.deps.DB.SetReportTemplateBrandingText(ctx, database.SetReportTemplateBrandingTextParams{
		ID:               templateID,
		BrandingTitle:    brandingTextOrNull(params.Title),
		BrandingSubtitle: brandingTextOrNull(params.Subtitle),
		BrandingHeader:   brandingTextOrNull(params.Header),
		BrandingFooter:   brandingTextOrNull(params.Footer),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to store template branding: %w", err)
	}

	if params.Logo != nil {
		if decoded == nil {
			if err := c.deps.DB.ClearReportTemplateBrandingLogo(ctx, templateID); err != nil {
				return nil, fmt.Errorf("failed to clear the template logo: %w", err)
			}
		} else if err := c.deps.DB.SetReportTemplateBrandingLogo(ctx, database.SetReportTemplateBrandingLogoParams{
			TemplateID: templateID,
			Data:       decoded.Bytes,
			Name:       decoded.Name,
			MediaType:  decoded.MediaType,
		}); err != nil {
			return nil, fmt.Errorf("failed to store the template logo: %w", err)
		}
	}

	return &updated, nil
}

// TemplateBrandingView assembles the branding view of one template row.
func (c *Controller) TemplateBrandingView(ctx context.Context, row database.ReportTemplate) TemplateBrandingView {
	info, err := c.deps.DB.GetReportTemplateBrandingLogoInfo(ctx, row.ID)

	return TemplateBrandingView{
		Title:         row.BrandingTitle.String,
		Subtitle:      row.BrandingSubtitle.String,
		Header:        row.BrandingHeader.String,
		Footer:        row.BrandingFooter.String,
		HasLogo:       err == nil,
		LogoName:      info.Name,
		LogoMediaType: info.MediaType,
	}
}

// TemplateBrandingLogo returns the stored logo bytes of one template.
func (c *Controller) TemplateBrandingLogo(
	ctx context.Context,
	userID, templateID int64,
) (name, mediaType string, data []byte, err error) {
	if _, err := c.GetReportTemplate(ctx, userID, templateID); err != nil {
		return "", "", nil, err
	}
	logo, err := c.deps.DB.GetReportTemplateBrandingLogo(ctx, templateID)
	if err != nil {
		return "", "", nil, ErrNoBrandingLogo
	}
	return logo.Name, logo.MediaType, logo.Data, nil
}

// isUniqueViolation reports whether the error is a Postgres unique-constraint
// violation (SQLSTATE 23505), so a duplicate template key reads as one.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}
