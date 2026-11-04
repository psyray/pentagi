package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// DefaultDuplicateThreshold is the similarity from which two skills are
	// reported as near duplicates. Between 0.93 and 0.98 the plan asks for a
	// human review rather than an automatic merge.
	DefaultDuplicateThreshold = 0.93

	mergeMetadataKey = "merged_from"
	mergedIntoKey    = "merged_into"
)

// DocumentRef is a compact document identity used by the duplicate report.
type DocumentRef struct {
	ID      string
	Slug    string
	Title   string
	Kind    string
	Status  string
	Chars   int
	Version int
}

// DuplicatePair is two documents whose bodies are similar.
type DuplicatePair struct {
	Left       DocumentRef
	Right      DocumentRef
	Similarity float64
}

// DuplicateGroup is a set of documents reachable through duplicate pairs.
type DuplicateGroup struct {
	Kind          string
	MaxSimilarity float64
	Documents     []DocumentRef
}

// DuplicateReport lists the near duplicates of a scope, plus the exact
// duplicates found by content hash.
type DuplicateReport struct {
	Threshold   float64
	GeneratedAt time.Time
	Pairs       []DuplicatePair
	Groups      []DuplicateGroup
	Exact       [][]DocumentRef
}

// MergeResult reports what a merge changed.
type MergeResult struct {
	Target  Document
	Sources []string
	Skipped []string
	Files   int
	Version int
}

// DuplicateReport pairs the documents of a scope by body similarity, using the
// first chunk of each document as its representative vector.
func (s *Store) DuplicateReport(ctx context.Context, userID int64, threshold float64) (*DuplicateReport, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	if threshold <= 0 {
		threshold = DefaultDuplicateThreshold
	}
	if threshold > 1 {
		threshold = 1
	}

	rows, err := s.db.Query(ctx, `
WITH first_chunk AS (
  SELECT DISTINCT ON (document_id) document_id, embedding
  FROM knowledge_chunks
  ORDER BY document_id, chunk_index
)
SELECT a.document_id::text, b.document_id::text, 1 - (a.embedding <=> b.embedding) AS similarity
FROM first_chunk a
JOIN first_chunk b ON a.document_id < b.document_id
JOIN knowledge_documents da ON da.id = a.document_id
JOIN knowledge_documents db ON db.id = b.document_id
WHERE da.deleted_at IS NULL AND db.deleted_at IS NULL
  AND da.kind = db.kind
  AND da.status <> 'deprecated' AND db.status <> 'deprecated'
  AND COALESCE(da.metadata->>'merged_into', '') = ''
  AND COALESCE(db.metadata->>'merged_into', '') = ''
  AND (da.user_id = $1 OR (da.visibility = 'shared' AND da.status IN ('reviewed','canonical')))
  AND (db.user_id = $1 OR (db.visibility = 'shared' AND db.status IN ('reviewed','canonical')))
  AND 1 - (a.embedding <=> b.embedding) >= $2
ORDER BY similarity DESC`, userID, threshold)
	if err != nil {
		return nil, fmt.Errorf("skills: duplicate report: %w", err)
	}
	defer rows.Close()

	type rawPair struct {
		left, right string
		similarity  float64
	}
	var raw []rawPair
	ids := map[string]struct{}{}
	for rows.Next() {
		var pair rawPair
		if err := rows.Scan(&pair.left, &pair.right, &pair.similarity); err != nil {
			return nil, fmt.Errorf("skills: scan duplicate pair: %w", err)
		}
		raw = append(raw, pair)
		ids[pair.left] = struct{}{}
		ids[pair.right] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("skills: iterate duplicate pairs: %w", err)
	}

	report := &DuplicateReport{
		Threshold:   threshold,
		GeneratedAt: time.Now().UTC(),
	}
	if len(raw) == 0 {
		return report, nil
	}

	refs, err := s.documentRefs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, pair := range raw {
		left, leftOK := refs[pair.left]
		right, rightOK := refs[pair.right]
		if !leftOK || !rightOK {
			continue
		}
		report.Pairs = append(report.Pairs, DuplicatePair{Left: left, Right: right, Similarity: pair.similarity})
	}
	report.Groups = groupDuplicates(report.Pairs)

	exact, err := s.exactDuplicates(ctx, userID)
	if err != nil {
		return nil, err
	}
	report.Exact = exact

	return report, nil
}

