-- +goose Up
-- +goose StatementBegin
-- Attribute every message to the agent that produced it, so the chat can be
-- grouped by subtask and then by agent (nullable: non-agent messages stay flat).
ALTER TABLE msglogs ADD COLUMN agent_type MSGCHAIN_TYPE NULL;

CREATE INDEX msglogs_agent_type_idx ON msglogs(agent_type);
CREATE INDEX msglogs_flow_subtask_agent_idx ON msglogs(flow_id, subtask_id, agent_type);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS msglogs_flow_subtask_agent_idx;
DROP INDEX IF EXISTS msglogs_agent_type_idx;
ALTER TABLE msglogs DROP COLUMN agent_type;
-- +goose StatementEnd
