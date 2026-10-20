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
			"name_embedding": []any{
				0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0,
				1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, // > maxPropertyListItems
			},
		},
	}
	got := nodeProperties(n)

	// uuid/name/summary/group_id/created_at/name_embedding are excluded; nested
	// maps and empty strings are dropped; the rest is sorted by key.
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
		{"oversized vector dropped", []any{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0, 11.0, 12.0, 13.0, 14.0, 15.0, 16.0, 17.0}, "", false},
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

func TestTargetHostsFragmentUsesCanonicalTopologyAndLegacyFallbacks(t *testing.T) {
	t.Parallel()
	// Graphiti 1.1 canonical backbone.
	assert.Contains(t, targetHostsFragment, "(th)-[:HAS_PORT]->(:Port)")
	assert.Contains(t, targetHostsFragment, "(th)-[:HAS_PORT]->(:Port)-[:RUNS_SERVICE]->(:Service)")
	// Graphiti 0.9 fallbacks.
	assert.Contains(t, targetHostsFragment, "(th)-[:RUNS_SERVICE]->(:Service)")
	assert.Contains(t, targetHostsFragment, "HAS_VULNERABILITY*1..4")
	assert.Contains(t, targetHostsFragment, "(th)<-[:ON_HOST]-(:ValidAccess)")
	// The edge-type count heuristic is gone.
	assert.NotContains(t, targetHostsFragment, "$minEdgeTypes")
	assert.NotContains(t, targetHostsFragment, "count(DISTINCT type(tr))")
}

func TestTargetConnectedFilterCoversBothTopologies(t *testing.T) {
	t.Parallel()
	// Variable-depth forward path: covers Host -> Port -> Service -> WebApp ->
	// Endpoint (1.1) and Host -> Service -> WebApp -> Vhost -> Endpoint (0.9).
	assert.Contains(t, targetConnectedFilter, "HAS_PORT|RUNS_SERVICE|HAS_VHOST|HOSTS_APP|HAS_ENDPOINT")
	assert.Contains(t, targetConnectedFilter, "*1..4")
	// Reverse links used by attempts, evidence, valid accesses and services.
	assert.Contains(t, targetConnectedFilter, "ON_HOST")
	assert.Contains(t, targetConnectedFilter, "ATTEMPTED_ON")
	assert.Contains(t, targetConnectedFilter, "EXTRACTED_FROM|ASSERTS")
	// Account layer (Credential/ValidAccess -> Account and MEMBER_OF nesting).
	assert.Contains(t, targetConnectedFilter, "AS_ACCOUNT|BELONGS_TO_ACCOUNT|MEMBER_OF")
}

func TestScopedQueriesDoNotRequireMinEdgeTypes(t *testing.T) {
	t.Parallel()
	queries := map[string]string{
		"attackGraphScopedQuery":      attackGraphScopedQuery,
		"attackGraphScopedCountQuery": attackGraphScopedCountQuery,
		"tagStatsQuery":               tagStatsQuery,
		"attackSurfaceQuery":          attackSurfaceQuery,
		"validAccessesQuery":          validAccessesQuery,
		"infraMapQuery":               infraMapQuery,
		"openPortsQuery":              openPortsQuery,
		"allVulnerabilitiesQuery":     allVulnerabilitiesQuery,
	}
	for name, q := range queries {
		assert.NotContainsf(t, q, "$minEdgeTypes", "%s still requires $minEdgeTypes", name)
	}
}

func TestInfraQueriesUsePortToServiceAndLegacyFallback(t *testing.T) {
	t.Parallel()
	for name, q := range map[string]string{
		"infraMapQuery":  infraMapQuery,
		"openPortsQuery": openPortsQuery,
	} {
		assert.Containsf(t, q, "(p)-[:RUNS_SERVICE]->(s:Service)", "%s uses the canonical Port -> Service edge", name)
		assert.Containsf(t, q, "(h)-[:RUNS_SERVICE]->(s09:Service)", "%s keeps the 0.9 Host -> Service fallback", name)
	}
}

func TestValidAccessesQueryPrefersCanonicalOnHost(t *testing.T) {
	t.Parallel()
	assert.Contains(t, validAccessesQuery, "(va)-[:ON_HOST]->(h:Host)")
	assert.Contains(t, validAccessesQuery, "(s)-[:ON_HOST]->(h09:Host)")
}

func TestCredentialsByCompromisedQueryUsesCanonicalDirections(t *testing.T) {
	t.Parallel()
	assert.Contains(t, credentialsByCompromisedQuery, "(c)-[:YIELDED_ACCESS]->(va:ValidAccess)")
	assert.Contains(t, credentialsByCompromisedQuery, "(c)-[:BELONGS_TO_ACCOUNT]->(ac:Account)<-[:AS_ACCOUNT]-(va:ValidAccess)")
	assert.Contains(t, credentialsByCompromisedQuery, "(c)-[:AUTHENTICATES_TO]->(s:Service)")
}

func TestAttackGraphViewsKeepLegacyVhostAndAddMemberOf(t *testing.T) {
	t.Parallel()
	for _, v := range []AttackGraphView{AttackGraphViewMain, AttackGraphViewAction} {
		spec := attackGraphViewSpecs[v]
		assert.Containsf(t, spec.labels, "Vhost", "view %q keeps the legacy Vhost label", v)
		assert.Containsf(t, spec.labels, "VHost", "view %q keeps the legacy VHost label", v)
	}
	assert.Contains(t, attackGraphViewSpecs[AttackGraphViewAction].edgeTypes, "HAS_VHOST")
	assert.Contains(t, attackGraphViewSpecs[AttackGraphViewPrivesc].edgeTypes, "MEMBER_OF")
	assert.Contains(t, attackSurfaceLabels, "VHost")
}