func (s *Store) documentRefs(ctx context.Context, ids map[string]struct{}) (map[string]DocumentRef, error) {
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}

	rows, err := s.db.Query(ctx, `
SELECT id::text, slug, title, kind, status, length(content), version
FROM knowledge_documents
WHERE id::text = ANY($1::text[])`, list)
	if err != nil {
		return nil, fmt.Errorf("skills: read duplicate documents: %w", err)
	}
	defer rows.Close()

	refs := make(map[string]DocumentRef, len(list))
	for rows.Next() {
		var ref DocumentRef
		if err := rows.Scan(&ref.ID, &ref.Slug, &ref.Title, &ref.Kind, &ref.Status, &ref.Chars, &ref.Version); err != nil {
			return nil, fmt.Errorf("skills: scan duplicate document: %w", err)
		}
		refs[ref.ID] = ref
	}

	return refs, rows.Err()
}

// exactDuplicates returns the documents that share a content hash.
func (s *Store) exactDuplicates(ctx context.Context, userID int64) ([][]DocumentRef, error) {
	rows, err := s.db.Query(ctx, `
SELECT content_hash, id::text, slug, title, kind, status, length(content), version
FROM knowledge_documents
WHERE deleted_at IS NULL
  AND COALESCE(metadata->>'merged_into', '') = ''
  AND status <> 'deprecated'
  AND (user_id = $1 OR (visibility = 'shared' AND status IN ('reviewed','canonical')))
ORDER BY content_hash, slug`, userID)
	if err != nil {
		return nil, fmt.Errorf("skills: exact duplicates: %w", err)
	}
	defer rows.Close()

	var (
		groups    [][]DocumentRef
		current   string
		collected []DocumentRef
	)
	flush := func() {
		if len(collected) > 1 {
			groups = append(groups, collected)
		}
		collected = nil
	}

	for rows.Next() {
		var (
			hash string
			ref  DocumentRef
		)
		if err := rows.Scan(&hash, &ref.ID, &ref.Slug, &ref.Title, &ref.Kind, &ref.Status, &ref.Chars, &ref.Version); err != nil {
			return nil, fmt.Errorf("skills: scan exact duplicate: %w", err)
		}
		if hash != current {
			flush()
			current = hash
		}
		collected = append(collected, ref)
	}
	flush()

	return groups, rows.Err()
}

// groupDuplicates builds the connected components of the pair graph. It is pure
// so the grouping can be tested without PostgreSQL.
func groupDuplicates(pairs []DuplicatePair) []DuplicateGroup {
	if len(pairs) == 0 {
		return nil
	}

	parent := map[string]string{}
	var find func(string) string
	find = func(id string) string {
		if parent[id] == id {
			return id
		}
		root := find(parent[id])
		parent[id] = root
		return root
	}
	ensure := func(id string) {
		if _, ok := parent[id]; !ok {
			parent[id] = id
		}
	}
	union := func(left, right string) {
		leftRoot, rightRoot := find(left), find(right)
		if leftRoot != rightRoot {
			parent[rightRoot] = leftRoot
		}
	}

	refs := map[string]DocumentRef{}
	kinds := map[string]string{}

	for _, pair := range pairs {
		ensure(pair.Left.ID)
		ensure(pair.Right.ID)
		union(pair.Left.ID, pair.Right.ID)
		refs[pair.Left.ID] = pair.Left
		refs[pair.Right.ID] = pair.Right
	}

	// The components are known now; maxSim is computed on the final roots so a
	// later union cannot leave a stale entry behind.
	maxSim := map[string]float64{}
	for _, pair := range pairs {
		root := find(pair.Left.ID)
		kinds[root] = pair.Left.Kind
		if pair.Similarity > maxSim[root] {
			maxSim[root] = pair.Similarity
		}
	}

	byRoot := map[string][]DocumentRef{}
	for id, ref := range refs {
		root := find(id)
		byRoot[root] = append(byRoot[root], ref)
	}

	groups := make([]DuplicateGroup, 0, len(byRoot))
	for root, documents := range byRoot {
		if len(documents) < 2 {
			continue
		}
		sort.Slice(documents, func(i, j int) bool { return documents[i].Slug < documents[j].Slug })
		groups = append(groups, DuplicateGroup{
			Kind:          kinds[root],
			MaxSimilarity: maxSim[root],
			Documents:     documents,
		})
	}

	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Documents) != len(groups[j].Documents) {
			return len(groups[i].Documents) > len(groups[j].Documents)
		}
		return groups[i].MaxSimilarity > groups[j].MaxSimilarity
	})

	return groups
}

