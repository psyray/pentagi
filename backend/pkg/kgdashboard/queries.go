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

// mainViewLabels is the entity label set of the attack-chain backbone used by
// the MAIN graph view. Edges are restricted to the structural / progress /
// attempt classes that make the chain readable.
var mainViewLabels = []string{
	"Host", "Port", "Service", "Vulnerability", "Misconfiguration",
	"ValidAccess", "Account", "Credential", "Attempt", "PrivChange",
	"WebApp", "Vhost",
}

// mainViewEdgeTypes restricts the MAIN view to attack-chain relationships.
var mainViewEdgeTypes = []string{
	"HAS_PORT", "RUNS_SERVICE", "ON_HOST", "HAS_VULNERABILITY",
	"DETECTED_VULNERABILITY", "CONFIRMED_VULNERABILITY", "HAS_MISCONFIGURATION",
	"HAS_ENDPOINT", "HOSTS_APP", "HAS_VHOST",
	"AUTHENTICATES_TO", "BELONGS_TO_ACCOUNT", "OWNS_ACCOUNT",
	"YIELDED_ACCESS", "YIELDED_PRIV_ACCESS", "AS_ACCOUNT", "VIA_SERVICE",
	"ESCALATED_VIA", "PIVOTED_TO",
}

// attackSurfaceLabels is the entity label set watched by the attack surface
// overview block. Order is kept stable for stable frontend rendering.
var attackSurfaceLabels = []string{
	"Host", "Port", "Service", "Vulnerability", "Misconfiguration",
	"ValidAccess", "Account", "Credential", "Endpoint", "WebApp", "Vhost",
	"PrivChange", "Artifact", "Attempt", "Capability", "Agent",
}

// tagStatsQuery counts entities per specific label (excluding the generic
// `Entity`/`Episodic`/`Agent` bucket when it is the only label).
const tagStatsQuery = `
MATCH (n) WHERE n.group_id = $group_id
UNWIND [l IN labels(n) WHERE NOT l IN ['Entity']] AS label
RETURN label AS tag, count(*) AS count
ORDER BY count DESC, label ASC
`

// attackGraphMainQuery returns the bounded MAIN subgraph: every node whose
// label set intersects mainViewLabels, plus every edge between two such
// nodes whose type is in mainViewEdgeTypes. The LIMIT is applied client-side
// after the driver returns (the cap is enforced via the maxNodes option).
const attackGraphMainQuery = `
MATCH (n) WHERE n.group_id = $group_id
  AND ANY(l IN labels(n) WHERE l IN $labels)
RETURN n AS n, coalesce(n.created_at, n.valid_at) AS ts
ORDER BY ts DESC
LIMIT $cap
`

// attackGraphMainEdgesQuery returns the edges of the MAIN view, with the
// source and target entity UUIDs so the frontend can wire them up without
// guessing from internal Neo4j ids.
const attackGraphMainEdgesQuery = `
MATCH (src)-[r]->(tgt)
WHERE src.group_id = $group_id AND tgt.group_id = $group_id AND r.group_id = $group_id
  AND type(r) IN $edgeTypes
  AND ANY(l IN labels(src) WHERE l IN $labels)
  AND ANY(l IN labels(tgt) WHERE l IN $labels)
RETURN coalesce(src.uuid, src.elementId) AS srcUUID,
       coalesce(tgt.uuid, tgt.elementId) AS tgtUUID,
       r AS edge
`

// attackGraphMainCountQuery returns the total node count for the MAIN view
// (before the cap) so the resolver can report truncation.
const attackGraphMainCountQuery = `
MATCH (n) WHERE n.group_id = $group_id
  AND ANY(l IN labels(n) WHERE l IN $labels)
RETURN count(n) AS total
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

// attackSurfaceQuery lists entities whose label set intersects
// attackSurfaceLabels, newest first, capped by $limit.
const attackSurfaceQuery = `
MATCH (n) WHERE n.group_id = $group_id
  AND ANY(l IN labels(n) WHERE l IN $labels)
RETURN labels(n) AS labels, n.name AS name, n.summary AS summary,
       coalesce(n.created_at, n.valid_at) AS createdAt
