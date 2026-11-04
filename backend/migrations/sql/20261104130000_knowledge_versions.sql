-- +goose Up
-- +goose StatementBegin

-- One row per content revision of a skill. The parent table keeps the current
-- state and the version counter; this table keeps what each version said, so a
-- reviewer can read a diff instead of trusting a version number.
CREATE TABLE IF NOT EXISTS knowledge_document_versions (
  document_id   uuid NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE,
  version       int NOT NULL,
  title         text NOT NULL,
  description   text NOT NULL DEFAULT '',
  content       text NOT NULL,
  content_hash  text NOT NULL,
  status        text NOT NULL DEFAULT 'draft',
  visibility    text NOT NULL DEFAULT 'private',
  tags          text[] NOT NULL DEFAULT '{}',
  metadata      jsonb NOT NULL DEFAULT '{}',
  actor_user_id bigint,
  created_at    timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (document_id, version)
);

CREATE INDEX IF NOT EXISTS knowledge_document_versions_document
  ON knowledge_document_versions (document_id, version DESC);

-- +goose StatementEnd

-- +goose StatementBegin

-- The history starts with the current state of every document: earlier content
-- is not recoverable, but from now on each import appends a row.
INSERT INTO knowledge_document_versions
  (document_id, version, title, description, content, content_hash, status, visibility, tags, metadata, created_at)
SELECT d.id, d.version, d.title, d.description, d.content, d.content_hash,
       d.status, d.visibility, d.tags, d.metadata, d.updated_at
FROM knowledge_documents d
WHERE d.deleted_at IS NULL
ON CONFLICT DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS knowledge_document_versions;
-- +goose StatementEnd
