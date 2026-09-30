-- +goose Up
-- +goose StatementBegin
-- Drop btree indexes on free-form TEXT columns that can exceed the btree row
-- limit. PostgreSQL cannot index values larger than 1/3 of an 8KB page
-- (~2704 bytes), so every CreateAgentLog / CreateVectorStoreLog with a long
-- task/query failed with:
--   ERROR: index row size N exceeds btree version 4 maximum 2704
--   for index "agentlogs_task_idx" / "vecstorelogs_query_idx"
-- No query filters on agentlogs.task or vecstorelogs.query (access is by
-- flow_id / task_id / subtask_id), so these indexes could never be used by
-- the planner. Same fix as subtasks_description_idx (see 20251102_194813).
DROP INDEX IF EXISTS agentlogs_task_idx;
DROP INDEX IF EXISTS vecstorelogs_query_idx;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Recreate the original btree indexes. NOTE: while these exist, INSERTs with
-- a task/query longer than ~2704 bytes fail again (btree row limit).
CREATE INDEX agentlogs_task_idx ON agentlogs(task);
CREATE INDEX vecstorelogs_query_idx ON vecstorelogs(query);
-- +goose StatementEnd