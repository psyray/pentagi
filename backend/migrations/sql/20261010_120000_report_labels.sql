-- +goose Up
-- +goose StatementBegin
-- Per-report labels the composed sections render (fork extended-version section 6,
-- plan § 6.11). The planner translates a small set of label keys into the report
-- language; the export composers read them with an English fallback, so a report
-- with no labels keeps today's wording. Additive: existing reports keep NULL.

ALTER TABLE reports ADD COLUMN labels JSONB NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE reports DROP COLUMN IF EXISTS labels;
-- +goose StatementEnd
