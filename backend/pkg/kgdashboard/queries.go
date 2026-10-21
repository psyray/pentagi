package kgdashboard

// All Cypher in this file is READ-ONLY (MATCH/OPTIONAL MATCH/RETURN only) and
// parameterized by $group_id. The service never builds group_id by string
// concatenation: it always comes from cfg.GroupID(flowID) on the caller side
// and is passed as a query parameter. Neo4j enforces parameter isolation.
//
// The taxonomy (entity labels + relation types) is the one Graphiti produces
// for PentAGI; see pentagi/graphiti/graphiti-models-benchmark.md. Label values
// are PascalCase (Host, Service, ValidAccess, ...); Graphiti stores every
// entity with both the generic `Entity` label and its specific label, so we
// match on the specific label and project labels(n) as the node's type set.

// viewSpec describes one attack-graph view: the entity label set materialized,
// the relationship type allowlist kept between those entities, and the node cap.
// An empty edgeTypes list means "keep every edge between selected nodes" (used
// by MAIN to preserve connectivity in early flows); a non-empty list restricts
// the graph to the semantically relevant relations to cut visual noise.
type viewSpec struct {
	labels    []string
	edgeTypes []string
	capNodes  int
}

// attackGraphViewSpecs is the per-view configuration of the Analytics tab.
//
// MAIN is the attack-chain backbone only (EE "Main Attack Chain"). ACTION adds
// the action-level entities (EE "Action Chain"). INFRA is the infrastructure
// map (Host → Port → Service). PRIVESC is the privilege-escalation chain.
//
// Graphiti 1.1 removed the VHost node and the HAS_VHOST edge. Vhost/VHost and
// HAS_VHOST are kept below as legacy support for graphs ingested with 0.9;
// they are harmless for 1.1 graphs.
var attackGraphViewSpecs = map[AttackGraphView]viewSpec{
	AttackGraphViewMain: {
		labels:   []string{"Host", "Port", "Service", "WebApp", "Vhost", "VHost", "Endpoint", "Vulnerability"},
		capNodes: 200,
	},
	AttackGraphViewAction: {
		labels: []string{
			"Host", "Port", "Service", "WebApp", "Vhost", "VHost", "Endpoint", "Vulnerability",
			"Misconfiguration", "Attempt", "Artifact", "Evidence", "AttackTechnique",
			"Tool", "ToolExecution",
		},
		edgeTypes: []string{
			"HAS_PORT", "RUNS_SERVICE", "HOSTS_APP", "HAS_VHOST", "HAS_ENDPOINT", "ON_HOST",
			"DETECTED_VULNERABILITY", "CONFIRMED_VULNERABILITY", "HAS_VULNERABILITY", "HAS_MISCONFIGURATION",
			"ATTEMPTED_ON", "RESULTED_IN", "PRODUCED", "REVEALED", "SUPPORTS", "RELATES_TO",
		},
		capNodes: 400,
	},
	AttackGraphViewInfra: {
		labels:    []string{"Host", "Port", "Service"},
		edgeTypes: []string{"HAS_PORT", "RUNS_SERVICE", "ON_HOST"},
		capNodes:  200,
	},
	AttackGraphViewPrivesc: {
		labels: []string{"Host", "Service", "Account", "Credential", "ValidAccess", "PrivChange"},
		edgeTypes: []string{
			"YIELDED_ACCESS", "YIELDED_PRIV_ACCESS", "ESCALATED_VIA", "AUTHENTICATES_TO",
			"AS_ACCOUNT", "BELONGS_TO_ACCOUNT", "MEMBER_OF", "VIA_SERVICE", "ON_HOST",
		},
		capNodes: 200,
	},
}

// attackSurfaceLabels is the entity label set watched by the attack surface
// overview block. Slimmed to attack-chain entities only — no Artifact,
// Evidence, Agent, Attempt, Capability (noise). Misconfiguration and Endpoint
// are kept here because the attack surface overview is broader than the MAIN
// graph view.
var attackSurfaceLabels = []string{
	"Host", "Port", "Service", "Vulnerability", "Misconfiguration",
	"ValidAccess", "Account", "Credential", "Endpoint", "WebApp", "Vhost", "VHost",
	"PrivChange",
}

