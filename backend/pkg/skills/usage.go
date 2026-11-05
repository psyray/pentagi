package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Event names recorded in knowledge_events for retrieval analysis.
const (
	EventSearched = "searched"
	EventRead     = "read"
)

// Event is one audit row. DocumentID may be empty for a search that spans
// several documents; Details carries the hits and their ranks.
type Event struct {
	DocumentID string
	FlowID     *int64
	TaskID     *int64
	SubtaskID  *int64
	Name       string
	Details    map[string]any
}

// RecordEvent appends one row to knowledge_events. It never fails the caller's
// tool path: a metrics write is best effort.
func (s *Store) RecordEvent(ctx context.Context, userID int64, event Event) error {
	if s.db == nil {
		return fmt.Errorf("skills: database is not configured")
	}
	if event.Name == "" {
		return fmt.Errorf("%w: event name is required", ErrInvalidInput)
	}

	details := event.Details
	if details == nil {
		details = map[string]any{}
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("skills: marshal event details: %w", err)
	}

	var documentID any
	if event.DocumentID != "" {
		documentID = event.DocumentID
	}

	if _, err := s.db.Exec(ctx, `
INSERT INTO knowledge_events (document_id, flow_id, task_id, subtask_id, actor_user_id, event, details)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::jsonb)`,
		documentID, event.FlowID, event.TaskID, event.SubtaskID, userID, event.Name, string(payload),
	); err != nil {
		return fmt.Errorf("skills: record event %q: %w", event.Name, err)
	}

	return nil
}

// SkillUsage is the retrieval usage of one document in the window.
type SkillUsage struct {
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	Kind        string     `json:"kind"`
	Status      string     `json:"status"`
	Suggestions int        `json:"suggestions"`
	Reads       int        `json:"reads"`
	LastSeen    *time.Time `json:"lastSeen,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// UsageReport aggregates the retrieval events of one scope. Success per skill
// (flags, shells) is not computed here: it needs the benchmark harness.
type UsageReport struct {
	Days                int          `json:"days"`
	Searches            int          `json:"searches"`
	EmptySearches       int          `json:"emptySearches"`
	EmptyRate           float64      `json:"emptyRate"`
	Suggestions         int          `json:"suggestions"`
	Reads               int          `json:"reads"`
	ReadAfterSuggestion float64      `json:"readAfterSuggestion"`
	Skills              []SkillUsage `json:"skills"`
	Dead                []SkillUsage `json:"dead"`
}

// UsageReport computes the retrieval usage of the documents visible to a user
// over the last days. Dead lists the documents with no suggestion and no read
// in the window.
func (s *Store) UsageReport(ctx context.Context, userID int64, days int) (*UsageReport, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}
	if days <= 0 {
		days = 30
	}

	report := &UsageReport{Days: days}

	var readsAfter int
	if err := s.db.QueryRow(ctx, `
WITH recent AS (
  SELECT id, created_at, event, details
  FROM knowledge_events
  WHERE created_at >= now() - make_interval(days => $1)
    AND event IN ($2, $3)
)
SELECT
  count(*) FILTER (WHERE event = $2) AS searches,
  count(*) FILTER (WHERE event = $2 AND COALESCE((details->>'hit_count')::int, 0) = 0) AS empty,
  (SELECT count(*) FROM recent w, jsonb_array_elements(COALESCE(w.details->'hits', '[]'::jsonb)) h
   WHERE w.event = $2) AS suggestions,
  count(*) FILTER (WHERE event = $3) AS reads,
  (SELECT count(*) FROM recent r
   WHERE r.event = $3 AND r.details ? 'slug'
     AND EXISTS (
       SELECT 1 FROM recent s, jsonb_array_elements(COALESCE(s.details->'hits', '[]'::jsonb)) h
       WHERE s.event = $2 AND h->>'slug' = r.details->>'slug' AND s.created_at <= r.created_at
     )) AS reads_after
FROM recent`, days, EventSearched, EventRead).Scan(
		&report.Searches, &report.EmptySearches, &report.Suggestions, &report.Reads, &readsAfter,
	); err != nil {
		return nil, fmt.Errorf("skills: usage summary: %w", err)
	}

	if report.Searches > 0 {
		report.EmptyRate = float64(report.EmptySearches) / float64(report.Searches)
	}
	if report.Reads > 0 {
		report.ReadAfterSuggestion = float64(readsAfter) / float64(report.Reads)
	}

	rows, err := s.db.Query(ctx, `
WITH recent AS (
  SELECT created_at, event, details
  FROM knowledge_events
  WHERE created_at >= now() - make_interval(days => $1)
    AND event IN ($2, $3)
),
hits AS (
  SELECT w.created_at, h->>'slug' AS slug
  FROM recent w, jsonb_array_elements(COALESCE(w.details->'hits', '[]'::jsonb)) h
  WHERE w.event = $2
),
suggestions AS (
  SELECT slug, count(*) AS n, max(created_at) AS last_seen FROM hits GROUP BY slug
),
reads AS (
  SELECT details->>'slug' AS slug, count(*) AS n, max(created_at) AS last_seen
  FROM recent WHERE event = $3 AND details ? 'slug' GROUP BY 1
)
SELECT d.slug, d.title, d.kind, d.status,
       COALESCE(s.n, 0), COALESCE(r.n, 0),
       GREATEST(s.last_seen, r.last_seen) AS last_seen,
       d.updated_at
FROM knowledge_documents d
LEFT JOIN suggestions s ON s.slug = d.slug
LEFT JOIN reads r ON r.slug = d.slug
WHERE d.deleted_at IS NULL
  AND (d.user_id = $4 OR (d.visibility = 'shared' AND d.status IN ('reviewed','canonical')))
ORDER BY COALESCE(s.n, 0) + COALESCE(r.n, 0) DESC, d.slug`, days, EventSearched, EventRead, userID)
	if err != nil {
		return nil, fmt.Errorf("skills: usage by document: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			usage    SkillUsage
			lastSeen *time.Time
		)
		if err := rows.Scan(&usage.Slug, &usage.Title, &usage.Kind, &usage.Status,
			&usage.Suggestions, &usage.Reads, &lastSeen, &usage.UpdatedAt); err != nil {
			return nil, fmt.Errorf("skills: scan usage row: %w", err)
		}
		usage.LastSeen = lastSeen
		report.Skills = append(report.Skills, usage)
		if usage.Suggestions == 0 && usage.Reads == 0 {
			report.Dead = append(report.Dead, usage)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("skills: iterate usage rows: %w", err)
	}

	return report, nil
}

// StaleDocuments returns the reviewed or canonical documents that have had no
// suggestion and no read for the given window and that have not been updated for
// at least that long. They are candidates for deprecation.
func StaleDocuments(report *UsageReport) []SkillUsage {
	if report == nil {
		return nil
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -report.Days)
	stale := make([]SkillUsage, 0)
	for _, usage := range report.Dead {
		if usage.Status != StatusReviewed && usage.Status != StatusCanonical {
			continue
		}
		if usage.UpdatedAt.After(cutoff) {
			continue
		}
		stale = append(stale, usage)
	}

	return stale
}
