package report

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"pentagi/pkg/database"
)

// Branding caps: the logo travels through GraphQL as a data URL, so it is kept
// small; the texts are short by nature (plan § 6.6 / P6).
const (
	BrandingMaxLogoBytes   = 256 * 1024
	BrandingMaxTitleLen    = 512
	BrandingMaxSubtitleLen = 512
	BrandingMaxHeaderLen   = 512
	BrandingMaxFooterLen   = 1024
)

// BrandingLogo is a decoded, validated logo ready to be stored.
type BrandingLogo struct {
	Bytes     []byte
	MediaType string
	Name      string
}

// EffectiveBranding is what the exports print: the report's overrides resolved
// over the localized defaults.
type EffectiveBranding struct {
	Title    string
	Subtitle string
	Header   string
	Footer   string
}

// DecodeBrandingLogo accepts a data URL (`data:image/png;base64,…`) or raw
// base64 and returns the validated bytes. Only png and jpeg are accepted: the
// PDF and Word renderers handle no other raster, and svg would need rasterizing.
func DecodeBrandingLogo(raw, name string) (*BrandingLogo, error) {
	payload := strings.TrimSpace(raw)
	if payload == "" {
		return nil, fmt.Errorf("empty logo payload")
	}

	if strings.HasPrefix(payload, "data:") {
		comma := strings.IndexByte(payload, ',')
		if comma < 0 {
			return nil, fmt.Errorf("malformed data URL")
		}
		payload = payload[comma+1:]
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
	if err != nil {
		return nil, fmt.Errorf("logo is not valid base64: %w", err)
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("empty logo")
	}
	if len(decoded) > BrandingMaxLogoBytes {
		return nil, fmt.Errorf("logo is %d bytes, the limit is %d", len(decoded), BrandingMaxLogoBytes)
	}

	// Sniff rather than trust a declared type: a mislabelled payload must not
	// reach the renderers.
	mediaType := http.DetectContentType(decoded)
	if mediaType != "image/png" && mediaType != "image/jpeg" {
		return nil, fmt.Errorf("unsupported logo type %q (png or jpeg only)", mediaType)
	}

	return &BrandingLogo{Bytes: decoded, MediaType: mediaType, Name: brandingLogoName(name, mediaType)}, nil
}

func brandingLogoName(name, mediaType string) string {
	cleaned := strings.TrimSpace(name)
	cleaned = strings.ReplaceAll(cleaned, "/", "-")
	cleaned = strings.ReplaceAll(cleaned, "\\", "-")
	if cleaned == "" {
		if mediaType == "image/jpeg" {
			return "logo.jpeg"
		}
		return "logo.png"
	}
	if len(cleaned) > 128 {
		cleaned = cleaned[:128]
	}
	return cleaned
}

// A TemplateBranding is the branding stored on a report template (backlog v2,
// L3): the values every report using that template inherits when it has no
// override of its own. Empty fields mean "not set".
type TemplateBranding struct {
	Title    string
	Subtitle string
	Header   string
	Footer   string
}

// TemplateBrandingFromRow reads the branding stored on a template row.
func TemplateBrandingFromRow(row database.ReportTemplate) TemplateBranding {
	return TemplateBranding{
		Title:    row.BrandingTitle.String,
		Subtitle: row.BrandingSubtitle.String,
		Header:   row.BrandingHeader.String,
		Footer:   row.BrandingFooter.String,
	}
}

// ResolveReportBranding applies a report's branding overrides over the localized
// defaults, so every consumer (composed sections, GraphQL, exports) prints the
// same values. The template's own branding sits between the report override and
// the report's title (backlog v2 L3): report override -> template branding ->
// report.Title -> report.TemplateLabel -> i18n labels. A nil template (bundled
// or unknown) leaves the historic chain untouched. `[[page]]`/`[[pages]]` are
// left for the page renderers.
func ResolveReportBranding(report database.Report, tpl *TemplateBranding) EffectiveBranding {
	labels := resolveLabels(report)

	title := strings.TrimSpace(report.BrandingTitle.String)
	if title == "" && tpl != nil {
		title = strings.TrimSpace(tpl.Title)
	}
	if title == "" {
		title = strings.TrimSpace(report.Title)
	}
	if title == "" {
		title = strings.TrimSpace(report.TemplateLabel)
	}

	date := nullTimeOrNow(report.CreatedAt).UTC().Format("2006-01-02")
	tokens := map[string]string{"title": title, "date": date}

	header := strings.TrimSpace(report.BrandingHeader.String)
	if header == "" && tpl != nil {
		header = strings.TrimSpace(tpl.Header)
	}
	header = substituteBrandingTokens(header, tokens)
	if header == "" {
		header = renderLabel(labels, "header_footer.header_text", tokens)
	}

	footer := strings.TrimSpace(report.BrandingFooter.String)
	if footer == "" && tpl != nil {
		footer = strings.TrimSpace(tpl.Footer)
	}
	footer = substituteBrandingTokens(footer, tokens)
	if footer == "" {
		footer = renderLabel(labels, "header_footer.footer_text", tokens)
	}

	subtitle := strings.TrimSpace(report.BrandingSubtitle.String)
	if subtitle == "" && tpl != nil {
		subtitle = strings.TrimSpace(tpl.Subtitle)
	}

	return EffectiveBranding{
		Title:    title,
		Subtitle: substituteBrandingTokens(subtitle, tokens),
		Header:   header,
		Footer:   footer,
	}
}

func substituteBrandingTokens(text string, tokens map[string]string) string {
	for name, value := range tokens {
		text = strings.ReplaceAll(text, "[["+name+"]]", value)
	}
	return text
}

// brandingTextOrNull trims an editor value, returning a NULL for an empty one.
func brandingTextOrNull(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: trimmed, Valid: true}
}

// ValidateBrandingTexts bounds the free-text fields.
func ValidateBrandingTexts(title, subtitle, header, footer *string) error {
	for _, field := range []struct {
		name  string
		value *string
		limit int
	}{
		{"title", title, BrandingMaxTitleLen},
		{"subtitle", subtitle, BrandingMaxSubtitleLen},
		{"header", header, BrandingMaxHeaderLen},
		{"footer", footer, BrandingMaxFooterLen},
	} {
		if field.value != nil && len([]rune(*field.value)) > field.limit {
			return fmt.Errorf("%s is longer than %d characters", field.name, field.limit)
		}
	}
	return nil
}
