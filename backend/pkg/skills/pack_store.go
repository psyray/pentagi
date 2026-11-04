package skills

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// PackImportOptions controls one pack import.
type PackImportOptions struct {
	// Status and Visibility override the manifest for every document.
	Status     string
	Visibility string
	// AttachFile stores one attachment blob and links it to the document. When
	// nil, attachments are reported as skipped instead of failing the import.
	AttachFile func(ctx context.Context, userID int64, documentID, path string, content []byte) error
}

// PackImportResult reports what one import changed.
type PackImportResult struct {
	PackID   string
	Name     string
	Created  []string
	Updated  []string
	Files    int
	Warnings []string
}

// ImportPack writes the pack row, imports every document and links them to the
// pack. It is idempotent: a second import versions the documents instead of
// duplicating them.
func (s *Store) ImportPack(ctx context.Context, userID int64, pack *Pack, opts PackImportOptions) (*PackImportResult, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	if pack == nil || len(pack.Documents) == 0 {
		return nil, fmt.Errorf("%w: the pack contains no document", ErrInvalidInput)
	}

	packID, err := s.UpsertPack(ctx, userID, pack.Manifest)
	if err != nil {
		return nil, err
	}

	result := &PackImportResult{
		PackID:   packID,
		Name:     pack.Manifest.Name,
		Warnings: append([]string{}, pack.Warnings...),
	}

	for _, doc := range pack.Documents {
		document, err := s.ImportMarkdown(ctx, userID, ImportInput{
			Slug:       doc.Slug,
			Content:    doc.Markdown,
			Title:      doc.Title,
			Kind:       doc.Kind,
			Status:     pick(opts.Status, pack.Manifest.Status),
			Visibility: pick(opts.Visibility, pack.Manifest.Visibility),
			SourceKind: "pack",
			SourceURI:  pack.Manifest.Name + "/" + doc.SourcePath,
			Language:   "en",
			PackID:     &packID,
		})
		if err != nil {
			return nil, fmt.Errorf("skills: import %s/%s: %w", doc.Kind, doc.Slug, err)
		}

		if document.Version > 1 {
			result.Updated = append(result.Updated, document.Slug)
		} else {
			result.Created = append(result.Created, document.Slug)
		}

		if err := s.LinkPackDocument(ctx, packID, document.ID); err != nil {
			return nil, err
		}

		if len(doc.Files) == 0 {
			continue
		}
		if opts.AttachFile == nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("%s: %d attachments skipped, no blob store configured", doc.Slug, len(doc.Files)))
			continue
		}
		for _, file := range doc.Files {
			if err := opts.AttachFile(ctx, userID, document.ID, file.Path, file.Content); err != nil {
				return nil, fmt.Errorf("skills: attach %s to %s: %w", file.Path, doc.Slug, err)
			}
			result.Files++
		}
	}

	return result, nil
}

// UpsertPack creates or updates the pack row and returns its id.
func (s *Store) UpsertPack(ctx context.Context, userID int64, manifest PackManifest) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("skills: database is not configured")
	}
	name := strings.TrimSpace(manifest.Name)
	if name == "" {
		return "", fmt.Errorf("%w: the pack name is required", ErrInvalidInput)
	}

	var packID string
	err := s.db.QueryRow(ctx, `
INSERT INTO knowledge_packs (user_id, name, description, visibility, version, license)
VALUES ($1, $2, $3, COALESCE(NULLIF($4, ''), 'private'), NULLIF($5, ''), NULLIF($6, ''))
ON CONFLICT (user_id, name) DO UPDATE SET
  description = EXCLUDED.description,
  visibility = EXCLUDED.visibility,
  version = EXCLUDED.version,
  license = EXCLUDED.license,
  updated_at = CURRENT_TIMESTAMP
RETURNING id::text`,
		userID, name, manifest.Description, manifest.Visibility, manifest.Version, manifest.License,
	).Scan(&packID)
	if err != nil {
		return "", fmt.Errorf("skills: upsert pack: %w", err)
	}

	return packID, nil
}

