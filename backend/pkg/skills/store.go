package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pentagi/pkg/providers/embeddings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	embeddingDimension = 1024
	defaultListLimit   = 100
	maxListLimit       = 500
	defaultSearchLimit = 5
	maxSearchLimit     = 50
	embeddingBatchSize = 32
)

var (
	ErrNotFound            = errors.New("skill not found")
	ErrEmbedderUnavailable = errors.New("embedding provider is not available")
	ErrInvalidInput        = errors.New("invalid skill input")
)

// DB is the subset of pgxpool.Pool the store needs. Keeping it narrow lets the
// unit tests drive the pure paths without a PostgreSQL server.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Store implements skill import, listing, reading and hybrid search.
type Store struct {
	db                DB
	embedder          embeddings.Embedder
	maxEmbeddingBytes int
	chunkSize         int
	chunkOverlap      int
}

// NewStore builds a store over the shared pool. embedder may be nil, in which
// case embedding-dependent operations report ErrEmbedderUnavailable.
func NewStore(db DB, embedder embeddings.Embedder, maxEmbeddingBytes int) *Store {
	if maxEmbeddingBytes <= 0 {
		maxEmbeddingBytes = 8192
	}
	return &Store{
		db:                db,
		embedder:          embedder,
		maxEmbeddingBytes: maxEmbeddingBytes,
		chunkSize:         defaultChunkSize,
		chunkOverlap:      defaultChunkOverlap,
	}
}

func (s *Store) IsAvailable() bool {
	return s != nil && s.db != nil && s.embedder != nil && s.embedder.IsAvailable()
}

func (s *Store) requireEmbedder() error {
	if s == nil || s.embedder == nil || !s.embedder.IsAvailable() {
		return ErrEmbedderUnavailable
	}
	return nil
}

