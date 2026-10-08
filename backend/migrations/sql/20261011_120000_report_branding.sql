-- +goose Up
-- +goose StatementBegin
-- Per-report export branding (fork extended-version section 6, plan § 6.6 / P6).
-- The cover page and the page header/footer of the exports follow the text
-- overrides when they are set; the i18n labels stay the fallback.
--
-- The logo lives in its own table: `reports` is read with `SELECT *` by every
-- ownership check, so a blob column would be pulled on every report query.

ALTER TABLE reports ADD COLUMN branding_title TEXT NULL;
ALTER TABLE reports ADD COLUMN branding_subtitle TEXT NULL;
ALTER TABLE reports ADD COLUMN branding_header TEXT NULL;
ALTER TABLE reports ADD COLUMN branding_footer TEXT NULL;

CREATE TABLE report_branding_logos (
  report_id BIGINT PRIMARY KEY REFERENCES reports(id) ON DELETE CASCADE,
  data BYTEA NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  media_type TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS report_branding_logos;
ALTER TABLE reports DROP COLUMN IF EXISTS branding_footer;
ALTER TABLE reports DROP COLUMN IF EXISTS branding_header;
ALTER TABLE reports DROP COLUMN IF EXISTS branding_subtitle;
ALTER TABLE reports DROP COLUMN IF EXISTS branding_title;
-- +goose StatementEnd
