-- +goose Up
-- +goose StatementBegin

-- User-defined report templates (backlog v2, L3). A template is the YAML
-- outline a report run plans and writes, stored verbatim so the source of
-- truth stays the YAML the operator edited. user_id NULL marks a global
-- template (admin-managed); otherwise the template belongs to that user.
-- UNIQUE(user_id, key) lets a user shadow a bundled or global key: the key
-- is the stable identity reports snapshot (reports.template_key), so a deleted
-- or edited template degrades to the bundled one without breaking history.
--
-- Branding lives on the template itself (the four texts) and in a side table
-- for the logo, mirroring reports/report_branding_logos: the texts default to
-- NULL (= inherit), the logo is a blob so report_templates is not read with
-- SELECT * on hot paths.
CREATE TABLE report_templates (
  id               BIGINT       PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  user_id          BIGINT       REFERENCES users(id) ON DELETE CASCADE,
  key              TEXT         NOT NULL,
  label            TEXT         NOT NULL DEFAULT '',
  version          INT          NOT NULL DEFAULT 1,
  yaml             TEXT         NOT NULL,
  enabled          BOOLEAN      NOT NULL DEFAULT TRUE,
  branding_title    TEXT,
  branding_subtitle TEXT,
  branding_header   TEXT,
  branding_footer   TEXT,
  created_at       TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at       TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,

  CONSTRAINT report_templates_key_not_empty CHECK (length(trim(key)) > 0),
  CONSTRAINT report_templates_yaml_not_empty CHECK (length(trim(yaml)) > 0),
  CONSTRAINT report_templates_user_key_unique UNIQUE (user_id, key)
);

CREATE INDEX report_templates_user_id_idx ON report_templates(user_id);
CREATE INDEX report_templates_created_at_idx ON report_templates(created_at DESC);

CREATE TABLE report_template_branding_logos (
  template_id BIGINT PRIMARY KEY REFERENCES report_templates(id) ON DELETE CASCADE,
  data        BYTEA NOT NULL,
  name        TEXT NOT NULL DEFAULT '',
  media_type  TEXT NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE OR REPLACE TRIGGER update_report_templates_modified
  BEFORE UPDATE ON report_templates
  FOR EACH ROW EXECUTE PROCEDURE update_modified_column();

-- Template management rides on the reports.* privileges: a global template
-- (user_id NULL) needs reports.admin, a personal one needs reports.edit.
-- Role 1 is Admin, role 2 is User; both already hold reports.edit/admin.
-- No new privilege rows: the existing set is reused on purpose.

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS report_template_branding_logos;
DROP TABLE IF EXISTS report_templates;

-- +goose StatementEnd