// targetHostsFragment is a reusable Cypher preamble that identifies "target"
// Host UUIDs and exposes them as `targetUUIDs`. A target Host exposes a Port
// and at least one other attack-chain signal (Service, Vulnerability,
// Misconfiguration, ValidAccess, Attempt or Evidence). Localhost/VPN/container
// noise hosts usually expose only a Port (from `ip a` output), so requiring a
// second signal filters them out.
//
// The fragment is topology-version agnostic:
//   - Graphiti 1.1 canonical shape: Host -HAS_PORT-> Port -RUNS_SERVICE->
//     Service, then HOSTS_APP -> WebApp -> HAS_ENDPOINT -> Endpoint.
//   - Graphiti 0.9 legacy shape: Host -RUNS_SERVICE-> Service and
//     Host -HAS_VULNERABILITY-> Vulnerability are still accepted.
//
// It only needs $group_id; the former $minEdgeTypes heuristic is gone.
const targetHostsFragment = `
MATCH (th:Host) WHERE th.group_id = $group_id
WITH th,
  EXISTS { MATCH (th)-[:HAS_PORT]->(:Port) } AS c_port,
  EXISTS { MATCH (th)-[:HAS_PORT]->(:Port)-[:RUNS_SERVICE]->(:Service) } AS c_service_11,
  EXISTS { MATCH (th)-[:RUNS_SERVICE]->(:Service) } AS c_service_09,
  EXISTS { MATCH (th)-[:HAS_PORT|RUNS_SERVICE|HOSTS_APP|HAS_ENDPOINT|HAS_VHOST|DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY|HAS_VULNERABILITY*1..4]->(:Vulnerability) } AS c_vuln,
  EXISTS { MATCH (th)-[:HAS_MISCONFIGURATION]->(:Misconfiguration) } AS c_misconf,
  EXISTS { MATCH (th)<-[:ON_HOST]-(:ValidAccess) } AS c_access,
  EXISTS { MATCH (th)<-[:ON_HOST]-(:Service) } AS c_access_09,
  EXISTS { MATCH (th)<-[:ATTEMPTED_ON]-(:Attempt) } AS c_attempt,
  EXISTS { MATCH (th)<-[:ASSERTS|EXTRACTED_FROM]-(:Evidence) } AS c_evidence
WHERE c_port AND (c_service_11 OR c_service_09 OR c_vuln OR c_misconf OR c_access OR c_access_09 OR c_attempt OR c_evidence)
WITH collect(th.uuid) AS targetUUIDs
`

// targetConnectedFilter is a reusable Cypher WHERE clause fragment that keeps
// only nodes connected to a target Host. The forward structural path uses a
// variable depth (1..4) so it covers both the Graphiti 1.1 canonical chain
// (Host -> Port -> Service -> WebApp -> Endpoint, with Vulnerabilities and
// Misconfigurations hanging off it) and the 0.9 chain (Host -> Service ->
// WebApp -> Vhost -> Endpoint). The reverse branches keep entities that point
// at the target (ValidAccess/Service ON_HOST, Attempt ATTEMPTED_ON, Evidence
// EXTRACTED_FROM/ASSERTS) and the account layer (Credential/ValidAccess ->
// Account, plus MEMBER_OF nesting).
const targetConnectedFilter = `
  AND (
    n.uuid IN targetUUIDs
    OR EXISTS {
      MATCH (th)-[:HAS_PORT|RUNS_SERVICE|HAS_VHOST|HOSTS_APP|HAS_ENDPOINT|DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY|HAS_VULNERABILITY|HAS_MISCONFIGURATION*1..4]->(n)
      WHERE th.uuid IN targetUUIDs
    }
    OR EXISTS {
      MATCH (n)-[:ON_HOST|ATTEMPTED_ON|TOOL_DISCOVERED|TOOL_TARGETED|AGENT_ANALYZED|EXTRACTED_FROM|ASSERTS]->(th)
      WHERE th.uuid IN targetUUIDs
    }
    OR EXISTS {
      MATCH (th)-[:HAS_PORT|RUNS_SERVICE|HAS_VHOST|HOSTS_APP|HAS_ENDPOINT*1..4]->(mid)
            <-[:AUTHENTICATES_TO|VIA_SERVICE|YIELDED_ACCESS|YIELDED_PRIV_ACCESS|ESCALATED_VIA]-(n)
      WHERE th.uuid IN targetUUIDs
    }
    OR EXISTS {
      MATCH (th)-[:HAS_PORT|RUNS_SERVICE|HAS_VHOST|HOSTS_APP|HAS_ENDPOINT*1..4]->(mid)
            <-[:AUTHENTICATES_TO|VIA_SERVICE|YIELDED_ACCESS|YIELDED_PRIV_ACCESS|ESCALATED_VIA]-(linked)
            -[:AS_ACCOUNT|BELONGS_TO_ACCOUNT|MEMBER_OF*1..3]->(n)
      WHERE th.uuid IN targetUUIDs
    }
  )
`

