package skills

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const (
	// DefaultEngagementLimit is the number of engagement cards returned when the
	// caller does not ask for a specific count.
	DefaultEngagementLimit = 5
	// maxEngagementLimit caps one response.
	maxEngagementLimit = 50
	// engagementCandidateLimit is how many engagements are scored before the top
	// matches are selected.
	engagementCandidateLimit = 200

	// The weights keep a service overlap worth more than a platform or a tag:
	// an engagement that shares two services is usually the best pattern.
	engagementServiceExactWeight = 3.0
	engagementServicePortWeight  = 1.5
	engagementServiceNameWeight  = 1.5
	engagementOSWeight           = 2.0
	engagementPlatformWeight     = 1.0
	engagementTagWeight          = 0.5
)

// EngagementQuery describes the target in front of the agent. Services use the
// "port/service" fingerprint of the mission frontmatter, such as
// "9100/jetdirect"; a bare service name or a bare port is accepted too.
type EngagementQuery struct {
	Services []string
	OS       string
	Platform string
	Tags     []string
	Limit    int
}

// EngagementMatch is one engagement with its match score and the services that
// actually matched. A zero score means the query had no criterion, not a weak
// match: scored searches drop the documents that score nothing.
type EngagementMatch struct {
	Document        Document
	Score           float64
	MatchedServices []string
}

// SearchEngagements lists the engagements of a scope and ranks them against
// the target fingerprint. It reads documents only, so it never re-embeds.
func (s *Store) SearchEngagements(ctx context.Context, userID int64, query EngagementQuery) ([]EngagementMatch, error) {
	if s.db == nil {
		return nil, fmt.Errorf("skills: database is not configured")
	}

	query = normalizeEngagementQuery(query)
	documents, err := s.List(ctx, userID, Filter{
		Kinds:    []string{KindEngagement},
		Statuses: engagementStatuses(),
		Limit:    engagementCandidateLimit,
	})
	if err != nil {
		return nil, err
	}

	return RankEngagements(documents, query), nil
}

// RankEngagements scores and orders engagements for a query. It is pure so the
// scoring can be tested without a database. An empty query keeps the input
// order, which the store already sorts by recency.
func RankEngagements(documents []Document, query EngagementQuery) []EngagementMatch {
	query = normalizeEngagementQuery(query)
	criteria := query.hasCriteria()

	matches := make([]EngagementMatch, 0, len(documents))
	for _, doc := range documents {
		if doc.Kind != KindEngagement || strings.TrimSpace(doc.Slug) == "" {
			continue
		}

		score, matched := ScoreEngagement(doc, query)
		if criteria && score <= 0 {
			continue
		}
		matches = append(matches, EngagementMatch{
			Document:        doc,
			Score:           score,
			MatchedServices: matched,
		})
	}

	if criteria {
		sort.SliceStable(matches, func(i, j int) bool {
			if matches[i].Score != matches[j].Score {
				return matches[i].Score > matches[j].Score
			}
			return matches[i].Document.Slug < matches[j].Document.Slug
		})
	}

	if query.Limit > 0 && len(matches) > query.Limit {
		matches = matches[:query.Limit]
	}

	return matches
}

// ScoreEngagement scores one engagement against a query. Each query service
// contributes its best candidate, so one document cannot gain more than one
// match per queried service. The second return value lists the services that
// matched, for the card.
func ScoreEngagement(doc Document, query EngagementQuery) (float64, []string) {
	var (
		score   float64
		matched []string
	)

	services := doc.MetadataStrings("services")
	for _, wanted := range query.Services {
		wanted = NormalizeService(wanted)
		if wanted == "" {
			continue
		}

		var (
			best      string
			bestScore float64
		)
		for _, candidate := range services {
			candidate = NormalizeService(candidate)
			if candidate == "" {
				continue
			}
			if value := scoreServices(wanted, candidate); value > bestScore {
				best, bestScore = candidate, value
			}
		}
		if bestScore > 0 {
			score += bestScore
			matched = append(matched, best)
		}
	}

	if wanted := strings.ToLower(strings.TrimSpace(query.OS)); wanted != "" &&
		wanted == doc.MetadataString("os") {
		score += engagementOSWeight
	}

	if wanted := strings.ToLower(strings.TrimSpace(query.Platform)); wanted != "" {
		for _, value := range doc.MetadataStrings("platform") {
			if strings.EqualFold(strings.TrimSpace(value), wanted) {
				score += engagementPlatformWeight
				break
			}
		}
	}

	for _, wanted := range query.Tags {
		wanted = strings.ToLower(strings.TrimSpace(wanted))
		if wanted == "" {
			continue
		}
		for _, value := range doc.Tags {
			if strings.ToLower(strings.TrimSpace(value)) == wanted {
				score += engagementTagWeight
				break
			}
		}
	}

	return score, matched
}

// NormalizeService turns a fingerprint into its canonical form: lowercase, no
// spaces, port first. It accepts "HTTPS/443", "443/https" and "https" so a pack
// author and an agent cannot disagree on the spelling.
func NormalizeService(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Join(strings.Fields(value), "")
	if value == "" {
		return ""
	}

	parts := strings.SplitN(value, "/", 2)
	if len(parts) == 1 {
		return parts[0]
	}

	left := strings.Trim(parts[0], "/ ")
	right := strings.Trim(parts[1], "/ ")
	switch {
	case left == "":
		return right
	case right == "":
		return left
	case isPortNumber(left):
		return left + "/" + right
	case isPortNumber(right):
		return right + "/" + left
	default:
		return left + "/" + right
	}
}

// NormalizeServices applies NormalizeService to a list and drops the duplicates.
func NormalizeServices(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := NormalizeService(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func scoreServices(wanted, candidate string) float64 {
	if wanted == "" || candidate == "" {
		return 0
	}
	if wanted == candidate {
		return engagementServiceExactWeight
	}

	wantedPort, wantedName := splitService(wanted)
	candidatePort, candidateName := splitService(candidate)

	var score float64
	if wantedPort != "" && wantedPort == candidatePort {
		score += engagementServicePortWeight
	}
	if wantedName != "" && wantedName == candidateName {
		score += engagementServiceNameWeight
	}

	return score
}

func splitService(value string) (port, name string) {
	parts := strings.SplitN(NormalizeService(value), "/", 2)
	if len(parts) == 1 {
		return "", parts[0]
	}
	return parts[0], parts[1]
}

func isPortNumber(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func normalizeEngagementQuery(query EngagementQuery) EngagementQuery {
	query.Services = NormalizeServices(query.Services)
	query.OS = strings.ToLower(strings.TrimSpace(query.OS))
	query.Platform = strings.ToLower(strings.TrimSpace(query.Platform))

	tags := make([]string, 0, len(query.Tags))
	for _, tag := range query.Tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	query.Tags = tags

	if query.Limit <= 0 {
		query.Limit = DefaultEngagementLimit
	}
	if query.Limit > maxEngagementLimit {
		query.Limit = maxEngagementLimit
	}

	return query
}

func (q EngagementQuery) hasCriteria() bool {
	return len(q.Services) > 0 || q.OS != "" || q.Platform != "" || len(q.Tags) > 0
}

func engagementStatuses() []string {
	return []string{StatusDraft, StatusReviewed, StatusCanonical}
}

// normalizeCVEs upper-cases the identifiers and drops the duplicates, so a
// mission can write "cve-2021-44228" and still be found by the canonical form.
func normalizeCVEs(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	upper := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			upper = append(upper, strings.ToUpper(value))
		}
	}

	return uniqueStrings(upper)
}
