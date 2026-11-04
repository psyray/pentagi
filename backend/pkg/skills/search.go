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
// document, with the best matching chunk as the excerpt. RRF fuses the two
// branches so identifiers (CVEs, tool names, ESC8, ...) are not lost when the
// embedding ranks them poorly.
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
  SELECT $1::vector AS emb, websearch_to_tsquery('simple', $2) AS tsq
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
),
vec AS (
  SELECT c.id, c.document_id, c.chunk_index, c.heading_path, c.content,
         1 - (c.embedding <=> p.emb) AS score
  FROM knowledge_chunks c
  JOIN filtered f ON f.id = c.document_id
  CROSS JOIN params p
  ORDER BY c.embedding <=> p.emb
  LIMIT $7
),
lex AS (
  SELECT c.id, c.document_id, c.chunk_index, c.heading_path, c.content,
         ts_rank_cd(c.search_vector, p.tsq) AS score
  FROM knowledge_chunks c
  JOIN filtered f ON f.id = c.document_id
  CROSS JOIN params p
  WHERE c.search_vector @@ p.tsq
  ORDER BY score DESC
  LIMIT $7
),
fused AS (
  SELECT id, document_id, chunk_index, heading_path, content,
         SUM(1.0 / ($9::float8 + rank)) AS score
  FROM (
    SELECT id, document_id, chunk_index, heading_path, content,
           row_number() OVER (ORDER BY score DESC) AS rank FROM vec
    UNION ALL
    SELECT id, document_id, chunk_index, heading_path, content,
           row_number() OVER (ORDER BY score DESC) AS rank FROM lex
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
LIMIT $8`,
		formatVector(vector), query, userID,
		nullableStrings(filter.Kinds), nullableStrings(defaultStatuses(filter.Statuses)), nullableStrings(filter.Tags),
		candidates, limit, rrfRankConstant,
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