// ─── Attack graph queries ───────────────────────────────────────────────

// attackGraphScopedQuery returns the subgraph of a scoped view: nodes whose
// labels are in $labels and that are connected to target hosts (structure
// path up to 4 hops, or directly pointing at the target). This filters out
// orphan noise like "read_file command" (a tool name misclassified as Port by
// Graphiti) while keeping Vulnerabilities and Credentials connected through
// Services.
const attackGraphScopedQuery = targetHostsFragment + `
MATCH (n) WHERE n.group_id = $group_id
  AND ANY(l IN labels(n) WHERE l IN $labels)` + targetConnectedFilter + `
RETURN n AS n, coalesce(n.created_at, n.valid_at) AS ts
ORDER BY ts DESC
LIMIT $cap
`

// attackGraphScopedEdgesQuery returns the edges of a scoped view. All edges
// between two selected nodes are kept when $edgeTypes is empty (MAIN, so the
// graph stays connected even in early flows); otherwise only edges whose type
// is in the allowlist are kept (ACTION, INFRA, PRIVESC), which cuts visual
// noise without dropping the backbone.
const attackGraphScopedEdgesQuery = `
MATCH (src)-[r]->(tgt)
WHERE src.group_id = $group_id AND tgt.group_id = $group_id AND r.group_id = $group_id
  AND ANY(l IN labels(src) WHERE l IN $labels)
  AND ANY(l IN labels(tgt) WHERE l IN $labels)
  AND (size($edgeTypes) = 0 OR type(r) IN $edgeTypes)
RETURN coalesce(src.uuid, src.elementId) AS srcUUID,
       coalesce(tgt.uuid, tgt.elementId) AS tgtUUID,
       r AS edge
`

// attackGraphScopedCountQuery returns the total node count for a scoped view.
const attackGraphScopedCountQuery = targetHostsFragment + `
MATCH (n) WHERE n.group_id = $group_id
  AND ANY(l IN labels(n) WHERE l IN $labels)` + targetConnectedFilter + `
RETURN count(n) AS total
`

// nodePropertiesQuery returns a single node by Graphiti UUID (or Neo4j element
// id as fallback) so the node detail panel can render its attributes.
const nodePropertiesQuery = `
MATCH (n)
WHERE n.group_id = $group_id AND (n.uuid = $uuid OR elementId(n) = $uuid)
RETURN n AS n
LIMIT 1
`

// attackGraphFullQuery returns every flow node (capped at $cap, newest first).
const attackGraphFullQuery = `
MATCH (n) WHERE n.group_id = $group_id
RETURN n AS n, coalesce(n.created_at, n.valid_at) AS ts
ORDER BY ts DESC
LIMIT $cap
`

// attackGraphFullEdgesQuery returns every flow edge with source/target UUIDs.
const attackGraphFullEdgesQuery = `
MATCH (src)-[r]->(tgt)
WHERE src.group_id = $group_id AND tgt.group_id = $group_id AND r.group_id = $group_id
RETURN coalesce(src.uuid, src.elementId) AS srcUUID,
       coalesce(tgt.uuid, tgt.elementId) AS tgtUUID,
       r AS edge
`

// attackGraphFullCountQuery returns the total node count for the FULL view.
const attackGraphFullCountQuery = `
MATCH (n) WHERE n.group_id = $group_id
RETURN count(n) AS total
`

// ─── Tag stats ───────────────────────────────────────────────────────────