ORDER BY createdAt DESC
LIMIT $limit
`

// credentialsByCompromisedQuery returns credential UUIDs that led to a
// ValidAccess or Account (heuristic for the "Compromised" status).
const credentialsByCompromisedQuery = `
MATCH (c:Credential) WHERE c.group_id = $group_id
  AND (
    EXISTS { MATCH (c)-[:AUTHENTICATES_TO]->(:Service) WHERE Service.group_id = $group_id }
    OR EXISTS { MATCH (c)-[:BELONGS_TO_ACCOUNT]->(:Account) WHERE Account.group_id = $group_id }
    OR EXISTS { MATCH (:Attempt)-[:YIELDED_ACCESS]->(:ValidAccess)<-[:AS_ACCOUNT]-(:Account)<-[:BELONGS_TO_ACCOUNT]-(c) }
  )
RETURN c.uuid AS uuid, c.name AS name, c.summary AS summary
`

// allCredentialsQuery returns every credential of the flow.
const allCredentialsQuery = `
MATCH (c:Credential) WHERE c.group_id = $group_id
RETURN c.uuid AS uuid, c.name AS name, c.summary AS summary
ORDER BY c.created_at DESC
`

// validAccessesQuery returns each ValidAccess with its reachable Account,
// backing Service and the Host the service runs on.
const validAccessesQuery = `
MATCH (va:ValidAccess) WHERE va.group_id = $group_id
OPTIONAL MATCH (va)-[:AS_ACCOUNT]->(a:Account)
OPTIONAL MATCH (va)-[:VIA_SERVICE]->(s:Service)
OPTIONAL MATCH (s)-[:ON_HOST]->(h:Host)
RETURN va.summary AS access, a.name AS account, h.name AS host, s.name AS service, va.summary AS summary
ORDER BY va.created_at DESC
`

// infraMapQuery returns Host → Port → Service rows for the flow.
const infraMapQuery = `
MATCH (h:Host)-[:HAS_PORT]->(p:Port)
WHERE h.group_id = $group_id AND p.group_id = $group_id
OPTIONAL MATCH (h)-[:RUNS_SERVICE]->(s:Service)
RETURN h.name AS host, p.name AS port, s.name AS service
ORDER BY host, toInteger(p.name)
`

// openPortsQuery returns one row per discovered port with the host that
// exposes it and the service (if any) detected on it.
const openPortsQuery = `
MATCH (p:Port)<-[:HAS_PORT]-(h:Host)
WHERE p.group_id = $group_id AND h.group_id = $group_id
OPTIONAL MATCH (h)-[:RUNS_SERVICE]->(s:Service)
RETURN p.name AS port, s.name AS service, h.name AS host
ORDER BY toInteger(p.name), host
`

// allVulnerabilitiesQuery returns every Vulnerability node of the flow with
// its name and summary (used to bucket by category and detect CVEs).
const allVulnerabilitiesQuery = `
MATCH (v:Vulnerability) WHERE v.group_id = $group_id
OPTIONAL MATCH (src)-[rel:DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY]->(v)
OPTIONAL MATCH (v)<-[:HAS_VULNERABILITY]-(h:Host)
RETURN v.uuid AS uuid, v.name AS name, v.summary AS summary,
       h.name AS foundOn, coalesce(src.source_description, src.name, '') AS source
ORDER BY v.created_at DESC
`

// toolUsageQuery counts episodes named "tool_execution_<tool>" per tool.
// Graphiti names tool episodes that way (see performer.storeToolExecutionToGraphiti).
const toolUsageQuery = `
MATCH (e:Episodic) WHERE e.group_id = $group_id AND e.name STARTS WITH 'tool_execution_'
RETURN substring(e.name, length('tool_execution_')) AS tool, count(*) AS executions
ORDER BY executions DESC
`

// artifactsQuery returns one row per Artifact produced by an Episodic source.
const artifactsQuery = `
MATCH (a:Artifact)<-[:PRODUCED]-(e:Episodic)
WHERE a.group_id = $group_id
RETURN coalesce(a.name, a.summary) AS artifact,
       coalesce(e.source_description, e.name) AS producedBy,
       a.summary AS summary
ORDER BY a.created_at DESC
LIMIT $limit
`
