-- +goose Up
-- +goose StatementBegin

INSERT INTO privileges (role_id, name) VALUES
  (1, 'skills.admin'),
  (1, 'skills.view'),
  (1, 'skills.create'),
  (1, 'skills.edit'),
  (1, 'skills.delete'),
  (1, 'skills.search'),
  (2, 'skills.view'),
  (2, 'skills.create'),
  (2, 'skills.edit'),
  (2, 'skills.delete'),
  (2, 'skills.search')
  ON CONFLICT DO NOTHING;

-- A reusable skill or reference document. Chunks live in knowledge_chunks so
-- this table stays small and the legacy langchain_pg_embedding table keeps its
-- current meaning for flow memory and agent-generated guides.
CREATE TABLE IF NOT EXISTS knowledge_documents (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id       bigint NOT NULL,
  pack_id       uuid,
  kind          text NOT NULL,
  slug          text NOT NULL,
  title         text NOT NULL,
  description   text NOT NULL DEFAULT '',
  content       text NOT NULL,
  source_kind   text NOT NULL DEFAULT 'manual',
  source_uri    text,
  content_hash  text NOT NULL,
  language      text,
  version       int NOT NULL DEFAULT 1,
  status        text NOT NULL DEFAULT 'draft',
  visibility    text NOT NULL DEFAULT 'private',
  quality       int NOT NULL DEFAULT 0,
  tags          text[] NOT NULL DEFAULT '{}',
  metadata      jsonb NOT NULL DEFAULT '{}',
  created_at    timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at    timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
  deleted_at    timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS knowledge_documents_owner_kind_slug
  ON knowledge_documents (user_id, kind, slug);
CREATE INDEX IF NOT EXISTS knowledge_documents_user_updated
  ON knowledge_documents (user_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS knowledge_documents_kind_status_visibility
  ON knowledge_documents (kind, status, visibility);
CREATE INDEX IF NOT EXISTS knowledge_documents_tags
  ON knowledge_documents USING gin (tags);
CREATE INDEX IF NOT EXISTS knowledge_documents_content_hash
  ON knowledge_documents (content_hash);
CREATE INDEX IF NOT EXISTS knowledge_documents_metadata
  ON knowledge_documents USING gin (metadata);

DROP TRIGGER IF EXISTS knowledge_documents_updated_at ON knowledge_documents;
CREATE TRIGGER knowledge_documents_updated_at
  BEFORE UPDATE ON knowledge_documents
  FOR EACH ROW EXECUTE PROCEDURE update_modified_column();

-- One row per chunk. The embedding dimension is fixed at 1024 (bge-m3); a
-- different embedding model needs a migration and a re-embedding pass.
CREATE TABLE IF NOT EXISTS knowledge_chunks (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  document_id   uuid NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE,
  chunk_index   int NOT NULL,
  heading_path  text NOT NULL DEFAULT '',
  content       text NOT NULL,
  content_hash  text NOT NULL,
  embedding     vector(1024) NOT NULL,
  search_vector tsvector GENERATED ALWAYS AS (to_tsvector('simple', COALESCE(content, ''))) STORED,
  metadata      jsonb NOT NULL DEFAULT '{}',
  created_at    timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (document_id, chunk_index)
);

CREATE INDEX IF NOT EXISTS knowledge_chunks_document_id
  ON knowledge_chunks (document_id);
CREATE INDEX IF NOT EXISTS knowledge_chunks_embedding_hnsw
  ON knowledge_chunks USING hnsw (embedding vector_cosine_ops)
  WITH (m = 16, ef_construction = 64);
CREATE INDEX IF NOT EXISTS knowledge_chunks_search_vector
  ON knowledge_chunks USING gin (search_vector);

CREATE TABLE IF NOT EXISTS knowledge_files (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  document_id uuid NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE,
  path        text NOT NULL,
  mime        text,
  size        bigint NOT NULL DEFAULT 0,
  sha256      text,
  blob_ref    text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (document_id, path)
);

CREATE TABLE IF NOT EXISTS knowledge_packs (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     bigint NOT NULL,
  name        text NOT NULL,
  description text NOT NULL DEFAULT '',
  visibility  text NOT NULL DEFAULT 'private',
  version     text,
  license     text,
  created_at  timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (user_id, name)
);

DROP TRIGGER IF EXISTS knowledge_packs_updated_at ON knowledge_packs;
CREATE TRIGGER knowledge_packs_updated_at
  BEFORE UPDATE ON knowledge_packs
  FOR EACH ROW EXECUTE PROCEDURE update_modified_column();

CREATE TABLE IF NOT EXISTS knowledge_pack_items (
  pack_id     uuid NOT NULL REFERENCES knowledge_packs(id) ON DELETE CASCADE,
  document_id uuid NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE,
  PRIMARY KEY (pack_id, document_id)
);

CREATE TABLE IF NOT EXISTS knowledge_events (
  id            bigint PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  document_id   uuid,
  flow_id       bigint,
  task_id       bigint,
  subtask_id    bigint,
  actor_user_id bigint,
  event         text NOT NULL,
  details       jsonb NOT NULL DEFAULT '{}',
  created_at    timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS knowledge_events_document
  ON knowledge_events (document_id, created_at DESC);
CREATE INDEX IF NOT EXISTS knowledge_events_flow
  ON knowledge_events (flow_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS knowledge_events;
DROP TABLE IF EXISTS knowledge_pack_items;
DROP TABLE IF EXISTS knowledge_packs;
DROP TABLE IF EXISTS knowledge_files;
DROP TABLE IF EXISTS knowledge_chunks;
DROP TABLE IF EXISTS knowledge_documents;

DELETE FROM privileges WHERE name LIKE 'skills.%';

-- +goose StatementEnd