// MergeDocuments folds the sources into the target: their bodies are appended
// under a provenance heading, their attachments move, and they become deprecated
// with a merged_into pointer. Merging the same source twice is a no-op.
func (s *Store) MergeDocuments(ctx context.Context, userID int64, target string, sources []string) (*MergeResult, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("%w: at least one source is required", ErrInvalidInput)
	}

	targetDoc, err := s.Get(ctx, userID, target)
	if err != nil {
		return nil, err
	}
	if targetDoc.UserID != userID {
		return nil, ErrNotFound
	}
	previousMetadata := targetDoc.Metadata

	result := &MergeResult{Sources: []string{}}
	content := strings.Builder{}
	content.WriteString(strings.TrimRight(targetDoc.Content, "\n"))
	tags := append([]string{}, targetDoc.Tags...)
	seen := map[string]struct{}{targetDoc.Slug: {}}

	for _, slug := range sources {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}

		sourceDoc, err := s.Get(ctx, userID, slug)
		if err != nil {
			return nil, err
		}
		if sourceDoc.UserID != userID {
			return nil, ErrNotFound
		}

		mergedInto := metadataString(sourceDoc.Metadata, mergedIntoKey)
		if mergedInto == targetDoc.Slug {
			result.Skipped = append(result.Skipped, slug)
			continue
		}
		if mergedInto != "" {
			return nil, fmt.Errorf("%w: %q is already merged into %q", ErrInvalidInput, slug, mergedInto)
		}

		content.WriteString("\n\n## Merged from `" + sourceDoc.Slug + "`\n\n")
		content.WriteString(strings.TrimSpace(sourceDoc.Content))
		content.WriteString("\n")
		tags = append(tags, sourceDoc.Tags...)
		result.Sources = append(result.Sources, slug)
	}

	if len(result.Sources) > 0 {
		merged, err := s.ImportMarkdown(ctx, userID, ImportInput{
			Slug:        targetDoc.Slug,
			Content:     content.String(),
			Title:       targetDoc.Title,
			Description: targetDoc.Description,
			Kind:        targetDoc.Kind,
			Status:      targetDoc.Status,
			Visibility:  targetDoc.Visibility,
			SourceKind:  "merge",
			SourceURI:   "merge:" + strings.Join(result.Sources, ","),
			Language:    targetDoc.Language,
			PackID:      targetDoc.PackID,
			DefaultTags: uniqueStrings(tags),
		})
		if err != nil {
			return nil, err
		}
		targetDoc = merged
	}

	if len(result.Sources) > 0 {
		if err := s.markMerged(ctx, userID, targetDoc, previousMetadata, result); err != nil {
			return nil, err
		}

		// Reload so the caller sees the merge metadata the transaction just wrote.
		final, err := s.Get(ctx, userID, targetDoc.ID)
		if err != nil {
			return nil, err
		}
		targetDoc = final
	}

	result.Target = *targetDoc
	result.Version = targetDoc.Version

	return result, nil
}

