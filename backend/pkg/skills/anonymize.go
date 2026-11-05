package skills

import (
	"context"
	"fmt"
	"regexp"
	"time"
)

// RedactedFlag is what an engagement keeps of a submitted flag: a 32 character
// hexadecimal token is only meaningful for the machine that issued it, and no
// secret pattern knows about it.
const RedactedFlag = "<REDACTED-FLAG>"

// flagTokenPattern matches a Hack The Box style flag.
var flagTokenPattern = regexp.MustCompile(`\b[0-9a-fA-F]{32}\b`)

// Replacer is the anonymisation hook of the store. It is deliberately narrower
// than the cloud anonymiser, so this package does not import it and a test can
// pass a closure.
type Replacer interface {
	ReplaceString(string) string
}

// RedactedDocument is the anonymised view of a document. The CLI uses it for a
// dry run, Reanonymize uses it to decide whether anything changed.
type RedactedDocument struct {
	Title       string
	Description string
	Content     string
	Flags       int
}

// Changed reports whether the redaction differs from the stored document.
func (r RedactedDocument) Changed(doc Document) bool {
	return r.Title != doc.Title || r.Description != doc.Description || r.Content != doc.Content
}

// RedactDocument applies the replacer to the user visible fields. Only an
// engagement has its flag token redacted: a 32 character hex string in a
// reference may be a legitimate hash.
func RedactDocument(doc Document, replacer Replacer) RedactedDocument {
	if replacer == nil {
		return RedactedDocument{
			Title:       doc.Title,
			Description: doc.Description,
			Content:     doc.Content,
		}
	}

	redacted := RedactedDocument{
		Title:       replacer.ReplaceString(doc.Title),
		Description: replacer.ReplaceString(doc.Description),
		Content:     replacer.ReplaceString(doc.Content),
	}
	if doc.Kind == KindEngagement {
		redacted.Content, redacted.Flags = redactFlags(redacted.Content)
	}

	return redacted
}

func redactFlags(text string) (string, int) {
	flags := 0
	out := flagTokenPattern.ReplaceAllStringFunc(text, func(string) string {
		flags++
		return RedactedFlag
	})

	return out, flags
}

// ReanonymizeResult reports what one re-anonymisation changed.
type ReanonymizeResult struct {
	Document    Document
	Changed     bool
	Skipped     bool
	Version     int
	Flags       int
	Title       bool
	Description bool
	Content     bool
}

// Reanonymize rewrites a document with the current anonymiser. The redacted
// body is imported as a new version, so the change is diffable and the previous
// text stays in the history. The slug, kind, status, visibility, tags and pack
// are kept; the metadata the frontmatter cannot carry is preserved, plus
// anonymized_at. A document that has nothing left to redact is skipped.
func (s *Store) Reanonymize(ctx context.Context, userID int64, idOrSlug string, replacer Replacer) (*ReanonymizeResult, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	if replacer == nil {
		return nil, fmt.Errorf("%w: an anonymiser is required", ErrInvalidInput)
	}

	doc, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return nil, err
	}
	if doc.UserID != userID {
		return nil, ErrNotFound
	}

	redacted := RedactDocument(*doc, replacer)
	result := &ReanonymizeResult{
		Document:    *doc,
		Flags:       redacted.Flags,
		Title:       redacted.Title != doc.Title,
		Description: redacted.Description != doc.Description,
		Content:     redacted.Content != doc.Content,
	}
	if !result.Title && !result.Description && !result.Content {
		result.Skipped = true
		result.Version = doc.Version
		return result, nil
	}

	metadata := make(map[string]any, len(doc.Metadata)+1)
	for key, value := range doc.Metadata {
		metadata[key] = value
	}
	metadata["anonymized_at"] = time.Now().UTC().Format(time.RFC3339)

	updated, err := s.ImportMarkdown(ctx, userID, ImportInput{
		Slug:        doc.Slug,
		Content:     redacted.Content,
		Title:       redacted.Title,
		Description: redacted.Description,
		Kind:        doc.Kind,
		Status:      doc.Status,
		Visibility:  doc.Visibility,
		SourceKind:  doc.SourceKind,
		SourceURI:   stringValue(doc.SourceURI),
		Language:    doc.Language,
		PackID:      doc.PackID,
		DefaultTags: doc.Tags,
		Metadata:    metadata,
	})
	if err != nil {
		return nil, err
	}

	result.Document = *updated
	result.Changed = true
	result.Version = updated.Version

	return result, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
