-- name: GetReport :one
SELECT * FROM reports
WHERE id = $1 AND user_id = $2 LIMIT 1;

-- name: GetReportByID :one
SELECT * FROM reports
WHERE id = $1 LIMIT 1;

-- name: GetFlowReport :one
SELECT * FROM reports
WHERE id = $1 AND flow_id = $2 AND user_id = $3 LIMIT 1;

-- name: GetFlowReports :many
SELECT * FROM reports
WHERE flow_id = $1 AND user_id = $2
ORDER BY created_at DESC;

-- name: GetUserReports :many
SELECT * FROM reports
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: GetActiveReports :many
SELECT * FROM reports
WHERE status IN ('created', 'planning', 'generating')
ORDER BY created_at ASC;

-- name: CreateReport :one
INSERT INTO reports (
  flow_id,
  user_id,
  status,
  title,
  template_key,
  template_label,
  provider_name,
  provider_type,
  language,
  additional_instructions,
  control_budget_seconds,
  content_budget_seconds
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
  $10,
  $11,
  $12
)
RETURNING *;

-- name: UpdateReportStatus :one
UPDATE reports
SET
  status = $2,
  error = NULL,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetReportEstimate :one
UPDATE reports
SET
  sections_estimate = $2,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetReportContentBudget :one
-- Used by resumeReport when the operator grants fresh content seconds.
UPDATE reports
SET
  content_budget_seconds = $2,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: ResetReportForResume :one
-- A resume restarts the run cleanly: the previous finished_at and error must
-- not linger while the worker is generating again.
UPDATE reports
SET
  status = $2,
  finished_at = NULL,
  error = NULL,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetReportStartedAt :one
-- Stamps the current run's start so the panel can show the run duration
-- (finished_at - started_at) instead of the report's whole lifetime.
UPDATE reports
SET
  started_at = CURRENT_TIMESTAMP,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetReportMsgChainIDs :one
-- Persists the report's two msgchains so a resume reopens them instead of
-- duplicating (backlog v2, L8).
UPDATE reports
SET
  msgchain_control_id = $2,
  msgchain_content_id = $3,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SyncReportSectionCounts :one
-- Derives the counters from the section rows so a hand-added (or deleted)
-- section is reflected: total excludes the skipped alternatives, done counts the
-- resolved ones. The planner's own total only holds until the first sync, which
-- keeps a fresh report's "not planned yet" signal (sections_total = 0) intact.
UPDATE reports
SET
  sections_total = (
    SELECT COUNT(*) FROM report_sections
    WHERE report_id = sqlc.arg(report_id) AND status <> 'skipped'
  ),
  sections_done = (
    SELECT COUNT(*) FROM report_sections
    WHERE report_id = sqlc.arg(report_id) AND status = 'done'
  ),
  updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(report_id)
RETURNING *;

-- name: SetReportBrandingText :one
UPDATE reports
SET
  branding_title = $2,
  branding_subtitle = $3,
  branding_header = $4,
  branding_footer = $5,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetReportBrandingLogo :exec
INSERT INTO report_branding_logos (report_id, data, name, media_type)
VALUES ($1, $2, $3, $4)
ON CONFLICT (report_id) DO UPDATE SET
  data = EXCLUDED.data,
  name = EXCLUDED.name,
  media_type = EXCLUDED.media_type,
  updated_at = CURRENT_TIMESTAMP;

-- name: ClearReportBrandingLogo :exec
DELETE FROM report_branding_logos
WHERE report_id = $1;

-- name: GetReportBrandingLogo :one
SELECT data, name, media_type FROM report_branding_logos
WHERE report_id = $1;

-- name: GetReportBrandingLogoInfo :one
SELECT name, media_type FROM report_branding_logos
WHERE report_id = $1;

-- name: SetReportLabels :one
UPDATE reports
SET
  labels = $2,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetReportPlan :one
UPDATE reports
SET
  status = $2,
  title = $3,
  sections_estimate = $4,
  sections_total = $5,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: UpdateReportProgress :one
UPDATE reports
SET
  sections_done = $2,
  tokens_in = $3,
  tokens_out = $4,
  control_used_seconds = $5,
  content_used_seconds = $6,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: FinishReport :one
