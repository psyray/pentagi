package kgdashboard

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// nodeFromNeo4j projects a Neo4j node into an AttackGraphNode. Graphiti
// stores the entity UUID under the `uuid` property; we fall back to the
// internal element id when absent (rare, only for legacy data).
func nodeFromNeo4j(n neo4j.Node) AttackGraphNode {
	uuid := neo4jNodeUUID(n)
	var name, summary string
	if v, ok := n.Props["name"]; ok {
		if s, ok := v.(string); ok {
			name = s
		}
	}
	if v, ok := n.Props["summary"]; ok {
		if s, ok := v.(string); ok {
			summary = s
		}
	}
	created := neo4jPropTime(n.Props, "created_at", "valid_at")
	return AttackGraphNode{
		UUID:      uuid,
		Labels:    filterLabels(n.Labels),
		Name:      name,
		Summary:   summary,
		CreatedAt: created,
	}
}

// neo4jNodeUUID returns the Graphiti entity UUID (Props["uuid"]) or the
// Neo4j element id as a stable fallback.
func neo4jNodeUUID(n neo4j.Node) string {
	if v, ok := n.Props["uuid"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return n.ElementId
}

// filterLabels drops the generic `Entity` label that Graphiti attaches to
// every entity; it carries no type information for the dashboard.
func filterLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if l == "Entity" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// neo4jPropTime extracts a time.Time from the first present property name.
func neo4jPropTime(props map[string]any, keys ...string) time.Time {
	for _, k := range keys {
		v, ok := props[k]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case time.Time:
			if !t.IsZero() {
				return t
			}
		case neo4j.Time:
			if ts := t.Time(); !ts.IsZero() {
				return ts
			}
		}
	}
	return time.Time{}
}

func dedupEdgesByUUID(edges []AttackGraphEdge) []AttackGraphEdge {
	seen := make(map[string]struct{}, len(edges))
	out := edges[:0]
	for _, e := range edges {
		if e.UUID == "" {
			out = append(out, e)
			continue
		}
		if _, ok := seen[e.UUID]; ok {
			continue
		}
		seen[e.UUID] = struct{}{}
		out = append(out, e)
	}
	return out
}

// recStr safely extracts a string column from a Neo4j record.
func recStr(rec *neo4j.Record, key string) string {
	v, ok := rec.Get(key)
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// recInt extracts an int64 column (Neo4j integers are int64).
func recInt(rec *neo4j.Record, key string) int64 {
	v, ok := rec.Get(key)
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

// recTime extracts a time column (Neo4j returns time.Time for datetime props).
func recTime(rec *neo4j.Record, key string) time.Time {
	v, ok := rec.Get(key)
	if !ok || v == nil {
		return time.Time{}
	}
	switch t := v.(type) {
	case time.Time:
		return t
	case neo4j.Time:
		return t.Time()
	}
	return time.Time{}
}

// recLabels extracts a string slice column.
func recLabels(rec *neo4j.Record, key string) []string {
	v, ok := rec.Get(key)
	if !ok || v == nil {
		return nil
	}
	if s, ok := v.([]string); ok {
		return filterLabels(s)
	}
	if any, ok := v.([]any); ok {
		out := make([]string, 0, len(any))
		for _, a := range any {
			if s, ok := a.(string); ok {
				out = append(out, s)
			}
		}
		return filterLabels(out)
	}
	return nil
}

// nodePropertyExclusions are internal Graphiti/Neo4j properties that are not
// useful in the node detail panel: they duplicate the projected fields
// (uuid/name/summary) or leak storage internals (group_id/episode/dates).
var nodePropertyExclusions = map[string]struct{}{
	"uuid":       {},
	"name":       {},
	"summary":    {},
	"group_id":   {},
	"created_at": {},
	"valid_at":   {},
	"episode":    {},
	"labels":     {},
}

// nodeProperties projects the remaining node attributes into sorted, stringified
// key/value pairs for the detail panel. Non-scalar values that cannot be
// rendered are dropped rather than serialized as "map[...]".
func nodeProperties(n neo4j.Node) []GraphitiProperty {
	keys := make([]string, 0, len(n.Props))
	for k := range n.Props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]GraphitiProperty, 0, len(keys))
	for _, k := range keys {
		if _, skip := nodePropertyExclusions[k]; skip {
			continue
		}
		if s, ok := stringifyProp(n.Props[k]); ok && s != "" {
			out = append(out, GraphitiProperty{Key: k, Value: s})
		}
	}
	return out
}

// stringifyProp renders a Graphiti attribute value as a display string. Scalars
// are formatted directly; slices of scalars are joined with ", ". Anything else
// (nested maps, relationships) is dropped.
func stringifyProp(v any) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", false
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64), true
	case time.Time:
		return t.Format(time.RFC3339), true
	case neo4j.Time:
		return t.Time().Format(time.RFC3339), true
	case []string:
		return strings.Join(t, ", "), len(t) > 0
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := stringifyProp(item); ok && s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", "), len(parts) > 0
	default:
		return "", false
	}
}