// ImportMarkdown parses frontmatter, chunks the body, embeds every chunk and
// upserts the document by (user, kind, slug). Re-importing the same slug
// replaces the chunks and increments the version.
func (s *Store) ImportMarkdown(ctx context.Context, userID int64, input ImportInput) (*Document, error) {
	if err := s.requireEmbedder(); err != nil {
		return nil, err
	}
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	if strings.TrimSpace(input.Content) == "" {
		return nil, fmt.Errorf("%w: content is required", ErrInvalidInput)
	}

	fm, body, _, err := parseMarkdown(input.Content)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("%w: content has no body after frontmatter", ErrInvalidInput)
	}

	title := trimTitle(firstNonEmpty(fm.Name, input.Title, firstHeading(body)), "Untitled skill")
	description := trimDescription(firstNonEmpty(fm.Description, firstParagraph(body)), "")
	slug := slugify(firstNonEmpty(input.Slug, fm.Name, input.Title, title))

	kind := firstNonEmpty(input.Kind, fm.Kind, kindFromFrontmatter(fm))
	if !validKind(kind) {
		return nil, fmt.Errorf("%w: unknown kind %q", ErrInvalidInput, kind)
	}
	status := firstNonEmpty(input.Status, fm.Status, DefaultStatus)
	if !validStatus(status) {
		return nil, fmt.Errorf("%w: unknown status %q", ErrInvalidInput, status)
	}
	visibility := firstNonEmpty(input.Visibility, fm.Visibility, DefaultVisibility)
	if !validVisibility(visibility) {
		return nil, fmt.Errorf("%w: unknown visibility %q", ErrInvalidInput, visibility)
	}
	language := firstNonEmpty(input.Language, fm.Language, "en")
	sourceKind := firstNonEmpty(input.SourceKind, "import")
	sourceURI := firstNonEmpty(input.SourceURI, fm.Source)

	tags := uniqueStrings(fm.Tags, []string(fm.Platform), input.DefaultTags)
	if fm.Category != "" {
		tags = uniqueStrings(tags, []string{fm.Category})
	}

	metadata := map[string]any{}
	putIfNotEmpty(metadata, "category", fm.Category)
	putIfNotEmpty(metadata, "safety", fm.Safety)
	putIfNotEmpty(metadata, "license", fm.License)
	putIfNotEmpty(metadata, "source", fm.Source)
	putIfNotEmpty(metadata, "frontmatter_version", fm.Version)
	if len(fm.Platform) > 0 {
		metadata["platform"] = []string(fm.Platform)
	}

	chunks, err := splitContent(body, s.chunkSize, s.chunkOverlap)
	if err != nil {
		return nil, err
	}
	paths := headingPaths(body, chunks)

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("skills: marshal metadata: %w", err)
	}

	newContentHash := contentHash(body)
	var existingHash string
	err = s.db.QueryRow(ctx, `
SELECT content_hash FROM knowledge_documents
WHERE user_id = $1 AND kind = $2 AND slug = $3 AND deleted_at IS NULL`, userID, kind, slug).Scan(&existingHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("skills: read existing document: %w", err)
	}
	contentChanged := existingHash != newContentHash

	var vectors [][]float32
	if contentChanged {
		embeddingInputs := make([]string, len(chunks))
		for i, chunk := range chunks {
			embeddingInputs[i] = embeddingText(title, paths[i], chunk, s.maxEmbeddingBytes)
		}
		vectors, err = s.embed(ctx, embeddingInputs)
		if err != nil {
			return nil, err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("skills: begin import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		documentID string
		version    int
		createdAt  time.Time
		updatedAt  time.Time
	)
	err = tx.QueryRow(ctx, `
INSERT INTO knowledge_documents
  (user_id, pack_id, kind, slug, title, description, content, source_kind, source_uri,
   content_hash, language, version, status, visibility, quality, tags, metadata)
VALUES
  ($1, $2::uuid, $3, $4, $5, $6, $7, $8, $9,
   $10, $11, 1, $12, $13, 0, $14, $15::jsonb)
ON CONFLICT (user_id, kind, slug) DO UPDATE SET
  pack_id = EXCLUDED.pack_id,
  title = EXCLUDED.title,
  description = EXCLUDED.description,
  content = EXCLUDED.content,
  source_kind = EXCLUDED.source_kind,
  source_uri = EXCLUDED.source_uri,
  content_hash = EXCLUDED.content_hash,
  language = EXCLUDED.language,
  version = knowledge_documents.version + 1,
  status = EXCLUDED.status,
  visibility = EXCLUDED.visibility,
  tags = EXCLUDED.tags,
  metadata = EXCLUDED.metadata,
  deleted_at = NULL
RETURNING id::text, version, created_at, updated_at`,
		userID, nullableString(input.PackID), kind, slug, title, description, body,
		sourceKind, nullableString(stringPtr(sourceURI)), newContentHash, language,
		status, visibility, tags, string(metadataJSON),
	).Scan(&documentID, &version, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("skills: upsert document: %w", err)
	}

	if contentChanged {
		if _, err := tx.Exec(ctx, `DELETE FROM knowledge_chunks WHERE document_id = $1::uuid`, documentID); err != nil {
			return nil, fmt.Errorf("skills: clear old chunks: %w", err)
		}

		for i, chunk := range chunks {
			chunkMeta, err := json.Marshal(map[string]any{
				"slug": slug, "kind": kind, "title": title, "heading_path": paths[i],
			})
			if err != nil {
				return nil, fmt.Errorf("skills: marshal chunk metadata: %w", err)
			}
			_, err = tx.Exec(ctx, `
INSERT INTO knowledge_chunks
  (id, document_id, chunk_index, heading_path, content, content_hash, embedding, metadata)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::vector, $8::jsonb)`,
				uuid.NewString(), documentID, i, paths[i], chunk, contentHash(chunk), formatVector(vectors[i]), string(chunkMeta),
			)
			if err != nil {
				return nil, fmt.Errorf("skills: insert chunk %d: %w", i, err)
			}
		}
	}

	event := "imported"
	if version > 1 {
		event = "updated"
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO knowledge_events (document_id, actor_user_id, event, details)
VALUES ($1::uuid, $2, $3, $4::jsonb)`,
		documentID, userID, event,
		fmt.Sprintf(`{"chunks":%d,"reembedded":%t}`, len(chunks), contentChanged),
	); err != nil {
		return nil, fmt.Errorf("skills: record import event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("skills: commit import: %w", err)
	}

	return &Document{
		ID: documentID, UserID: userID, PackID: input.PackID, Kind: kind, Slug: slug,
		Title: title, Description: description, Content: body, SourceKind: sourceKind,
		SourceURI: stringPtr(sourceURI), ContentHash: newContentHash, Language: language,
		Version: version, Status: status, Visibility: visibility, Tags: tags,
		Metadata: metadata, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

func (s *Store) embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	vectors := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += embeddingBatchSize {
		end := min(start+embeddingBatchSize, len(texts))
		batch, err := s.embedder.EmbedDocuments(ctx, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("skills: embed chunks: %w", err)
		}
		if len(batch) != end-start {
			return nil, fmt.Errorf("skills: embedder returned %d vectors for %d chunks", len(batch), end-start)
		}
		for _, vector := range batch {
			if len(vector) != embeddingDimension {
				return nil, fmt.Errorf(
					"skills: embedding has %d dimensions, the knowledge_chunks table expects %d",
					len(vector), embeddingDimension,
				)
			}
		}
		vectors = append(vectors, batch...)
	}

	return vectors, nil
}

func (s *Store) queryEmbedding(ctx context.Context, query string) ([]float32, error) {
	if err := s.requireEmbedder(); err != nil {
		return nil, err
	}
	vector, err := s.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("skills: embed query: %w", err)
	}
	if len(vector) != embeddingDimension {
		return nil, fmt.Errorf(
			"skills: query embedding has %d dimensions, the knowledge_chunks table expects %d",
			len(vector), embeddingDimension,
		)
	}
	return vector, nil
}

// List returns the caller's documents plus shared reviewed/canonical ones.
func (s *Store) List(ctx context.Context, userID int64, filter Filter) ([]Document, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	limit := clampLimit(filter.Limit, defaultListLimit, maxListLimit)
	statuses := defaultStatuses(filter.Statuses)

	rows, err := s.db.Query(ctx, `
SELECT id::text, user_id, COALESCE(pack_id::text, ''), kind, slug, title, description,
       '' AS content, source_kind, COALESCE(source_uri, ''), content_hash, COALESCE(language, ''),
       version, status, visibility, quality, tags, metadata, created_at, updated_at
FROM knowledge_documents
WHERE deleted_at IS NULL
  AND (user_id = $1 OR (visibility = 'shared' AND status IN ('reviewed','canonical')))
  AND ($2::text[] IS NULL OR kind = ANY($2))
  AND ($3::text[] IS NULL OR status = ANY($3))
  AND ($4::text[] IS NULL OR tags && $4)
ORDER BY updated_at DESC
LIMIT $5`, userID, nullableStrings(filter.Kinds), nullableStrings(statuses), nullableStrings(filter.Tags), limit)
	if err != nil {
		return nil, fmt.Errorf("skills: list documents: %w", err)
	}
	defer rows.Close()

	documents := make([]Document, 0)
	for rows.Next() {
		doc, err := scanDocument(rows, false)
		if err != nil {
			return nil, err
		}
		documents = append(documents, doc)
	}
	return documents, rows.Err()
}

// Get returns one document by id or slug, including its body.
func (s *Store) Get(ctx context.Context, userID int64, idOrSlug string) (*Document, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	idOrSlug = strings.TrimSpace(idOrSlug)
	if idOrSlug == "" {
		return nil, fmt.Errorf("%w: slug is required", ErrInvalidInput)
	}

	row := s.db.QueryRow(ctx, `
SELECT id::text, user_id, COALESCE(pack_id::text, ''), kind, slug, title, description,
       content, source_kind, COALESCE(source_uri, ''), content_hash, COALESCE(language, ''),
       version, status, visibility, quality, tags, metadata, created_at, updated_at
FROM knowledge_documents
WHERE deleted_at IS NULL
  AND (id::text = $1 OR slug = $1)
  AND (user_id = $2 OR (visibility = 'shared' AND status IN ('reviewed','canonical')))
LIMIT 1`, idOrSlug, userID)

	doc, err := scanDocument(row, true)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &doc, nil
}

// Read returns the document and its attachments.
func (s *Store) Read(ctx context.Context, userID int64, idOrSlug string) (*Document, []File, error) {
	doc, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return nil, nil, err
	}
	files, err := s.Files(ctx, doc.ID)
	if err != nil {
		return nil, nil, err
	}
	return doc, files, nil
}

// Files lists the attachments declared for a document.
func (s *Store) Files(ctx context.Context, documentID string) ([]File, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	rows, err := s.db.Query(ctx, `
SELECT path, COALESCE(mime, ''), size, COALESCE(sha256, ''), blob_ref
FROM knowledge_files
WHERE document_id = $1::uuid
ORDER BY path`, documentID)
	if err != nil {
		return nil, fmt.Errorf("skills: list files: %w", err)
	}
	defer rows.Close()

	files := make([]File, 0)
	for rows.Next() {
		var file File
		if err := rows.Scan(&file.Path, &file.MIME, &file.Size, &file.SHA256, &file.BlobRef); err != nil {
			return nil, fmt.Errorf("skills: scan file: %w", err)
		}
		files = append(files, file)
	}
	return files, rows.Err()
}

// AddFile upserts an attachment record for a document owned by the caller.
// The blob itself is written by the caller (the REST layer reuses the resource
// blob store); BlobRef is the blob-store key.
func (s *Store) AddFile(ctx context.Context, userID int64, idOrSlug string, file File) (*File, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	if strings.TrimSpace(file.Path) == "" {
		return nil, fmt.Errorf("%w: file path is required", ErrInvalidInput)
	}
	if strings.TrimSpace(file.BlobRef) == "" {
		return nil, fmt.Errorf("%w: blob reference is required", ErrInvalidInput)
	}

	doc, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return nil, err
	}
	if doc.UserID != userID {
		return nil, ErrNotFound
	}

	_, err = s.db.Exec(ctx, `
INSERT INTO knowledge_files (document_id, path, mime, size, sha256, blob_ref)
VALUES ($1::uuid, $2, $3, $4, $5, $6)
ON CONFLICT (document_id, path) DO UPDATE SET
  mime = EXCLUDED.mime,
  size = EXCLUDED.size,
  sha256 = EXCLUDED.sha256,
  blob_ref = EXCLUDED.blob_ref`,
		doc.ID, file.Path, file.MIME, file.Size, file.SHA256, file.BlobRef)
	if err != nil {
		return nil, fmt.Errorf("skills: add file: %w", err)
	}

	return &file, nil
}

// GetFile returns one attachment of a document readable by the caller.
func (s *Store) GetFile(ctx context.Context, userID int64, idOrSlug, filePath string) (*File, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}

	doc, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return nil, err
	}

	var file File
	err = s.db.QueryRow(ctx, `
SELECT path, COALESCE(mime, ''), size, COALESCE(sha256, ''), blob_ref
FROM knowledge_files
WHERE document_id = $1::uuid AND path = $2`, doc.ID, filePath).
		Scan(&file.Path, &file.MIME, &file.Size, &file.SHA256, &file.BlobRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("skills: get file: %w", err)
	}

	return &file, nil
}

// DeleteFile removes an attachment record from a document owned by the caller
// and returns the blob reference so the caller can delete an orphan blob.
func (s *Store) DeleteFile(ctx context.Context, userID int64, idOrSlug, filePath string) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("skills: database is not configured")
	}

	doc, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return "", err
	}
	if doc.UserID != userID {
		return "", ErrNotFound
	}

	var blobRef string
	err = s.db.QueryRow(ctx, `
DELETE FROM knowledge_files
WHERE document_id = $1::uuid AND path = $2
RETURNING blob_ref`, doc.ID, filePath).Scan(&blobRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("skills: delete file: %w", err)
	}

	return blobRef, nil
}

// BlobReferenced reports whether any attachment still points at blobRef.
func (s *Store) BlobReferenced(ctx context.Context, blobRef string) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("skills: database is not configured")
	}

	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM knowledge_files WHERE blob_ref = $1)`, blobRef).
		Scan(&exists); err != nil {
		return false, fmt.Errorf("skills: check blob reference: %w", err)
	}

	return exists, nil
}

// UpdateMetadata changes the lifecycle fields of a document owned by the
// caller. Content and embeddings are untouched, so no re-embedding is needed.
func (s *Store) UpdateMetadata(ctx context.Context, userID int64, idOrSlug string, input MetadataInput) (*Document, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	if input.Status != nil && !validStatus(*input.Status) {
		return nil, fmt.Errorf("%w: unknown status %q", ErrInvalidInput, *input.Status)
	}
	if input.Visibility != nil && !validVisibility(*input.Visibility) {
		return nil, fmt.Errorf("%w: unknown visibility %q", ErrInvalidInput, *input.Visibility)
	}
	if input.Quality != nil && (*input.Quality < 0 || *input.Quality > 100) {
		return nil, fmt.Errorf("%w: quality must be between 0 and 100", ErrInvalidInput)
	}

	doc, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return nil, err
	}
	if doc.UserID != userID {
		return nil, ErrNotFound
	}

	var tags any
	if input.Tags != nil {
		tags = input.Tags
	}

	_, err = s.db.Exec(ctx, `
UPDATE knowledge_documents
SET status = COALESCE($2, status),
    visibility = COALESCE($3, visibility),
    description = COALESCE($4, description),
    quality = COALESCE($5, quality),
    tags = COALESCE($6::text[], tags)
WHERE id = $1::uuid AND user_id = $7 AND deleted_at IS NULL`,
		doc.ID, input.Status, input.Visibility, input.Description, input.Quality, tags, userID)
	if err != nil {
		return nil, fmt.Errorf("skills: update metadata: %w", err)
	}

	return s.Get(ctx, userID, doc.ID)
}

