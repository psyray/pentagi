package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pmezard/go-difflib/difflib"
)

// maxDiffSourceChars bounds a diff. The algorithm is quadratic in the worst
// case, so refusing an oversized pair is safer than freezing the API.
const maxDiffSourceChars = 500_000

// DocumentVersion is one stored revision of a document, body included.
type DocumentVersion struct {
	DocumentID  string
	Version     int
	Title       string
	Description string
	Content     string
	ContentHash string
	Status      string
	Visibility  string
	Tags        []string
	Metadata    map[string]any
	ActorUserID *int64
	CreatedAt   time.Time
}

// VersionSummary describes one revision without its body.
type VersionSummary struct {
	Version     int
	Title       string
	Status      string
	ContentHash string
	Chars       int
	ActorUserID *int64
	CreatedAt   time.Time
}

// VersionDiff is the unified diff between two revisions.
type VersionDiff struct {
	From    int
	To      int
	Diff    string
	Added   int
	Removed int
	Changed bool
}

// ListVersions returns the history of a document, newest first.
func (s *Store) ListVersions(ctx context.Context, userID int64, idOrSlug string) ([]VersionSummary, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}

	doc, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(ctx, `
SELECT version, title, status, content_hash, length(content), actor_user_id, created_at
FROM knowledge_document_versions
WHERE document_id = $1::uuid
ORDER BY version DESC`, doc.ID)
	if err != nil {
		return nil, fmt.Errorf("skills: list versions: %w", err)
	}
	defer rows.Close()

	versions := make([]VersionSummary, 0)
	for rows.Next() {
		var (
			summary VersionSummary
			actor   *int64
		)
		if err := rows.Scan(&summary.Version, &summary.Title, &summary.Status, &summary.ContentHash,
			&summary.Chars, &actor, &summary.CreatedAt); err != nil {
			return nil, fmt.Errorf("skills: scan version: %w", err)
		}
		summary.ActorUserID = actor
		versions = append(versions, summary)
	}

	return versions, rows.Err()
}

// GetVersion returns one revision, including its body.
func (s *Store) GetVersion(ctx context.Context, userID int64, idOrSlug string, version int) (*DocumentVersion, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}

	doc, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return nil, err
	}

	row := s.db.QueryRow(ctx, `
SELECT version, title, description, content, content_hash, status, visibility, tags, metadata,
       actor_user_id, created_at
FROM knowledge_document_versions
WHERE document_id = $1::uuid AND version = $2`, doc.ID, version)

	var (
		revision DocumentVersion
		tags     []string
		metadata []byte
		actor    *int64
	)
	err = row.Scan(&revision.Version, &revision.Title, &revision.Description, &revision.Content,
		&revision.ContentHash, &revision.Status, &revision.Visibility, &tags, &metadata, &actor,
		&revision.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("skills: version %d: %w", version, ErrNotFound)
		}
		return nil, fmt.Errorf("skills: read version: %w", err)
	}

	revision.DocumentID = doc.ID
	revision.Tags = tags
	revision.ActorUserID = actor
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &revision.Metadata)
	}
	if revision.Metadata == nil {
		revision.Metadata = map[string]any{}
	}

	return &revision, nil
}

// DiffVersions returns a unified diff between two revisions. A missing `to`
// means the current version.
func (s *Store) DiffVersions(ctx context.Context, userID int64, idOrSlug string, from, to int) (*VersionDiff, error) {
	doc, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return nil, err
	}
	if from <= 0 {
		return nil, fmt.Errorf("%w: a from version is required", ErrInvalidInput)
	}
	if to <= 0 {
		to = doc.Version
	}

	source, err := s.GetVersion(ctx, userID, doc.ID, from)
	if err != nil {
		return nil, err
	}
	target, err := s.GetVersion(ctx, userID, doc.ID, to)
	if err != nil {
		return nil, err
	}

	return DiffDocuments(source, target)
}