UPDATE reports
SET
  status = $2,
  error = $3,
  finished_at = CURRENT_TIMESTAMP,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: DeleteReport :exec
DELETE FROM reports
WHERE id = $1 AND user_id = $2;

-- name: GetReportSections :many
SELECT * FROM report_sections
WHERE report_id = $1
ORDER BY position ASC, id ASC;

-- name: GetReportSection :one
SELECT * FROM report_sections
WHERE id = $1 AND report_id = $2 LIMIT 1;

-- name: GetMaxReportSectionPosition :one
SELECT COALESCE(MAX(position), 0) AS position FROM report_sections
WHERE report_id = $1;

-- name: ShiftReportSectionPositions :exec
-- IS NOT DISTINCT FROM so a NULL parent (top-level sections) matches the NULL
-- argument: `parent_id = NULL` never matched and silently skipped the shift,
-- leaving the new section on top of an existing position.
UPDATE report_sections
SET position = position + 1
WHERE report_id = $1 AND parent_id IS NOT DISTINCT FROM $2 AND position >= $3;

-- name: CompactReportSectionPositions :exec
-- Pulls the siblings after a removed section back by one so positions stay
-- contiguous across add/delete cycles (IS NOT DISTINCT FROM for the NULL parent).
UPDATE report_sections
SET position = position - 1
WHERE report_id = $1 AND parent_id IS NOT DISTINCT FROM $2 AND position > $3;

-- name: CreateReportSection :one
INSERT INTO report_sections (
  report_id,
  parent_id,
  position,
  key,
  kind,
  title,
  instructions
) VALUES (
  $1,
  $2,
  $3,
  $4,
  $5,
  $6,
  $7
)
RETURNING *;

-- name: SetReportSectionStatus :one
UPDATE report_sections
SET
  status = $2,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: UpdateReportSectionContent :one
UPDATE report_sections
SET
  content = $2,
  status = $3,
  tokens_in = $4,
  tokens_out = $5,
  duration_seconds = $6,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: UpdateReportSectionFields :one
UPDATE report_sections
SET
  title = $2,
  instructions = $3,
  variables = $4,
  warnings = $5,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetReportSectionTitle :one
UPDATE report_sections
SET
  title = $2,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetReportSectionPosition :one
UPDATE report_sections
SET
  position = $2,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetReportSectionHidden :one
UPDATE report_sections
SET
  hidden = $2,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetReportSectionRegenRequested :one
UPDATE report_sections
SET
  regen_requested = $2,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: DeleteReportSection :exec
DELETE FROM report_sections
WHERE id = $1 AND report_id = $2;

-- name: SetReportSectionSources :one
UPDATE report_sections
SET
  source_subtask_ids = $2,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SetReportSectionEvidence :one
UPDATE report_sections
SET
  evidence_refs = $2,
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: AppendReportSectionWarning :one
UPDATE report_sections
SET
  warnings = array_append(COALESCE(warnings, '{}'), $2),
  updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: GetReportLogs :many
SELECT * FROM report_logs
WHERE report_id = $1
ORDER BY id ASC
LIMIT $2;

-- name: CreateReportLog :one
INSERT INTO report_logs (
  report_id,
  section_id,
  kind,
  message,
  payload
) VALUES (
  $1,
  $2,
  $3,
  $4,
  $5
)
RETURNING *;

-- name: GetReportSectionAttachments :many
-- Every operator attachment of one report, in creation order; the controller
-- groups them by section for the reader and the archive (backlog v2, L6).
SELECT * FROM report_section_attachments
WHERE report_id = $1
ORDER BY id ASC;

-- name: GetReportSectionAttachmentsBySection :many
SELECT * FROM report_section_attachments
WHERE report_id = $1 AND section_id = $2
ORDER BY id ASC;

-- name: DeleteReportSectionAttachments :exec
-- The set mutation replaces a section's attachments wholesale.
DELETE FROM report_section_attachments
WHERE report_id = $1 AND section_id = $2;

-- name: CreateReportSectionAttachment :one
INSERT INTO report_section_attachments (
  report_id,
  section_id,
  label,
  path,
  bytes
) VALUES (
  $1,
  $2,
  $3,
  $4,
  $5
)
RETURNING *;