// tagStatsQuery counts entities per specific label, excluding the generic
// `Entity` and `Episodic` labels. Only entities connected to target hosts
// (depth 1 or 2) are counted — this ensures the tag stats reflect the attack
// surface of the pentest target(s), not infrastructure noise.
const tagStatsQuery = targetHostsFragment + `
MATCH (n) WHERE n.group_id = $group_id` + targetConnectedFilter + `
UNWIND [l IN labels(n) WHERE NOT l IN ['Entity', 'Episodic']] AS label
RETURN label AS tag, count(*) AS count
ORDER BY count DESC, label ASC
`

// ─── Attack surface ──────────────────────────────────────────────────────

// attackSurfaceQuery lists attack-chain entities connected to target hosts,
// newest first, capped by $limit.
const attackSurfaceQuery = targetHostsFragment + `
MATCH (n) WHERE n.group_id = $group_id
  AND ANY(l IN labels(n) WHERE l IN $labels)` + targetConnectedFilter + `
RETURN labels(n) AS labels, n.name AS name, n.summary AS summary,
       coalesce(n.created_at, n.valid_at) AS createdAt
ORDER BY createdAt DESC
LIMIT $limit
`

// ─── Credentials ────────────────────────────────────────────────────────

// credentialsByCompromisedQuery returns credential UUIDs that led to a
// ValidAccess or Account (heuristic for the "Compromised" status). Graphiti
// 1.1 canonical directions are Credential -AUTHENTICATES_TO-> Service,
// Credential -BELONGS_TO_ACCOUNT-> Account, Credential -YIELDED_ACCESS->
// ValidAccess and ValidAccess -AS_ACCOUNT-> Account. The latter also matches
// the 0.9 graphs (the original query already used that direction).
const credentialsByCompromisedQuery = `
MATCH (c:Credential) WHERE c.group_id = $group_id
  AND (
    EXISTS { MATCH (c)-[:AUTHENTICATES_TO]->(s:Service) WHERE s.group_id = $group_id }
    OR EXISTS { MATCH (c)-[:BELONGS_TO_ACCOUNT]->(a:Account) WHERE a.group_id = $group_id }
    OR EXISTS { MATCH (c)-[:YIELDED_ACCESS]->(va:ValidAccess) WHERE va.group_id = $group_id }
    OR EXISTS {
      MATCH (c)-[:BELONGS_TO_ACCOUNT]->(ac:Account)<-[:AS_ACCOUNT]-(va:ValidAccess)
      WHERE va.group_id = $group_id
    }
  )
RETURN c.uuid AS uuid, c.name AS name, c.summary AS summary
`

// allCredentialsQuery returns every credential of the flow.
const allCredentialsQuery = `
MATCH (c:Credential) WHERE c.group_id = $group_id
RETURN c.uuid AS uuid, c.name AS name, c.summary AS summary
ORDER BY c.created_at DESC
`

// ─── Valid accesses ──────────────────────────────────────────────────────

// validAccessesQuery returns each ValidAccess with its reachable Account,
// backing Service and the Host it applies to. Graphiti 1.1 links the
// ValidAccess straight to the Host (ValidAccess -ON_HOST-> Host); the 0.9
// shape went through the Service (Service -ON_HOST-> Host). Both are kept,
// canonical first, and the Host is restricted to target hosts so noise hosts
// (localhost, VPN) don't appear.
const validAccessesQuery = targetHostsFragment + `
MATCH (va:ValidAccess) WHERE va.group_id = $group_id
OPTIONAL MATCH (va)-[:AS_ACCOUNT]->(a:Account)
OPTIONAL MATCH (va)-[:VIA_SERVICE]->(s:Service)
OPTIONAL MATCH (va)-[:ON_HOST]->(h:Host) WHERE h.uuid IN targetUUIDs
OPTIONAL MATCH (s)-[:ON_HOST]->(h09:Host) WHERE h09.uuid IN targetUUIDs
WITH va, a, s, coalesce(h, h09) AS host
RETURN va.summary AS access, a.name AS account, host.name AS host, s.name AS service, va.summary AS summary
ORDER BY va.created_at DESC
`

// ─── Infrastructure map ──────────────────────────────────────────────────