// DiffDocuments builds the unified diff of two revisions. It is pure so the
// rendering can be tested without PostgreSQL.
func DiffDocuments(from, to *DocumentVersion) (*VersionDiff, error) {
	if from == nil || to == nil {
		return nil, fmt.Errorf("%w: two versions are required", ErrInvalidInput)
	}
	if len(from.Content) > maxDiffSourceChars || len(to.Content) > maxDiffSourceChars {
		return nil, fmt.Errorf("%w: a document is longer than %d characters", ErrInvalidInput, maxDiffSourceChars)
	}

	result := &VersionDiff{From: from.Version, To: to.Version}
	if from.Content == to.Content {
		return result, nil
	}
	result.Changed = true

	patch, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(from.Content),
		B:        difflib.SplitLines(to.Content),
		FromFile: fmt.Sprintf("v%d", from.Version),
		ToFile:   fmt.Sprintf("v%d", to.Version),
		Context:  3,
	})
	if err != nil {
		return nil, fmt.Errorf("skills: build diff: %w", err)
	}
	result.Diff = patch

	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "+"):
			result.Added++
		case strings.HasPrefix(line, "-"):
			result.Removed++
		}
	}

	return result, nil
}

// writeVersionRow records the state of a document right after an import. It is
// called inside the import transaction so a version never exists without its
// document.
func (s *Store) writeVersionRow(
	ctx context.Context,
	tx pgx.Tx,
	documentID string,
	version int,
	title, description, content, contentHash, status, visibility string,
	tags []string,
	metadata string,
	actorUserID int64,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO knowledge_document_versions
  (document_id, version, title, description, content, content_hash, status, visibility, tags, metadata, actor_user_id)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11)
ON CONFLICT (document_id, version) DO UPDATE SET
  title = EXCLUDED.title,
  description = EXCLUDED.description,
  content = EXCLUDED.content,
  content_hash = EXCLUDED.content_hash,
  status = EXCLUDED.status,
  visibility = EXCLUDED.visibility,
  tags = EXCLUDED.tags,
  metadata = EXCLUDED.metadata,
  actor_user_id = EXCLUDED.actor_user_id`,
		documentID, version, title, description, content, contentHash, status, visibility,
		tags, metadata, actorUserID); err != nil {
		return fmt.Errorf("skills: store version %d: %w", version, err)
	}

	return nil
}

// publishTransitions is the review lifecycle. A missing entry refuses the move,
// which keeps a draft from jumping straight to canonical.
var publishTransitions = map[string][]string{
	StatusDraft:      {StatusReviewed},
	StatusReviewed:   {StatusCanonical, StatusDraft},
	StatusCanonical:  {StatusDeprecated, StatusReviewed},
	StatusDeprecated: {StatusReviewed, StatusDraft},
}

// Publish moves a document through the review lifecycle and records the actor
// and the note in the event log.
func (s *Store) Publish(ctx context.Context, userID int64, idOrSlug, target, note string) (*Document, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	target = strings.TrimSpace(target)
	if !validStatus(target) {
		return nil, fmt.Errorf("%w: unknown status %q", ErrInvalidInput, target)
	}

	doc, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return nil, err
	}
	if doc.UserID != userID {
		return nil, ErrNotFound
	}
	if doc.Status == target {
		return doc, nil
	}
	if !publishAllowed(doc.Status, target) {
		return nil, fmt.Errorf("%w: cannot move a %s document to %s", ErrInvalidInput, doc.Status, target)
	}
	if target == StatusReviewed && strings.TrimSpace(doc.Description) == "" {
		return nil, fmt.Errorf("%w: a reviewed document needs a description", ErrInvalidInput)
	}
	if target == StatusCanonical && len(doc.Tags) == 0 {
		return nil, fmt.Errorf("%w: a canonical document needs at least one tag", ErrInvalidInput)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("skills: begin publish: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
UPDATE knowledge_documents SET status = $2 WHERE id = $1::uuid AND deleted_at IS NULL`,
		doc.ID, target); err != nil {
		return nil, fmt.Errorf("skills: publish document: %w", err)
	}

	details, err := json.Marshal(map[string]any{"from": doc.Status, "to": target, "note": strings.TrimSpace(note)})
	if err != nil {
		return nil, fmt.Errorf("skills: marshal publish event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO knowledge_events (document_id, actor_user_id, event, details)
VALUES ($1::uuid, $2, 'published', $3::jsonb)`, doc.ID, userID, string(details)); err != nil {
		return nil, fmt.Errorf("skills: record publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("skills: commit publish: %w", err)
	}

	return s.Get(ctx, userID, doc.ID)
}

func publishAllowed(from, to string) bool {
	for _, allowed := range publishTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}
