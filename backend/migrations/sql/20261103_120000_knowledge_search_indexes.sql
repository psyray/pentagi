-- +goose NO TRANSACTION
-- +goose Up

-- Search indexes for the knowledge and memory vector store.
--
-- Concurrent builds keep the live table writable while the indexes are built.
-- A failed concurrent build leaves an invalid index behind; check
-- pg_index.indisvalid and drop/recreate it before retrying.
--
-- The production database already carries an HNSW index built by hand. The
-- IF NOT EXISTS clause adopts it instead of creating a duplicate, and the
-- down migration deliberately leaves it in place for that reason.
--
-- pgvector 0.7.x applies a metadata filter outside the HNSW scan, so a filtered
-- query can return fewer neighbours than requested. Moving to pgvector 0.8.x
-- (hnsw.iterative_scan) is tracked separately.

-- The legacy embedding column is an untyped `vector`. pgvector 0.7 requires a
-- dimension to build HNSW, so the working index is on the 1024-dimension cast,
-- the same expression the production database carries. The generic index name
-- was created once and left INVALID; drop it so it cannot mislead a planner.
DROP INDEX IF EXISTS langchain_pg_embedding_hnsw;
CREATE INDEX CONCURRENTLY IF NOT EXISTS langchain_pg_embedding_hnsw_1024
  ON langchain_pg_embedding USING hnsw (((embedding)::vector(1024)) vector_cosine_ops)
  WITH (m = 16, ef_construction = 64);

-- Lexical index: created in the hybrid-search lot, when the query actually
-- uses it. Adding a GIN index over `document` now would tax every memory write
-- for no reader.

-- Ownership is read on every agent search and deduplication lookup.
CREATE INDEX CONCURRENTLY IF NOT EXISTS langchain_pg_embedding_meta_user_id
  ON langchain_pg_embedding ((cmetadata ->> 'user_id'));

-- The agent search filters on doc_type first and user_id second.
CREATE INDEX CONCURRENTLY IF NOT EXISTS langchain_pg_embedding_meta_doc_type_user_id
  ON langchain_pg_embedding ((cmetadata ->> 'doc_type'), (cmetadata ->> 'user_id'));

-- Exact-duplicate detection: new documents carry a normalised content hash.
CREATE INDEX CONCURRENTLY IF NOT EXISTS langchain_pg_embedding_meta_content_hash
  ON langchain_pg_embedding ((cmetadata ->> 'content_hash'));

ANALYZE langchain_pg_embedding;

-- +goose Down

DROP INDEX IF EXISTS langchain_pg_embedding_meta_content_hash;
DROP INDEX IF EXISTS langchain_pg_embedding_meta_doc_type_user_id;
DROP INDEX IF EXISTS langchain_pg_embedding_meta_user_id;

-- langchain_pg_embedding_hnsw_1024 is intentionally not dropped: it may
-- predate this migration (production carries a hand-built copy), and dropping
-- it would silently disable vector search on rollback.
