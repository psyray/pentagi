// Package skills stores reusable procedures and references for PentAGI agents.
//
// A skill is a versioned markdown document with a short trigger description,
// tags and a body. It is split into chunks so retrieval can return the relevant
// section instead of the whole file. The legacy langchain_pg_embedding table
// keeps its current meaning (flow memory and agent-generated guides); skills
// live in their own tables and are queried with the hybrid vector plus lexical
// search implemented here.
package skills

import "time"

const (
	KindSkill      = "skill"
	KindReference  = "reference"
	KindEngagement = "engagement"

	StatusDraft      = "draft"
	StatusReviewed   = "reviewed"
	StatusCanonical  = "canonical"
	StatusDeprecated = "deprecated"

	VisibilityPrivate = "private"
	VisibilityShared  = "shared"
	VisibilityAdmin   = "admin"

	DefaultKind       = KindSkill
	DefaultStatus     = StatusDraft
	DefaultVisibility = VisibilityPrivate
)

// Document is one skill, reference or engagement.
type Document struct {
	ID          string
	UserID      int64
	PackID      *string
	Kind        string
	Slug        string
	Title       string
	Description string
	Content     string
	SourceKind  string
	SourceURI   *string
	ContentHash string
	Language    string
	Version     int
	Status      string
	Visibility  string
	Quality     int
	Tags        []string
	Metadata    map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// File is an attachment stored alongside a document. The bytes live in the
// user resources blob store; blob_ref is the hash that addresses them.
type File struct {
	Path    string `json:"path"`
	MIME    string `json:"mime"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	BlobRef string `json:"blobRef"`
}

// Filter narrows a list or a search.
type Filter struct {
	Kinds    []string
	Statuses []string
	Tags     []string
	Limit    int
}

// SearchHit is a ranked skill card. Excerpt is the best matching chunk, never
// the whole document.
type SearchHit struct {
	Document    Document
	Score       float64
	Excerpt     string
	HeadingPath string
	ChunkIndex  int
}

// ImportInput describes one markdown import.
type ImportInput struct {
	// Slug overrides the slug derived from the frontmatter, the title or the
	// first heading. A pack uses it to keep its directory names.
	Slug        string
	Content     string
	Title       string
	Description string
	Kind        string
	Visibility  string
	Status      string
	SourceKind  string
	SourceURI   string
	Language    string
	PackID      *string
	DefaultTags []string
	// Metadata carries extra metadata merged after the frontmatter-derived
	// keys. A key already derived from the frontmatter keeps that value, so a
	// re-anonymisation can preserve the merge pointers and the audit trail.
	Metadata map[string]any
}

// MetadataInput updates the non-content fields of a document. A nil pointer
// keeps the current value; an empty Tags slice clears the tags.
type MetadataInput struct {
	Status      *string
	Visibility  *string
	Description *string
	Quality     *int
	Tags        []string
}

// Section is a markdown heading slice of a document body.
type Section struct {
	Heading string
	Level   int
	Content string
	Offset  int
}

// MetadataString returns a trimmed string metadata value.
func (d Document) MetadataString(key string) string {
	return metadataString(d.Metadata, key)
}

// MetadataStrings returns a string list metadata value.
func (d Document) MetadataStrings(key string) []string {
	return metadataStrings(d.Metadata, key)
}
