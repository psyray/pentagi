package skills

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ScopedStore restricts every read to an explicit set of packs. A flow created
// with a pack selection (an A/B run, a client engagement that must not see the
// shared corpus) reads only those packs: the catalogue, list_skills,
// search_skills, read_skill, the legacy dual-read block and the surfacing all go
// through the same store, so the restriction holds everywhere without each
// caller having to remember the filter.
//
// An empty selection means every visible pack, and Scope then returns the store
// itself, so a flow without a selection pays nothing.
type ScopedStore struct {
	inner *Store
	packs []string
}

// Scope returns a store limited to packIDs. The ids are not validated here: an
// unknown id simply matches no document, which is the safe outcome.
func (s *Store) Scope(packIDs []string) *ScopedStore {
	return &ScopedStore{inner: s, packs: normalisePackIDs(packIDs)}
}

// Unscoped returns the store behind the scope, or nil when the receiver is nil.
func (s *ScopedStore) Unscoped() *Store {
	if s == nil {
		return nil
	}
	return s.inner
}

// Packs returns the pack selection applied to this store, empty when unscoped.
func (s *ScopedStore) Packs() []string {
	if s == nil {
		return nil
	}
	return s.packs
}

func (s *ScopedStore) IsAvailable() bool {
	return s != nil && s.inner != nil && s.inner.IsAvailable()
}

func (s *ScopedStore) List(ctx context.Context, userID int64, filter Filter) ([]Document, error) {
	if s == nil {
		return nil, fmt.Errorf("skills: the scoped store is nil")
	}
	filter.PackIDs = s.packs
	return s.inner.List(ctx, userID, filter)
}

func (s *ScopedStore) Search(ctx context.Context, userID int64, query string, filter Filter, limit int) ([]SearchHit, error) {
	if s == nil {
		return nil, fmt.Errorf("skills: the scoped store is nil")
	}
	filter.PackIDs = s.packs
	return s.inner.Search(ctx, userID, query, filter, limit)
}

// Read refuses a document outside the selection, so an agent cannot escape the
// scope by guessing a slug it saw somewhere else.
func (s *ScopedStore) Read(ctx context.Context, userID int64, idOrSlug string) (*Document, []File, error) {
	if s == nil {
		return nil, nil, fmt.Errorf("skills: the scoped store is nil")
	}

	doc, files, err := s.inner.Read(ctx, userID, idOrSlug)
	if err != nil {
		return nil, nil, err
	}
	if !s.allows(doc) {
		return nil, nil, ErrNotFound
	}

	return doc, files, nil
}

// SearchEngagements ranks the visible engagements and then drops the ones
// outside the selection: the ranking weights do not know about packs, so
// filtering afterwards keeps the scores unchanged.
func (s *ScopedStore) SearchEngagements(ctx context.Context, userID int64, query EngagementQuery) ([]EngagementMatch, error) {
	if s == nil {
		return nil, fmt.Errorf("skills: the scoped store is nil")
	}
	if len(s.packs) == 0 {
		return s.inner.SearchEngagements(ctx, userID, query)
	}

	matches, err := s.inner.SearchEngagements(ctx, userID, query)
	if err != nil {
		return nil, err
	}

	kept := make([]EngagementMatch, 0, len(matches))
	for _, match := range matches {
		if s.allows(&match.Document) {
			kept = append(kept, match)
		}
	}

	return kept, nil
}

// RecordEvent delegates: usage analysis is about what the flow was shown, so it
// must keep working under a scope.
func (s *ScopedStore) RecordEvent(ctx context.Context, userID int64, event Event) error {
	if s == nil {
		return fmt.Errorf("skills: the scoped store is nil")
	}
	return s.inner.RecordEvent(ctx, userID, event)
}

func (s *ScopedStore) allows(doc *Document) bool {
	if len(s.packs) == 0 {
		return true
	}
	if doc == nil || doc.PackID == nil {
		return false
	}

	packID := strings.TrimSpace(*doc.PackID)
	for _, allowed := range s.packs {
		if allowed == packID {
			return true
		}
	}

	return false
}

// ParsePackIDs validates a pack selection coming from an API and drops the
// duplicates. An empty selection, or none at all, means no restriction.
func ParsePackIDs(ids []string) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	parsed := make([]uuid.UUID, 0, len(ids))
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, raw := range ids {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid pack id %q", ErrInvalidInput, raw)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		parsed = append(parsed, id)
	}

	if len(parsed) == 0 {
		return nil, nil
	}

	return parsed, nil
}

// normalisePackIDs is the string form used by the SQL filters.
func normalisePackIDs(packIDs []string) []string {
	out := make([]string, 0, len(packIDs))
	seen := make(map[string]struct{}, len(packIDs))
	for _, id := range packIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
