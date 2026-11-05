package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	rrfRankConstant = 60.0
	minCandidates   = 40
	maxCandidates   = 200
	excerptMaxRunes = 700
)

// Search runs a hybrid vector plus lexical query and returns one card per
// document, with the best matching chunk as the excerpt. RRF fuses three
// branches: the vector search, a strict lexical search (every term), and a
// rare-term lexical search. The last one keeps identifiers such as ESC8, a CVE
// id or a tool name visible even when the embedding dilutes them in a long
// document, while the strict branch keeps precision on short queries.
func (s *Store) Search(ctx context.Context, userID int64, query string, filter Filter, limit int) ([]SearchHit, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: query is required", ErrInvalidInput)
	}
	vector, err := s.queryEmbedding(ctx, query)
	if err != nil {
		return nil, err
	}

	limit = clampLimit(limit, defaultSearchLimit, maxSearchLimit)
	candidates := limit * 8
	if candidates < minCandidates {
		candidates = minCandidates
	}
	if candidates > maxCandidates {
		candidates = maxCandidates
	}

	rows, err := s.db.Query(ctx, `
WITH params AS (
  SELECT $1::vector AS emb, to_tsvector('simple', $2) AS ts,
         websearch_to_tsquery('simple', $2) AS tsq
),
filtered AS (
  SELECT d.id, d.user_id, d.pack_id, d.kind, d.slug, d.title, d.description,
         d.source_kind, d.source_uri, d.content_hash, d.language, d.version,
         d.status, d.visibility, d.quality, d.tags, d.metadata, d.created_at, d.updated_at
  FROM knowledge_documents d
  WHERE d.deleted_at IS NULL
    AND (d.user_id = $3 OR (d.visibility = 'shared' AND d.status IN ('reviewed','canonical')))
    AND ($4::text[] IS NULL OR d.kind = ANY($4))
    AND ($5::text[] IS NULL OR d.status = ANY($5))
    AND ($6::text[] IS NULL OR d.tags && $6)
    AND ($7::text[] IS NULL OR d.pack_id::text = ANY($7))
),
total AS (
  SELECT GREATEST(count(*), 1) AS n FROM filtered
),
terms AS (
  SELECT u.lexeme,
         (SELECT count(DISTINCT c2.document_id)
          FROM knowledge_chunks c2
          JOIN filtered f2 ON f2.id = c2.document_id
          WHERE c2.search_vector @@ to_tsquery('simple', quote_literal(u.lexeme))) AS df
  FROM params p, unnest(p.ts) u(lexeme)
  LIMIT 32
),
rare AS (
  SELECT CASE WHEN count(*) = 0 THEN NULL
              ELSE to_tsquery('simple', string_agg(quote_literal(lexeme), ' | '))
         END AS tsq
  FROM terms, total t
  WHERE df <= GREATEST(3, t.n / 50)
),
vec AS (
  SELECT c.id, c.document_id, c.chunk_index, c.heading_path, c.content,
         1 - (c.embedding <=> p.emb) AS score
  FROM knowledge_chunks c
  JOIN filtered f ON f.id = c.document_id
  CROSS JOIN params p
  ORDER BY c.embedding <=> p.emb
  LIMIT $8
),
lex AS (
  SELECT c.id, c.document_id, c.chunk_index, c.heading_path, c.content,
         ts_rank_cd(c.search_vector, p.tsq) AS score
  FROM knowledge_chunks c
  JOIN filtered f ON f.id = c.document_id
  CROSS JOIN params p
  WHERE p.tsq IS NOT NULL AND c.search_vector @@ p.tsq
  ORDER BY score DESC
  LIMIT $8
),
rare_lex AS (
  SELECT c.id, c.document_id, c.chunk_index, c.heading_path, c.content,
         ts_rank_cd(c.search_vector, r.tsq) AS score
  FROM knowledge_chunks c
  JOIN filtered f ON f.id = c.document_id
  CROSS JOIN rare r
  WHERE r.tsq IS NOT NULL AND c.search_vector @@ r.tsq
  ORDER BY score DESC
  LIMIT $8
),
fused AS (
  SELECT id, document_id, chunk_index, heading_path, content,
         SUM(1.0 / ($10::float8 + rank)) AS score
  FROM (
    SELECT id, document_id, chunk_index, heading_path, content,
           row_number() OVER (ORDER BY score DESC) AS rank FROM vec
    UNION ALL
    SELECT id, document_id, chunk_index, heading_path, content,
           row_number() OVER (ORDER BY score DESC) AS rank FROM lex
    UNION ALL
    SELECT id, document_id, chunk_index, heading_path, content,
           row_number() OVER (ORDER BY score DESC) AS rank FROM rare_lex
  ) ranked
  GROUP BY id, document_id, chunk_index, heading_path, content
)
SELECT f.id::text, f.document_id::text, f.chunk_index, f.heading_path, f.content, f.score,
       d.user_id, COALESCE(d.pack_id::text, ''), d.kind, d.slug, d.title, d.description,
       d.source_kind, COALESCE(d.source_uri, ''), d.content_hash, COALESCE(d.language, ''),
       d.version, d.status, d.visibility, d.quality, d.tags, d.metadata, d.created_at, d.updated_at
FROM fused f
JOIN filtered d ON d.id = f.document_id
ORDER BY f.score DESC
LIMIT $9`,
		formatVector(vector), query, userID,
		nullableStrings(filter.Kinds), nullableStrings(defaultStatuses(filter.Statuses)), nullableStrings(filter.Tags),
		nullableStrings(filter.PackIDs), candidates, limit, rrfRankConstant,
	)
	if err != nil {
		return nil, fmt.Errorf("skills: search documents: %w", err)
	}
	defer rows.Close()

	hits := make([]SearchHit, 0, limit)
	seen := map[string]struct{}{}

	for rows.Next() {
		var (
			hit       SearchHit
			chunkID   string
			chunkText string
			packID    string
			sourceURI string
			tags      []string
			meta      []byte
		)

		err := rows.Scan(
			&chunkID, &hit.Document.ID, &hit.ChunkIndex, &hit.HeadingPath, &chunkText, &hit.Score,
			&hit.Document.UserID, &packID, &hit.Document.Kind, &hit.Document.Slug,
			&hit.Document.Title, &hit.Document.Description, &hit.Document.SourceKind, &sourceURI,
			&hit.Document.ContentHash, &hit.Document.Language, &hit.Document.Version,
			&hit.Document.Status, &hit.Document.Visibility, &hit.Document.Quality,
			&tags, &meta, &hit.Document.CreatedAt, &hit.Document.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("skills: scan search hit: %w", err)
		}

		if packID != "" {
			hit.Document.PackID = &packID
		}
		if sourceURI != "" {
			hit.Document.SourceURI = &sourceURI
		}
		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &hit.Document.Metadata)
		}
		if hit.Document.Metadata == nil {
			hit.Document.Metadata = map[string]any{}
		}
		hit.Document.Tags = tags
		hit.Excerpt = excerpt(chunkText, excerptMaxRunes)

		if _, ok := seen[hit.Document.ID]; ok {
			continue
		}
		seen[hit.Document.ID] = struct{}{}
		hits = append(hits, hit)
		if len(hits) >= limit {
			break
		}
	}

	return hits, rows.Err()
}

func excerpt(content string, maxRunes int) string {
	content = strings.Join(strings.Fields(content), " ")
	return truncateRunes(content, maxRunes)
}
