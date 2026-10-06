package kgdashboard

import (
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/assert"
)

func TestFilterLabelsDropsGenericEntity(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"Host"}, filterLabels([]string{"Entity", "Host"}))
	assert.Equal(t, []string{"Host", "Service"}, filterLabels([]string{"Entity", "Host", "Service"}))
	assert.Equal(t, []string{}, filterLabels([]string{"Entity"}))
	assert.Equal(t, []string{}, filterLabels(nil))
}

func TestExtractCVE(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"CVE-2021-44228 in log4j", "CVE-2021-44228"},
		{"found cve-2019-1234 in service", "cve-2019-1234"},
		{"no vuln here", ""},
		{"CVE-2021-44228 and CVE-2014-0160 both", "CVE-2021-44228"},
		{"CVE-2021-44228123 too long", ""}, // 7 digits max
	}
	for _, c := range cases {
		got := extractCVE(c.in)
		assert.Equalf(t, c.want, got, "extractCVE(%q)", c.in)
	}
}

func TestCategorizeVuln(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		row  vulnRow
		want VulnCategory
	}{
		{"cve in name", vulnRow{name: "CVE-2021-44228"}, VulnCategoryCVE},
		{"cve in summary", vulnRow{name: "log4shell", summary: "CVE-2021-44228 RCE"}, VulnCategoryCVE},
		{"critical keyword", vulnRow{name: "remote code execution in handler"}, VulnCategoryCritical},
		{"rce acronym", vulnRow{summary: "achieved RCE via deserialization"}, VulnCategoryCritical},
		{"high sql injection", vulnRow{summary: "SQL injection in login form"}, VulnCategoryHigh},
		{"privilege escalation is high", vulnRow{summary: "privilege escalation via sudo"}, VulnCategoryHigh},
		{"medium xss", vulnRow{summary: "stored XSS in profile name"}, VulnCategoryMedium},
		{"low severity", vulnRow{summary: "low severity info disclosure"}, VulnCategoryLow},
		{"info bucket default", vulnRow{name: "open port 22", summary: "ssh banner"}, VulnCategoryInfo},
	}
	for _, c := range cases {
		got := categorizeVuln(c.row)
		assert.Equalf(t, c.want, got, "categorizeVuln(%+v) name=%q", c.row, c.name)
	}
}

func TestPrimaryLabel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Host", primaryLabel([]string{"Entity", "Host"}))
	assert.Equal(t, "Host", primaryLabel([]string{"Host"}))
	assert.Equal(t, "Entity", primaryLabel([]string{"Entity"}))
	assert.Equal(t, "Entity", primaryLabel(nil))
}

func TestNeo4jNodeUUIDPrefersUUIDProp(t *testing.T) {
	t.Parallel()
	n := neo4j.Node{
		ElementId: "4:abc:0",
		Props:     map[string]any{"uuid": "graphiti-uuid-123"},
	}
	assert.Equal(t, "graphiti-uuid-123", neo4jNodeUUID(n))

	n2 := neo4j.Node{ElementId: "4:abc:1", Props: map[string]any{}}
	assert.Equal(t, "4:abc:1", neo4jNodeUUID(n2))
}

func TestNodeFromNeo4jDropsGenericLabel(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	n := neo4j.Node{
		ElementId: "4:abc:2",
		Labels:    []string{"Entity", "Service"},
		Props: map[string]any{
			"uuid":       "svc-uuid",
			"name":       "ssh",
			"summary":    "ssh on 22",
			"created_at": created,
		},
	}
	got := nodeFromNeo4j(n)
	assert.Equal(t, "svc-uuid", got.UUID)
	assert.Equal(t, []string{"Service"}, got.Labels)
	assert.Equal(t, "ssh", got.Name)
	assert.Equal(t, "ssh on 22", got.Summary)
	assert.Equal(t, created, got.CreatedAt)
}

func TestDedupEdgesByUUID(t *testing.T) {
	t.Parallel()
	in := []AttackGraphEdge{
		{UUID: "e1", Type: "HAS_PORT", SourceUUID: "h1", TargetUUID: "p1"},
		{UUID: "e1", Type: "HAS_PORT", SourceUUID: "h1", TargetUUID: "p1"},
		{UUID: "e2", Type: "RUNS_SERVICE", SourceUUID: "h1", TargetUUID: "s1"},
		{UUID: "", Type: "RELATES_TO", SourceUUID: "a", TargetUUID: "b"}, // empty uuid kept
	}
	out := dedupEdgesByUUID(in)
	assert.Len(t, out, 3)
	assert.Equal(t, "e1", out[0].UUID)
	assert.Equal(t, "e2", out[1].UUID)
	assert.Equal(t, "", out[2].UUID)
}

func TestNodePropertiesExcludesInternalAndSorts(t *testing.T) {
	t.Parallel()
	n := neo4j.Node{
		ElementId: "4:abc:9",
		Props: map[string]any{
			"uuid":        "n-uuid",
			"name":        "/flag",
			"summary":     "POST /flag returned 404",
			"group_id":    "flow-857",
			"created_at":  time.Now(),
			"status_code": int64(404),
			"method":      "POST",
			"path":        "/flag",
			"severity":    "critical",
			"nested":      map[string]any{"a": 1},
			"empty":       "",
		},
	}
	got := nodeProperties(n)

	// uuid/name/summary/group_id/created_at are excluded; nested maps and empty
	// strings are dropped; the rest is sorted by key.
	keys := make([]string, 0, len(got))
	for _, p := range got {
		keys = append(keys, p.Key)
	}
	assert.Equal(t, []string{"method", "path", "severity", "status_code"}, keys)
	assert.Equal(t, "404", got[3].Value)
}

func TestStringifyProp(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		in     any
		want   string
		okWant bool
	}{
		{"string", "critical", "critical", true},
		{"bool", true, "true", true},
		{"int64", int64(404), "404", true},
		{"float", 1.5, "1.5", true},
		{"list of strings", []string{"a", "b"}, "a, b", true},
		{"list of any", []any{"a", int64(2)}, "a, 2", true},
		{"nil", nil, "", false},
		{"map dropped", map[string]any{"a": 1}, "", false},
		{"empty list", []string{}, "", false},
	}
	for _, c := range cases {
		got, ok := stringifyProp(c.in)
		assert.Equalf(t, c.okWant, ok, "%s: ok", c.name)
		assert.Equalf(t, c.want, got, "%s: value", c.name)
	}
}

func TestAttackGraphViewSpecsCoverAllScopedViews(t *testing.T) {
	t.Parallel()
	for _, v := range []AttackGraphView{
		AttackGraphViewMain,
		AttackGraphViewAction,
		AttackGraphViewInfra,
		AttackGraphViewPrivesc,
	} {
		spec, ok := attackGraphViewSpecs[v]
		assert.Truef(t, ok, "view %q has a spec", v)
		assert.NotEmptyf(t, spec.labels, "view %q has labels", v)
		assert.Positivef(t, spec.capNodes, "view %q has a positive cap", v)
	}
	// MAIN keeps every edge between its nodes (empty allowlist) so early flows
	// stay connected; the other scoped views filter by relationship type.
	assert.Empty(t, attackGraphViewSpecs[AttackGraphViewMain].edgeTypes)
	assert.NotEmpty(t, attackGraphViewSpecs[AttackGraphViewInfra].edgeTypes)
}
