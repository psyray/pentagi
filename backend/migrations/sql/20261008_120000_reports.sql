-- +goose Up
-- +goose StatementBegin
-- Reports (fork extended-version section 6): AI-generated documents for a flow.
-- A report run instantiates the section tree of its template (planner node),
-- then content nodes write the sections (streamed, soft agent-time budgets).
-- report_logs feed the generation console (control messages, section events,
-- operator notes). All additive: no change to existing tables or writes.
-- Status/kind columns are TEXT + Go allowlist (fork convention, same as the
-- subtasks planning metadata migration): unexpected values are kept verbatim.

CREATE TABLE reports (
  id   BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  flow_id   BIGINT NOT NULL REFERENCES flows(id) ON DELETE CASCADE,
  user_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status    TEXT   NOT NULL DEFAULT 'created',
  title     TEXT   NOT NULL DEFAULT '',
  template_key   TEXT NOT NULL DEFAULT 'pentagi-default-pentest-report',
  template_label TEXT NOT NULL DEFAULT '',
  provider_name  TEXT NOT NULL DEFAULT '',
  provider_type  PROVIDER_TYPE NOT NULL DEFAULT 'custom',
  language       TEXT NOT NULL DEFAULT 'English',
  additional_instructions TEXT NULL,
  control_budget_seconds INTEGER NOT NULL DEFAULT 0,
  content_budget_seconds INTEGER NOT NULL DEFAULT 0,
  control_used_seconds   DOUBLE PRECISION NOT NULL DEFAULT 0,
  content_used_seconds   DOUBLE PRECISION NOT NULL DEFAULT 0,
  sections_estimate INTEGER NOT NULL DEFAULT 0,
  sections_total    INTEGER NOT NULL DEFAULT 0,
  sections_done     INTEGER NOT NULL DEFAULT 0,
  tokens_in  BIGINT NOT NULL DEFAULT 0,
  tokens_out BIGINT NOT NULL DEFAULT 0,
  error      TEXT NULL,
  created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  updated_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  finished_at TIMESTAMPTZ NULL
);

CREATE INDEX reports_flow_idx ON reports(flow_id);
CREATE INDEX reports_user_idx ON reports(user_id);
CREATE INDEX reports_status_idx ON reports(status);

CREATE TABLE report_sections (
  id   BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  report_id   BIGINT NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
  parent_id   BIGINT NULL REFERENCES report_sections(id) ON DELETE CASCADE,
  position    INTEGER NOT NULL DEFAULT 0,
  key         TEXT NOT NULL DEFAULT '',
  kind        TEXT NOT NULL DEFAULT 'llm',
  title       TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'queued',
  content     TEXT NOT NULL DEFAULT '',
  instructions TEXT NULL,
  variables   JSON NOT NULL DEFAULT '[]',
  warnings    TEXT[] NULL,
  hidden      BOOLEAN NOT NULL DEFAULT false,
  tokens_in   BIGINT NOT NULL DEFAULT 0,
  tokens_out  BIGINT NOT NULL DEFAULT 0,
  duration_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  updated_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX report_sections_report_idx ON report_sections(report_id);
CREATE INDEX report_sections_parent_idx ON report_sections(parent_id);

CREATE TABLE report_logs (
  id   BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  report_id  BIGINT NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
  section_id BIGINT NULL REFERENCES report_sections(id) ON DELETE SET NULL,
  kind    TEXT NOT NULL,
  message TEXT NOT NULL,
  payload JSON NULL,
  created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX report_logs_report_idx ON report_logs(report_id);
CREATE INDEX report_logs_section_idx ON report_logs(section_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS report_logs;
DROP TABLE IF EXISTS report_sections;
DROP TABLE IF EXISTS reports;
-- +goose StatementEnd