// infraMapQuery returns Host -> Port -> Service rows, filtered to target hosts
// only so localhost, VPN, and container IPs don't clutter the map. Graphiti 1.1
// hangs the Service off the Port (Port -RUNS_SERVICE-> Service); the 0.9
// Host -RUNS_SERVICE-> Service edge is kept as a fallback.
const infraMapQuery = targetHostsFragment + `
MATCH (h:Host)-[:HAS_PORT]->(p:Port)
WHERE h.group_id = $group_id AND p.group_id = $group_id AND h.uuid IN targetUUIDs
OPTIONAL MATCH (p)-[:RUNS_SERVICE]->(s:Service)
OPTIONAL MATCH (h)-[:RUNS_SERVICE]->(s09:Service)
WITH p, h, coalesce(s.name, s09.name) AS serviceName
WITH DISTINCT p, h, serviceName
RETURN h.name AS host, p.name AS port, serviceName AS service
ORDER BY host, toInteger(p.name)
`

// ─── Open ports ─────────────────────────────────────────────────────────

// openPortsQuery returns one row per discovered port on a target host, with
// the Service attached to that port (1.1) or to the host (0.9 fallback).
const openPortsQuery = targetHostsFragment + `
MATCH (p:Port)<-[:HAS_PORT]-(h:Host)
WHERE p.group_id = $group_id AND h.group_id = $group_id AND h.uuid IN targetUUIDs
OPTIONAL MATCH (p)-[:RUNS_SERVICE]->(s:Service)
OPTIONAL MATCH (h)-[:RUNS_SERVICE]->(s09:Service)
WITH p, h, coalesce(s.name, s09.name) AS serviceName
WITH DISTINCT p, h, serviceName
RETURN p.name AS port, serviceName AS service, h.name AS host
ORDER BY toInteger(p.name), host
`

// ─── Vulnerabilities ────────────────────────────────────────────────────

// allVulnerabilitiesQuery returns every Vulnerability node connected to a
// target host. The structural path is version-agnostic: 1.1 hangs the
// Vulnerability off a Service/WebApp/Endpoint, 0.9 could attach it to the Host
// directly (HAS_VULNERABILITY) or through Host -RUNS_SERVICE-> Service.
const allVulnerabilitiesQuery = targetHostsFragment + `
MATCH (v:Vulnerability) WHERE v.group_id = $group_id
  AND EXISTS {
    MATCH (th:Host)-[:HAS_PORT|RUNS_SERVICE|HOSTS_APP|HAS_ENDPOINT|HAS_VHOST|DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY|HAS_VULNERABILITY*1..4]->(v)
    WHERE th.uuid IN targetUUIDs
  }
OPTIONAL MATCH (src)-[:DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY|HAS_VULNERABILITY]->(v)
OPTIONAL MATCH (v)<-[:HAS_VULNERABILITY]-(h:Host) WHERE h.uuid IN targetUUIDs
RETURN v.uuid AS uuid, v.name AS name, v.summary AS summary,
       h.name AS foundOn, coalesce(src.source_description, src.name, '') AS source
ORDER BY v.created_at DESC
`

// ─── Tool usage ──────────────────────────────────────────────────────────

// toolUsageQuery counts episodes named "tool_execution_<tool>" per tool.
// Flow-level (not host-centric), so no target-host filter.
const toolUsageQuery = `
MATCH (e:Episodic) WHERE e.group_id = $group_id AND e.name STARTS WITH 'tool_execution_'
RETURN substring(e.name, size('tool_execution_')) AS tool, count(*) AS executions
ORDER BY executions DESC
`

// ─── Artifacts ──────────────────────────────────────────────────────────

// artifactsQuery returns one row per Artifact produced during the flow. Graphiti
// links an artifact to its producer through PRODUCED / RESULTED_IN /
// EXTRACTED_FROM from a non-Episodic entity (Attempt, Artifact, Evidence), never
// from the Episodic episode node itself, so the producer is resolved from any of
// those edges (and left empty when there is none).
const artifactsQuery = `
MATCH (a:Artifact) WHERE a.group_id = $group_id
OPTIONAL MATCH (a)<-[:PRODUCED|RESULTED_IN|EXTRACTED_FROM]-(src)
WITH a, head([x IN collect(DISTINCT src.name) WHERE x IS NOT NULL]) AS producedBy
RETURN coalesce(a.name, a.summary) AS artifact,
       coalesce(producedBy, '') AS producedBy,
       coalesce(a.summary, '') AS summary
ORDER BY coalesce(a.created_at, a.valid_at) DESC
LIMIT $limit
`
