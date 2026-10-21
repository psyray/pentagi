-- +goose Up
-- +goose StatementBegin
-- Evidence backing for report sections (fork extended-version section 6, plan
-- § 6.10). source_subtask_ids records which subtasks a finding was derived from
-- (the planner's mapping), so the curator can assemble that subtask's evidence
-- pack. evidence_refs records the evidence the writer actually cited (the
-- manifest the archive bundles): [{kind, path|screenshotId, label}].
-- Additive: existing sections keep NULL sources and an empty manifest.

ALTER TABLE report_sections ADD COLUMN source_subtask_ids BIGINT[] NULL;
ALTER TABLE report_sections ADD COLUMN evidence_refs JSON NOT NULL DEFAULT '[]';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE report_sections DROP COLUMN IF EXISTS evidence_refs;
ALTER TABLE report_sections DROP COLUMN IF EXISTS source_subtask_ids;
-- +goose StatementEnd
