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

// mainViewLabels is the entity label set of the MAIN view: only attack-chain
// entities. Artifact, Evidence, Agent, Attempt, Endpoint, Capability,
// AttackTechnique are excluded — they are either noise (command outputs,
// extracted facts) or progress tracking, not part of the attack chain backbone.
var mainViewLabels = []string{
	"Host", "Port", "Service", "Vulnerability", "Misconfiguration",
	"ValidAccess", "Account", "Credential", "PrivChange",
	"WebApp", "Vhost",
}

// hostAttackEdgeTypes are the edge types that indicate a Host is a real attack
// target (as opposed to localhost, VPN, gateway infrastructure that
// Graphiti happens to extract from command output).
var hostAttackEdgeTypes = []string{
	"HAS_PORT", "HAS_VULNERABILITY", "HAS_MISCONFIGURATION",
	"HOSTS_APP", "RUNS_SERVICE", "HAS_VHOST",
	"DETECTED_VULNERABILITY", "CONFIRMED_VULNERABILITY",
}

// minHostDegree is the minimum number of outgoing attack-chain edges a Host
// must have to be included in the MAIN view. The real target host (e.g.
// 10.129.244.174 with 34 connections) passes easily; noise hosts (localhost,
// VPN, gateway with 0-2 spurious edges) are filtered out.
const minHostDegree = 3

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

// attackGraphMainQuery returns the MAIN subgraph: attack-chain entity nodes,
// filtered to only include Hosts with enough connections to be real targets
// (minHostDegree). Non-Host nodes (Port, Service, Vulnerability, etc.) are
// always included since they are already filtered by mainViewLabels.
const attackGraphMainQuery = `
MATCH (n) WHERE n.group_id = $group_id
  AND ANY(l IN labels(n) WHERE l IN $labels)
  AND (
    NOT 'Host' IN labels(n)
    OR EXISTS {
      MATCH (h:Host) WHERE h.uuid = n.uuid AND h.group_id = $group_id
        AND size([(h)-[:HAS_PORT|HAS_VULNERABILITY|HAS_MISCONFIGURATION|HOSTS_APP|RUNS_SERVICE|HAS_VHOST|DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY]->() | 1]) >= $minDegree
    }
  )
RETURN n AS n, coalesce(n.created_at, n.valid_at) AS ts
ORDER BY ts DESC
LIMIT $cap
`

// attackGraphMainEdgesQuery returns the edges of the MAIN view, with the
// source and target entity UUIDs so the frontend can wire them up without
// guessing from internal Neo4j ids. Unlike the original design, we do NOT
// filter by edge type here: the MAIN vs FULL distinction is about which nodes
// to show, not which edges. All edges between two MAIN-view nodes are kept
// so the graph is connected even in early flows where attack-chain edges
// (HAS_PORT, YIELDED_ACCESS ...) have not been extracted yet.
const attackGraphMainEdgesQuery = `
MATCH (src)-[r]->(tgt)
WHERE src.group_id = $group_id AND tgt.group_id = $group_id AND r.group_id = $group_id
  AND ANY(l IN labels(src) WHERE l IN $labels)
  AND ANY(l IN labels(tgt) WHERE l IN $labels)
RETURN coalesce(src.uuid, src.elementId) AS srcUUID,
       coalesce(tgt.uuid, tgt.elementId) AS tgtUUID,
       r AS edge
`

// attackGraphMainCountQuery returns the total node count for the MAIN view
// (before the cap) so the resolver can report truncation. Applies the same
// Host degree filter as attackGraphMainQuery.
const attackGraphMainCountQuery = `
MATCH (n) WHERE n.group_id = $group_id
  AND ANY(l IN labels(n) WHERE l IN $labels)
  AND (
    NOT 'Host' IN labels(n)
    OR EXISTS {
      MATCH (h:Host) WHERE h.uuid = n.uuid AND h.group_id = $group_id
        AND size([(h)-[:HAS_PORT|HAS_VULNERABILITY|HAS_MISCONFIGURATION|HOSTS_APP|RUNS_SERVICE|HAS_VHOST|DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY]->() | 1]) >= $minDegree
    }
  )
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
// Every node variable referenced in an EXISTS subquery WHERE clause must be
// named — anonymous nodes like (:Service) cannot be referenced as Service.
const credentialsByCompromisedQuery = `
MATCH (c:Credential) WHERE c.group_id = $group_id
  AND (
    EXISTS { MATCH (c)-[:AUTHENTICATES_TO]->(s:Service) WHERE s.group_id = $group_id }
    OR EXISTS { MATCH (c)-[:BELONGS_TO_ACCOUNT]->(a:Account) WHERE a.group_id = $group_id }
    OR EXISTS {
      MATCH (at:Attempt)-[:YIELDED_ACCESS]->(va:ValidAccess)
            <-[:AS_ACCOUNT]-(ac:Account)<-[:BELONGS_TO_ACCOUNT]-(c)
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
RETURN substring(e.name, size('tool_execution_')) AS tool, count(*) AS executions
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
