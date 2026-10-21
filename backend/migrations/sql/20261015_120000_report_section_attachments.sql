-- +goose Up
-- +goose StatementBegin
-- Operator attachments on report sections (fork extended-version section 6,
-- backlog v2 lot L6). Separate from report_sections.evidence_refs on purpose:
-- evidence is what the writer cited, attachments are what the operator chose to
-- bundle. report_id is denormalized so the archive reads a report's attachments
-- without joining, and both FKs cascade so a report/section delete clears them.

CREATE TABLE report_section_attachments (
  id         BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  report_id  BIGINT NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
  section_id BIGINT NOT NULL REFERENCES report_sections(id) ON DELETE CASCADE,
  label      TEXT NOT NULL DEFAULT '',
  path       TEXT NOT NULL,
  bytes      BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX report_section_attachments_report_idx ON report_section_attachments(report_id);
CREATE INDEX report_section_attachments_section_idx ON report_section_attachments(section_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS report_section_attachments;
-- +goose StatementEnd
