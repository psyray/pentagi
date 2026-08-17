// Package kgdashboard exposes a read-only view of the Graphiti knowledge graph
// (Neo4j) for a given PentAGI flow, powering the in-flow dashboard: attack-chain
// graph, attack surface overview, credentials status, valid accesses,
// infrastructure map, open ports, vulnerability breakdown, detected CVEs, tool
// usage and produced artifacts.
//
// The service is intentionally narrow: it only ever reads from Neo4j, scopes
// every Cypher query by the flow's group_id (cfg.GroupID(flowID)), and returns
// typed Go structs ready for GraphQL resolvers. It is disabled when Graphiti
// is not enabled or Neo4j is unreachable; callers must tolerate empty results
// in that case (the resolvers do).
package kgdashboard

import "time"

// AttackGraphView selects which slice of the knowledge graph to materialize.
type AttackGraphView string

const (
	// AttackGraphViewMain restricts the graph to the attack-chain backbone:
	// Host → Port → Service → Vulnerability / Misconfiguration → ValidAccess
	// → Account, plus the supporting Credential / Attempt / PrivChange nodes.
	AttackGraphViewMain AttackGraphView = "MAIN"
	// AttackGraphViewFull materializes every entity and edge of the flow,
	// subject to a configurable node cap (default 1500). Above the cap, nodes
	// are sampled by recency.
	AttackGraphViewFull AttackGraphView = "FULL"
)

// AttackGraphNode is a Neo4j entity projected for graph rendering.
type AttackGraphNode struct {
	UUID      string    `json:"uuid"`
	Labels    []string  `json:"labels"`
	Name      string    `json:"name"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"createdAt"`
}

// AttackGraphEdge is a Neo4j relationship projected for graph rendering.
type AttackGraphEdge struct {
	UUID       string    `json:"uuid"`
	Type       string    `json:"type"`
	Fact       string    `json:"fact"`
	SourceUUID string    `json:"sourceUUID"`
	TargetUUID string    `json:"targetUUID"`
	CreatedAt  time.Time `json:"createdAt"`
}

// AttackGraph is the materialized subgraph for a flow dashboard view.
type AttackGraph struct {
	Nodes      []AttackGraphNode `json:"nodes"`
	Edges      []AttackGraphEdge `json:"edges"`
	TotalNodes int               `json:"totalNodes"`
	TotalEdges int               `json:"totalEdges"`
	Truncated  bool              `json:"truncated"`
}

// GraphitiTagStat counts entities of a given taxonomy tag for the flow.
type GraphitiTagStat struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// AttackSurfaceItem is one row of the attack surface overview (one entity
// belonging to one of the watched tags: Tool, ToolExecution, Service,
// Vulnerability, Endpoint, Agent, Account, Credential, Artifact, Port,
// Attempt, Host, Misconfiguration, ValidAccess, WebApp, Subtask, Vhost,
// PrivChange, ...).
type AttackSurfaceItem struct {
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"createdAt"`
}

// CredentialStatus is the inferred status of a discovered credential.
type CredentialStatus string

const (
	// CredentialStatusCompromised: the credential led to a ValidAccess / Account
	// (via YIELDED_ACCESS, AS_ACCOUNT, AUTHENTICATES_TO, BELONGS_TO_ACCOUNT).
	CredentialStatusCompromised CredentialStatus = "Compromised"
	// CredentialStatusDiscovered: the credential is present in the graph but
	// did not yield access.
	CredentialStatusDiscovered CredentialStatus = "Discovered"
	// CredentialStatusUnknown is used when the graph is missing the supporting
	// edges to decide.
	CredentialStatusUnknown CredentialStatus = "Unknown"
)

// CredentialStatusRow aggregates credentials by inferred status.
type CredentialStatusRow struct {
	Status   CredentialStatus `json:"status"`
	Count    int              `json:"count"`
	Examples []string         `json:"examples"`
}

// ValidAccessRow is one validated access path discovered in the flow.
type ValidAccessRow struct {
	Access  string `json:"access"`
	Account string `json:"account"`
	Host    string `json:"host"`
	Service string `json:"service"`
	Summary string `json:"summary"`
}

// InfraMapRow is one Host → Port → Service row of the infrastructure map.
type InfraMapRow struct {
	Host    string `json:"host"`
	Port    string `json:"port"`
	Service string `json:"service"`
}

// OpenPortRow is one port row of the open ports view.
type OpenPortRow struct {
	Port    string `json:"port"`
	Service string `json:"service"`
	Host    string `json:"host"`
}

// VulnCategory buckets vulnerabilities for the breakdown view.
type VulnCategory string

const (
	VulnCategoryCVE      VulnCategory = "CVE"
	VulnCategoryCritical VulnCategory = "Critical"
	VulnCategoryHigh     VulnCategory = "High"
	VulnCategoryMedium   VulnCategory = "Medium"
	VulnCategoryLow      VulnCategory = "Low"
	VulnCategoryInfo     VulnCategory = "Info"
)

// VulnBreakdownRow aggregates vulnerabilities by category.
type VulnBreakdownRow struct {
	Category VulnCategory `json:"category"`
	Count    int          `json:"count"`
	Examples []string     `json:"examples"`
}

// DetectedCVERow is one CVE detected during the flow.
type DetectedCVERow struct {
	CVE     string `json:"cve"`
	FoundOn string `json:"foundOn"`
	Source  string `json:"source"`
}

// GraphitiToolUsageRow counts tool executions per tool name, as observed by
// Graphiti (episodes named tool_execution_<tool>).
type GraphitiToolUsageRow struct {
	Tool       string `json:"tool"`
	Executions int    `json:"executions"`
}

// ArtifactRow is one artifact produced during the flow.
type ArtifactRow struct {
	Artifact   string `json:"artifact"`
	ProducedBy string `json:"producedBy"`
	Summary    string `json:"summary"`
}
