-- name: GetReportTemplate :one
SELECT * FROM report_templates
WHERE id = $1 LIMIT 1;

-- name: GetReportTemplateByKey :one
-- DB-first template resolution (L3): a user's own template wins over a global
-- one (IS NOT DISTINCT FROM matches the NULL user_id of a global template), and
-- the caller prefers enabled templates but falls back to a disabled one so an
-- historical report still renders after its template was switched off.
SELECT * FROM report_templates
WHERE key = $1 AND (user_id = $2 OR user_id IS NULL)
ORDER BY (user_id = $2) DESC, enabled DESC, updated_at DESC
LIMIT 1;

-- name: GetUserReportTemplates :many
SELECT * FROM report_templates
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: GetGlobalReportTemplates :many
SELECT * FROM report_templates
WHERE user_id IS NULL
ORDER BY created_at DESC;

-- name: ListReportTemplates :many
-- Every template the Settings page lists for the caller: the caller's own plus
-- the global ones, newest first.
SELECT * FROM report_templates
WHERE user_id = $1 OR user_id IS NULL
ORDER BY created_at DESC;

-- name: CreateReportTemplate :one
INSERT INTO report_templates (
  user_id,
  key,
  label,
  version,
  yaml,
  enabled,
  branding_title,
  branding_subtitle,
  branding_header,
  branding_footer
) VALUES (
  $1,
  $2,
  $3,
  $4,
  $5,
  $6,
  $7,
  $8,
  $9,
  $10
)
RETURNING *;

-- name: UpdateReportTemplate :one
UPDATE report_templates
SET
  key = $3,
  label = $4,
  version = $5,
  yaml = $6,
  enabled = $7,
  branding_title = $8,
  branding_subtitle = $9,
  branding_header = $10,
  branding_footer = $11,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND (user_id = $2 OR user_id IS NULL)
RETURNING *;

-- name: DeleteReportTemplate :exec
DELETE FROM report_templates
WHERE id = $1 AND (user_id = $2 OR user_id IS NULL);

-- name: SetReportTemplateEnabled :one
UPDATE report_templates
SET
  enabled = $2,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetReportTemplateBrandingText :one
UPDATE report_templates
SET
  branding_title = $2,
  branding_subtitle = $3,
  branding_header = $4,
  branding_footer = $5,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetReportTemplateBrandingLogo :exec
INSERT INTO report_template_branding_logos (template_id, data, name, media_type)
VALUES ($1, $2, $3, $4)
ON CONFLICT (template_id) DO UPDATE SET
  data = EXCLUDED.data,
  name = EXCLUDED.name,
  media_type = EXCLUDED.media_type,
  updated_at = CURRENT_TIMESTAMP;

-- name: ClearReportTemplateBrandingLogo :exec
DELETE FROM report_template_branding_logos
WHERE template_id = $1;

-- name: GetReportTemplateBrandingLogo :one
SELECT data, name, media_type FROM report_template_branding_logos
WHERE template_id = $1;

-- name: GetReportTemplateBrandingLogoInfo :one
SELECT name, media_type FROM report_template_branding_logos
WHERE template_id = $1;
