-- +goose Up
-- +goose StatementBegin
-- Report msgchains (fork extended-version section 6, backlog v2 lot L8): the
-- report engine emits one control chain (planner + vision probe) and one content
-- chain (writers) so the existing usage/cost views see report consumption like
-- any other agent. reports keeps the chain ids so a resume reopens the same rows
-- instead of duplicating them (plan Q2).
--
-- MSGCHAIN_TYPE gains report_control/report_content through ADD VALUE: unlike a
-- full type recreate it touches no other table, so the migration is valid whether
-- or not the optional msglogs.agent_type column exists (it is added by another
-- branch). PostgreSQL 12+ allows ADD VALUE inside the migration transaction as
-- long as the new value is not used before the commit, which is not the case here.

ALTER TABLE reports ADD COLUMN msgchain_control_id BIGINT NULL;
ALTER TABLE reports ADD COLUMN msgchain_content_id BIGINT NULL;

ALTER TYPE MSGCHAIN_TYPE ADD VALUE IF NOT EXISTS 'report_control';
ALTER TYPE MSGCHAIN_TYPE ADD VALUE IF NOT EXISTS 'report_content';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- PostgreSQL cannot remove an enum value; the two are simply left in place, and
-- the report columns that carry their ids are dropped.
ALTER TABLE reports DROP COLUMN IF EXISTS msgchain_content_id;
ALTER TABLE reports DROP COLUMN IF EXISTS msgchain_control_id;
-- +goose StatementEnd