// markMerged records the merge on the target and on every source, moving the
// attachments of the sources to the target.
func (s *Store) markMerged(ctx context.Context, userID int64, target *Document, previous map[string]any, result *MergeResult) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("skills: begin merge: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	mergedFrom := metadataStrings(previous, mergeMetadataKey)
	mergedFrom = append(mergedFrom, result.Sources...)
	mergedFrom = uniqueStrings(mergedFrom)

	targetDetails, err := json.Marshal(map[string]any{
		mergeMetadataKey: uniqueStrings(mergedFrom),
		"merged_at":      time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("skills: marshal merge metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE knowledge_documents SET metadata = metadata || $2::jsonb WHERE id = $1::uuid`,
		target.ID, string(targetDetails)); err != nil {
		return fmt.Errorf("skills: update merged target: %w", err)
	}

	for _, slug := range result.Sources {
		var sourceID string
		if err := tx.QueryRow(ctx, `
SELECT id::text FROM knowledge_documents
WHERE user_id = $1 AND slug = $2 AND deleted_at IS NULL`, userID, slug).Scan(&sourceID); err != nil {
			return fmt.Errorf("skills: find merged source %q: %w", slug, err)
		}

		moved, err := moveAttachments(ctx, tx, sourceID, target.ID, slug)
		if err != nil {
			return err
		}
		result.Files += moved

		sourceDetails, err := json.Marshal(map[string]any{
			mergedIntoKey: target.Slug,
			"merged_at":   time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("skills: marshal merge pointer: %w", err)
		}
		if _, err := tx.Exec(ctx, `
UPDATE knowledge_documents
SET status = $2, metadata = metadata || $3::jsonb
WHERE id = $1::uuid`, sourceID, StatusDeprecated, string(sourceDetails)); err != nil {
			return fmt.Errorf("skills: update merged source %q: %w", slug, err)
		}
	}

	details, err := json.Marshal(map[string]any{"sources": result.Sources, "files": result.Files})
	if err != nil {
		return fmt.Errorf("skills: marshal merge event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO knowledge_events (document_id, actor_user_id, event, details)
VALUES ($1::uuid, $2, 'merged', $3::jsonb)`, target.ID, userID, string(details)); err != nil {
		return fmt.Errorf("skills: record merge event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("skills: commit merge: %w", err)
	}

	return nil
}

// moveAttachments re-homes the attachments of a merged document under
// merged/<slug>/ so two sources cannot collide on the same path.
func moveAttachments(ctx context.Context, tx pgx.Tx, sourceID, targetID, sourceSlug string) (int, error) {
	rows, err := tx.Query(ctx, `SELECT path FROM knowledge_files WHERE document_id = $1::uuid`, sourceID)
	if err != nil {
		return 0, fmt.Errorf("skills: list merged attachments: %w", err)
	}
	defer rows.Close()

	paths := make([]string, 0)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return 0, fmt.Errorf("skills: scan merged attachment: %w", err)
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("skills: iterate merged attachments: %w", err)
	}

	moved := 0
	for _, path := range paths {
		tag, err := tx.Exec(ctx, `
UPDATE knowledge_files SET document_id = $1::uuid, path = $2
WHERE document_id = $3::uuid AND path = $4
  AND NOT EXISTS (
    SELECT 1 FROM knowledge_files f
    WHERE f.document_id = $1::uuid AND f.path = $2
  )`,
			targetID, mergedAttachmentPath(sourceSlug, path), sourceID, path)
		if err != nil {
			return moved, fmt.Errorf("skills: move attachment %q: %w", path, err)
		}
		moved += int(tag.RowsAffected())
	}

	return moved, nil
}

func mergedAttachmentPath(sourceSlug, path string) string {
	trimmed := strings.TrimPrefix(path, PackAttachmentsDir+"/")
	return PackAttachmentsDir + "/merged/" + sourceSlug + "/" + trimmed
}

func metadataString(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)

	return strings.TrimSpace(text)
}

func metadataStrings(metadata map[string]any, key string) []string {
	value, ok := metadata[key]
	if !ok {
		return nil
	}

	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{typed}
	default:
		return nil
	}
}
