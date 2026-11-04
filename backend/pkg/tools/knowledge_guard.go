package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"pentagi/pkg/database"

	"github.com/vxcontrol/cloud/anonymizer"
	"github.com/vxcontrol/langchaingo/schema"
	"github.com/vxcontrol/langchaingo/vectorstores"
)

const (
	// knowledgeDedupResultLimit bounds the pre-insertion lookup. Three documents
	// are enough to catch an exact copy and the closest near-duplicate without
	// turning every write into a broad scan.
	knowledgeDedupResultLimit = 3

	// knowledgeDedupScoreThreshold is the cosine similarity at which a stored
	// question is treated as the same topic. It is deliberately conservative:
	// two guides about the same procedure with different wording are duplicates,
	// two procedures about the same tool are not.
	knowledgeDedupScoreThreshold = 0.93
)

// KnowledgeSearchOption configures how the knowledge tools scope their reads.
// The options are variadic so existing callers and tests keep compiling.
type KnowledgeSearchOption func(*knowledgeSearchOptions)

type knowledgeSearchOptions struct {
	shared      bool
	deduplicate bool
}

// WithSharedKnowledgeSearch lets a tool read and deduplicate across every user
// when shared is true, or only across the caller's own documents when false.
//
// The resolved default is the historical shared behaviour, so out-of-tree
// callers keep their semantics. The server always passes the configured value.
func WithSharedKnowledgeSearch(shared bool) KnowledgeSearchOption {
	return func(o *knowledgeSearchOptions) {
		o.shared = shared
	}
}

// WithKnowledgeDeduplication enables or disables the pre-insertion duplicate
// lookup. It is on by default; a caller that wires a store it cannot query
// (unit tests with a zero value) turns it off explicitly.
func WithKnowledgeDeduplication(enabled bool) KnowledgeSearchOption {
	return func(o *knowledgeSearchOptions) {
		o.deduplicate = enabled
	}
}

func resolveKnowledgeSearchOptions(opts []KnowledgeSearchOption) knowledgeSearchOptions {
	options := knowledgeSearchOptions{shared: true, deduplicate: true}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	return options
}

// knowledgeScopeFilters returns the metadata predicates that restrict a search
// or a deduplication lookup to the caller's documents. An empty map is the
// historical shared store.
func knowledgeScopeFilters(userID int64, shared bool) map[string]any {
	if shared {
		return map[string]any{}
	}
	return map[string]any{"user_id": strconv.FormatInt(userID, 10)}
}

// knowledgeFingerprint normalises a document and hashes it, so two writes that
// differ only by line endings, trailing spaces or repeated blank lines collide.
func knowledgeFingerprint(content string) string {
	sum := sha256.Sum256([]byte(normalizeKnowledgeText(content)))
	return hex.EncodeToString(sum[:])
}

func normalizeKnowledgeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	lines := strings.Split(s, "\n")
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

// knowledgeDuplicate is a document that already covers the one being stored.
type knowledgeDuplicate struct {
	Document schema.Document
	Exact    bool
}

// findKnowledgeDuplicate looks for an earlier document under the same
// doc_type, subtype and visibility scope. It reports an exact copy when the
// stored content hash matches, and a near-duplicate when the closest question
// reaches knowledgeDedupScoreThreshold.
func findKnowledgeDuplicate(
	ctx context.Context,
	store vectorstores.VectorStore,
	filters map[string]any,
	query string,
	contentHash string,
) (*knowledgeDuplicate, error) {
	if store == nil || strings.TrimSpace(query) == "" {
		return nil, nil
	}

	docs, err := store.SimilaritySearch(
		ctx,
		query,
		knowledgeDedupResultLimit,
		vectorstores.WithFilters(filters),
	)
	if err != nil {
		return nil, err
	}

	for _, doc := range docs {
		if hash, ok := doc.Metadata["content_hash"].(string); ok && hash != "" && hash == contentHash {
			return &knowledgeDuplicate{Document: doc, Exact: true}, nil
		}
	}
	if len(docs) > 0 && docs[0].Score >= knowledgeDedupScoreThreshold {
		return &knowledgeDuplicate{Document: docs[0]}, nil
	}

	return nil, nil
}

// describeKnowledgeDuplicate renders the tool answer for a skipped write. The
// question is included because the vector store does not return the row id.
func describeKnowledgeDuplicate(kind string, dup *knowledgeDuplicate) string {
	question, _ := dup.Document.Metadata["question"].(string)
	question = strings.TrimSpace(question)
	if question == "" {
		question = "an earlier document"
	}

	if dup.Exact {
		return fmt.Sprintf(
			"not stored: an identical %s already exists (question: %q); reuse it with the matching search tool instead of duplicating it",
			kind, question,
		)
	}

	return fmt.Sprintf(
		"not stored: a similar %s already exists (question: %q, similarity %.3f); reuse it with the matching search tool or update it explicitly instead of duplicating it",
		kind, question, dup.Document.Score,
	)
}

// logKnowledgeOperation records one vector-store operation, including an empty
// retrieval or a skipped duplicate. Misses used to return before the log call,
// which made the hit rate unmeasurable.
func logKnowledgeOperation(
	ctx context.Context,
	vslp VectorStoreLogProvider,
	replacer anonymizer.Replacer,
	taskID, subtaskID *int64,
	filters map[string]any,
	query string,
	action database.VecstoreActionType,
	result string,
) {
	if vslp == nil {
		return
	}
	agentCtx, ok := GetAgentContext(ctx)
	if !ok {
		return
	}
	filtersData, err := json.Marshal(filters)
	if err != nil {
		return
	}
	if replacer != nil {
		query = replacer.ReplaceString(query)
	}
	_, _ = vslp.PutLog(
		ctx,
		agentCtx.ParentAgentType,
		agentCtx.CurrentAgentType,
		filtersData,
		query,
		action,
		result,
		taskID,
		subtaskID,
	)
}

// clampKnowledgeResult keeps a retrieval inside one tool-result budget. It cuts
// on a rune boundary so the marker never splits a multi-byte character.
func clampKnowledgeResult(result string, limit int) string {
	if limit <= 0 || len(result) <= limit {
		return result
	}

	cut := limit
	for cut > 0 && !utf8.RuneStart(result[cut]) {
		cut--
	}

	return fmt.Sprintf(
		"%s\n\n[knowledge result truncated: %d bytes omitted; use a narrower query or request a single document]\n",
		result[:cut], len(result)-cut,
	)
}