// LinkPackDocument attaches one document to a pack, ignoring an existing link.
func (s *Store) LinkPackDocument(ctx context.Context, packID, documentID string) error {
	if s.db == nil {
		return fmt.Errorf("skills: database is not configured")
	}

	if _, err := s.db.Exec(ctx, `
INSERT INTO knowledge_pack_items (pack_id, document_id)
VALUES ($1::uuid, $2::uuid)
ON CONFLICT DO NOTHING`, packID, documentID); err != nil {
		return fmt.Errorf("skills: link document to pack: %w", err)
	}

	return nil
}

// GetPackByName returns the pack id and its stored manifest fields.
func (s *Store) GetPackByName(ctx context.Context, userID int64, name string) (string, PackManifest, error) {
	if s.db == nil {
		return "", PackManifest{}, fmt.Errorf("skills: database is not configured")
	}

	var (
		packID      string
		manifest    PackManifest
		version     *string
		license     *string
		description string
	)
	err := s.db.QueryRow(ctx, `
SELECT id::text, name, description, visibility, version, license
FROM knowledge_packs
WHERE user_id = $1 AND name = $2`, userID, strings.TrimSpace(name),
	).Scan(&packID, &manifest.Name, &description, &manifest.Visibility, &version, &license)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", PackManifest{}, fmt.Errorf("skills: pack %q: %w", name, ErrNotFound)
		}
		return "", PackManifest{}, fmt.Errorf("skills: read pack %q: %w", name, err)
	}

	manifest.Description = description
	if version != nil {
		manifest.Version = *version
	}
	if license != nil {
		manifest.License = *license
	}

	return packID, manifest, nil
}

// ListPackDocuments returns the documents of a pack, body included, ordered by
// kind then slug so an export is reproducible.
func (s *Store) ListPackDocuments(ctx context.Context, packID string) ([]Document, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}

	rows, err := s.db.Query(ctx, `
SELECT d.id::text, d.user_id, COALESCE(d.pack_id::text, ''), d.kind, d.slug, d.title, d.description,
       d.content, d.source_kind, COALESCE(d.source_uri, ''), d.content_hash, COALESCE(d.language, ''),
       d.version, d.status, d.visibility, d.quality, d.tags, d.metadata, d.created_at, d.updated_at
FROM knowledge_documents d
JOIN knowledge_pack_items i ON i.document_id = d.id
WHERE i.pack_id = $1::uuid AND d.deleted_at IS NULL
ORDER BY d.kind, d.slug`, packID)
	if err != nil {
		return nil, fmt.Errorf("skills: list pack documents: %w", err)
	}
	defer rows.Close()

	documents := make([]Document, 0)
	for rows.Next() {
		doc, err := scanDocument(rows, true)
		if err != nil {
			return nil, err
		}
		documents = append(documents, doc)
	}

	return documents, rows.Err()
}

// ExportPack rebuilds a pack tree in memory. loadBlob reads an attachment from
// the resource blob store; without it, attachments are left out with a warning
// in the returned files map being incomplete.
func (s *Store) ExportPack(
	ctx context.Context,
	userID int64,
	name string,
	loadBlob func(hash string) ([]byte, error),
) (*PackExport, *PackImportResult, error) {
	packID, manifest, err := s.GetPackByName(ctx, userID, name)
	if err != nil {
		return nil, nil, err
	}

	documents, err := s.ListPackDocuments(ctx, packID)
	if err != nil {
		return nil, nil, err
	}

	report := &PackImportResult{PackID: packID, Name: manifest.Name}
	files := make(map[string][]PackFile, len(documents))

	for _, doc := range documents {
		attachments, err := s.Files(ctx, doc.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, attachment := range attachments {
			if loadBlob == nil {
				report.Warnings = append(report.Warnings,
					fmt.Sprintf("%s: attachment %s skipped, no blob store configured", doc.Slug, attachment.Path))
				continue
			}
			content, err := loadBlob(attachment.BlobRef)
			if err != nil {
				report.Warnings = append(report.Warnings,
					fmt.Sprintf("%s: attachment %s unreadable: %v", doc.Slug, attachment.Path, err))
				continue
			}
			files[doc.ID] = append(files[doc.ID], PackFile{Path: attachment.Path, Content: content})
			report.Files++
		}
	}

	return RenderPack(manifest, documents, files), report, nil
}

func pick(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
