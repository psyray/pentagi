-- +goose Up
-- +goose StatementBegin

-- A section the operator edited while the report was still generating (or that
-- was explicitly asked to be rewritten): the live worker's drain loop rewrites
-- it and clears the flag. Plan reports backlog v2, L1.
ALTER TABLE report_sections ADD COLUMN regen_requested BOOLEAN NOT NULL DEFAULT false;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE report_sections DROP COLUMN regen_requested;

-- +goose StatementEnd