// Delete soft-deletes a document and removes its chunks.
func (s *Store) Delete(ctx context.Context, userID int64, idOrSlug string) error {
	if s.db == nil {
		return fmt.Errorf("skills: database is not configured")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("skills: begin delete: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var documentID string
	err = tx.QueryRow(ctx, `
UPDATE knowledge_documents
SET deleted_at = CURRENT_TIMESTAMP
WHERE deleted_at IS NULL AND user_id = $2 AND (id::text = $1 OR slug = $1)
RETURNING id::text`, idOrSlug, userID).Scan(&documentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("skills: delete document: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM knowledge_chunks WHERE document_id = $1::uuid`, documentID); err != nil {
		return fmt.Errorf("skills: delete chunks: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO knowledge_events (document_id, actor_user_id, event, details)
VALUES ($1::uuid, $2, 'deleted', '{}'::jsonb)`, documentID, userID); err != nil {
		return fmt.Errorf("skills: record delete event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("skills: commit delete: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDocument(row rowScanner, withContent bool) (Document, error) {
	var (
		doc       Document
		packID    string
		sourceURI string
		tags      []string
		meta      []byte
	)

	err := row.Scan(
		&doc.ID, &doc.UserID, &packID, &doc.Kind, &doc.Slug, &doc.Title, &doc.Description,
		&doc.Content, &doc.SourceKind, &sourceURI, &doc.ContentHash, &doc.Language,
		&doc.Version, &doc.Status, &doc.Visibility, &doc.Quality, &tags, &meta,
		&doc.CreatedAt, &doc.UpdatedAt,
	)
	if err != nil {
		return Document{}, err
	}

	if !withContent {
		doc.Content = ""
	}
	if packID != "" {
		doc.PackID = &packID
	}
	if sourceURI != "" {
		doc.SourceURI = &sourceURI
	}
	doc.Tags = tags
	if len(meta) > 0 {
		_ = json.Unmarshal(meta, &doc.Metadata)
	}
	if doc.Metadata == nil {
		doc.Metadata = map[string]any{}
	}
	return doc, nil
}

func defaultStatuses(values []string) []string {
	if len(values) > 0 {
		return values
	}
	return []string{StatusDraft, StatusReviewed, StatusCanonical}
}

func clampLimit(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func nullableStrings(values []string) any {
	if len(values) == 0 {
		return nil
	}
	return values
}

func nullableString(value *string) any {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return *value
}

func stringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	value = strings.TrimSpace(value)
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func putIfNotEmpty(target map[string]any, key, value string) {
	if strings.TrimSpace(value) != "" {
		target[key] = value
	}
}

func kindFromFrontmatter(fm frontMatter) string {
	if fm.Kind != "" {
		return fm.Kind
	}
	if fm.Name != "" || fm.Description != "" {
		return KindSkill
	}
	return KindReference
}

func validKind(kind string) bool {
	switch kind {
	case KindSkill, KindReference, KindEngagement:
		return true
	default:
		return false
	}
}

func validStatus(status string) bool {
	switch status {
	case StatusDraft, StatusReviewed, StatusCanonical, StatusDeprecated:
		return true
	default:
		return false
	}
}

func validVisibility(visibility string) bool {
	switch visibility {
	case VisibilityPrivate, VisibilityShared, VisibilityAdmin:
		return true
	default:
		return false
	}
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(normalizeContent(content)))
	return hex.EncodeToString(sum[:])
}

func normalizeContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			if len(out) > 0 && out[len(out)-1] == "" {
				continue
			}
			out = append(out, "")
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func formatVector(vector []float32) string {
	values := make([]string, len(vector))
	for i, value := range vector {
		values[i] = strconv.FormatFloat(float64(value), 'f', -1, 32)
	}
	return "[" + strings.Join(values, ",") + "]"
}
