-- +goose Up
-- +goose StatementBegin

-- Retrieval events make the usage dashboard possible. A search writes one row
-- with its hits in details, a read writes one row with the document id, so the
-- table is scanned by event and by window.
CREATE INDEX IF NOT EXISTS knowledge_events_event_created_idx
  ON knowledge_events (event, created_at DESC);

CREATE INDEX IF NOT EXISTS knowledge_events_actor_created_idx
  ON knowledge_events (actor_user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS knowledge_events_document_idx
  ON knowledge_events (document_id)
  WHERE document_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS knowledge_events_document_idx;
DROP INDEX IF EXISTS knowledge_events_actor_created_idx;
DROP INDEX IF EXISTS knowledge_events_event_created_idx;

-- +goose StatementEnd
