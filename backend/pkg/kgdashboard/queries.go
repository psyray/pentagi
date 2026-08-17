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
// AttackTechnique, and Misconfiguration are excluded — they are either noise
// (command outputs, extracted facts, wordlist files misclassified as misconfig)
// or progress tracking, not part of the attack chain backbone. Real
// misconfigurations remain visible in the FULL view and the Vulnerability
// breakdown table.
var mainViewLabels = []string{
	"Host", "Port", "Service", "Vulnerability",
	"ValidAccess", "Account", "Credential", "PrivChange",
	"WebApp", "Vhost",
}

// attackSurfaceLabels is the entity label set watched by the attack surface
// overview block. Slimmed to attack-chain entities only — no Artifact,
// Evidence, Agent, Attempt, Capability (noise). Misconfiguration and Endpoint
// are kept here (but not in mainViewLabels) because the attack surface
// overview is broader than the graph MAIN view.
var attackSurfaceLabels = []string{
	"Host", "Port", "Service", "Vulnerability", "Misconfiguration",
	"ValidAccess", "Account", "Credential", "Endpoint", "WebApp", "Vhost",
	"PrivChange",
}

// minHostEdgeTypes is the minimum number of distinct attack-chain edge types
// a Host must have to be considered a real target (as opposed to localhost,
// VPN, gateway infrastructure that Graphiti happens to extract from command
// output). The real target host (e.g. 10.129.244.174 with 4 types: HAS_PORT,
// RUNS_SERVICE, HAS_VHOST, HOSTS_APP) passes easily; noise hosts with only
// HAS_PORT from `ip a` output are filtered out.
const minHostEdgeTypes = 2

// targetHostsFragment is a reusable Cypher preamble that identifies "target"
// Host UUIDs — Hosts with >= minHostEdgeTypes distinct outgoing attack-chain
// edge types. Queries that need to filter to the attack target(s) prepend
// this fragment and use the resulting `targetUUIDs` list in their WHERE
// clause. Requires $group_id and $minEdgeTypes parameters.
const targetHostsFragment = `
MATCH (th:Host)-[tr:HAS_PORT|HAS_VULNERABILITY|HAS_MISCONFIGURATION|HOSTS_APP|RUNS_SERVICE|HAS_VHOST|DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY]->()
WHERE th.group_id = $group_id
WITH th, count(DISTINCT type(tr)) AS tCount
WHERE tCount >= $minEdgeTypes
WITH collect(th.uuid) AS targetUUIDs
`

// targetConnectedFilter is a reusable Cypher WHERE clause fragment that keeps
// only nodes connected to a target Host (directly at depth 1, or indirectly
// at depth 2 through a Service/Port/Endpoint). This filters out orphan noise
// like "read_file command" (a tool name misclassified as Port) while keeping
// Vulnerabilities and Credentials that connect to the Host through a Service.
//
// Depth 1: Host -> n (HAS_PORT, RUNS_SERVICE, HAS_VHOST, etc.)
// Depth 1 (reverse): n -> Host (ON_HOST)
// Depth 2: Host -> Service <- DETECTED_VULNERABILITY <- Vulnerability
// Depth 2: Host -> Service <- AUTHENTICATES_TO <- Credential
// Depth 2: Host -> Service <- VIA_SERVICE <- ValidAccess
// Depth 2 (reverse): Service -> ON_HOST -> Host, Service -> DETECTED_VULN -> Vuln
const targetConnectedFilter = `
  AND (
    n.uuid IN targetUUIDs
    OR EXISTS {
      MATCH (th)-[:HAS_PORT|RUNS_SERVICE|HAS_VHOST|HAS_VULNERABILITY|HAS_MISCONFIGURATION|HOSTS_APP|HAS_ENDPOINT|ON_HOST|DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY]->(n)
      WHERE th.uuid IN targetUUIDs
    }
    OR EXISTS {
      MATCH (n)-[:ON_HOST]->(th) WHERE th.uuid IN targetUUIDs
    }
    OR EXISTS {
      MATCH (th)-[:RUNS_SERVICE|HAS_PORT|HAS_VHOST|HOSTS_APP|HAS_ENDPOINT]->(mid)
            <-[:DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY|AUTHENTICATES_TO|VIA_SERVICE|YIELDED_ACCESS|YIELDED_PRIV_ACCESS|ESCALATED_VIA]-(n)
      WHERE th.uuid IN targetUUIDs
    }
    OR EXISTS {
      MATCH (th)<-[:ON_HOST]-(mid)-[:DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY|AUTHENTICATES_TO|VIA_SERVICE]->(n)
      WHERE th.uuid IN targetUUIDs
    }
  )
`

// ─── Attack graph queries ───────────────────────────────────────────────

