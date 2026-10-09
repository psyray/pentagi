-- +goose Up
-- +goose StatementBegin

-- When the current run started. The panel's "Took" is finished_at - started_at,
-- the real duration of the run, not the report's whole lifetime (a report can be
-- resumed days after it was created). Plan reports backlog v2, follow-up.
ALTER TABLE reports ADD COLUMN started_at TIMESTAMPTZ NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE reports DROP COLUMN started_at;

-- +goose StatementEnd