// attackGraphMainQuery returns the MAIN subgraph: attack-chain entity nodes
// that are connected to target hosts (directly at depth 1, or via a Service
// at depth 2). This filters out orphan noise like "read_file command" (a tool
// name misclassified as Port by Graphiti) while keeping Vulnerabilities and
// Credentials connected through Services.
const attackGraphMainQuery = targetHostsFragment + `
MATCH (n) WHERE n.group_id = $group_id
  AND ANY(l IN labels(n) WHERE l IN $labels)` + targetConnectedFilter + `
RETURN n AS n, coalesce(n.created_at, n.valid_at) AS ts
ORDER BY ts DESC
LIMIT $cap
`

// attackGraphMainEdgesQuery returns the edges of the MAIN view. All edges
// between two MAIN-view nodes are kept (no edge-type filter) so the graph
// stays connected even in early flows.
const attackGraphMainEdgesQuery = `
MATCH (src)-[r]->(tgt)
WHERE src.group_id = $group_id AND tgt.group_id = $group_id AND r.group_id = $group_id
  AND ANY(l IN labels(src) WHERE l IN $labels)
  AND ANY(l IN labels(tgt) WHERE l IN $labels)
RETURN coalesce(src.uuid, src.elementId) AS srcUUID,
       coalesce(tgt.uuid, tgt.elementId) AS tgtUUID,
       r AS edge
`

// attackGraphMainCountQuery returns the total node count for the MAIN view.
const attackGraphMainCountQuery = targetHostsFragment + `
MATCH (n) WHERE n.group_id = $group_id
  AND ANY(l IN labels(n) WHERE l IN $labels)` + targetConnectedFilter + `
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
// ValidAccess or Account (heuristic for the "Compromised" status).
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

// ─── Valid accesses ──────────────────────────────────────────────────────

// validAccessesQuery returns each ValidAccess with its reachable Account,
// backing Service and the Host the service runs on. The Host is filtered to
// target hosts only so noise hosts (localhost, VPN) don't appear.
const validAccessesQuery = targetHostsFragment + `
MATCH (va:ValidAccess) WHERE va.group_id = $group_id
OPTIONAL MATCH (va)-[:AS_ACCOUNT]->(a:Account)
OPTIONAL MATCH (va)-[:VIA_SERVICE]->(s:Service)
OPTIONAL MATCH (s)-[:ON_HOST]->(h:Host) WHERE h.uuid IN targetUUIDs
RETURN va.summary AS access, a.name AS account, h.name AS host, s.name AS service, va.summary AS summary
ORDER BY va.created_at DESC
`

// ─── Infrastructure map ──────────────────────────────────────────────────

// infraMapQuery returns Host -> Port -> Service rows, filtered to target hosts
// only so localhost, VPN, and container IPs don't clutter the map.
const infraMapQuery = targetHostsFragment + `
MATCH (h:Host)-[:HAS_PORT]->(p:Port)
WHERE h.group_id = $group_id AND p.group_id = $group_id AND h.uuid IN targetUUIDs
OPTIONAL MATCH (h)-[:RUNS_SERVICE]->(s:Service)
RETURN h.name AS host, p.name AS port, s.name AS service
ORDER BY host, toInteger(p.name)
`

// ─── Open ports ─────────────────────────────────────────────────────────

// openPortsQuery returns one row per discovered port on a target host.
const openPortsQuery = targetHostsFragment + `
MATCH (p:Port)<-[:HAS_PORT]-(h:Host)
WHERE p.group_id = $group_id AND h.group_id = $group_id AND h.uuid IN targetUUIDs
OPTIONAL MATCH (h)-[:RUNS_SERVICE]->(s:Service)
RETURN p.name AS port, s.name AS service, h.name AS host
ORDER BY toInteger(p.name), host
`

// ─── Vulnerabilities ────────────────────────────────────────────────────

// allVulnerabilitiesQuery returns every Vulnerability node connected to a
// target host (directly via HAS_VULNERABILITY, or via a Service at depth 2
// through DETECTED_VULNERABILITY/CONFIRMED_VULNERABILITY).
const allVulnerabilitiesQuery = targetHostsFragment + `
MATCH (v:Vulnerability) WHERE v.group_id = $group_id
  AND (
    EXISTS { MATCH (h:Host)-[:HAS_VULNERABILITY]->(v) WHERE h.uuid IN targetUUIDs }
    OR EXISTS { MATCH (h:Host)-[:RUNS_SERVICE]->(s:Service)-[:DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY]->(v) WHERE h.uuid IN targetUUIDs }
    OR EXISTS { MATCH (h:Host)<-[:ON_HOST]-(s:Service)-[:DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY]->(v) WHERE h.uuid IN targetUUIDs }
  )
OPTIONAL MATCH (src)-[rel:DETECTED_VULNERABILITY|CONFIRMED_VULNERABILITY]->(v)
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

// artifactsQuery returns one row per Artifact produced by an Episodic source.
// Flow-level (not host-centric), so no target-host filter.
const artifactsQuery = `
MATCH (a:Artifact)<-[:PRODUCED]-(e:Episodic)
WHERE a.group_id = $group_id
RETURN coalesce(a.name, a.summary) AS artifact,
       coalesce(e.source_description, e.name) AS producedBy,
       a.summary AS summary
ORDER BY a.created_at DESC
LIMIT $limit
`
