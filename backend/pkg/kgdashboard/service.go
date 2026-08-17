package kgdashboard

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/sirupsen/logrus"
)

// defaultMaxNodes bounds the FULL graph view to protect the frontend from a
// very large flow (the Graphiti benchmark observed 3.4k nodes / 12k edges).
// Above the cap, nodes are kept newest-first.
const defaultMaxNodes = 1500

// defaultRowLimit bounds per-block result rows (attack surface, artifacts).
const defaultRowLimit = 500

// defaultExamples limits how many example names are returned per aggregated
// row (credentials, vulnerability breakdown).
const defaultExamples = 5

// defaultCacheTTL bounds how long a per-flow block result stays cached.
const defaultCacheTTL = 15 * time.Second

// Service is the read-only entry point the GraphQL resolvers use. A nil
// service (or a service whose Neo4j client is disabled) returns empty results
// and never errors, so the dashboard degrades gracefully when Graphiti is
// off. All methods are safe for concurrent use.
type Service interface {
	IsEnabled() bool
	GetAttackGraph(ctx context.Context, flowID int64, groupID string, view AttackGraphView) (*AttackGraph, error)
	GetTagStats(ctx context.Context, flowID int64, groupID string) ([]GraphitiTagStat, error)
	GetAttackSurface(ctx context.Context, flowID int64, groupID string) ([]AttackSurfaceItem, error)
	GetCredentialsStatus(ctx context.Context, flowID int64, groupID string) ([]CredentialStatusRow, error)
	GetValidAccesses(ctx context.Context, flowID int64, groupID string) ([]ValidAccessRow, error)
	GetInfrastructureMap(ctx context.Context, flowID int64, groupID string) ([]InfraMapRow, error)
	GetOpenPorts(ctx context.Context, flowID int64, groupID string) ([]OpenPortRow, error)
	GetVulnerabilityBreakdown(ctx context.Context, flowID int64, groupID string) ([]VulnBreakdownRow, error)
	GetDetectedCVEs(ctx context.Context, flowID int64, groupID string) ([]DetectedCVERow, error)
	GetToolUsage(ctx context.Context, flowID int64, groupID string) ([]GraphitiToolUsageRow, error)
	GetArtifacts(ctx context.Context, flowID int64, groupID string) ([]ArtifactRow, error)
}

// NewService builds a Service backed by Neo4j. It is the only constructor
// callers should use; main wires it from cfg.Neo4j* and cfg.GraphitiEnabled.
// A failure to connect is logged and swallowed: the returned Service is
// disabled and its methods return empty results.
func NewService(uri, user, password, database string, maxConns int, timeout time.Duration) Service {
	client, _ := newNeo4jClient(uri, user, password, database, maxConns, timeout)
	return &service{
		client:   client,
		maxNodes: defaultMaxNodes,
		rowLimit: defaultRowLimit,
		cacheTTL: defaultCacheTTL,
		cache:    map[cacheKey]cacheEntry{},
	}
}

type service struct {
	client   *neo4jClient
	maxNodes int
	rowLimit int
	cacheTTL time.Duration

	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

type cacheKey struct {
	block  string
	flowID int64
	view   string
}

type cacheEntry struct {
	at   time.Time
	data any
}

// IsEnabled reports whether the dashboard subsystem can serve data.
func (s *service) IsEnabled() bool {
	return s != nil && s.client != nil && s.client.isEnabled()
}

func (s *service) fromCache(k cacheKey) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, hit := s.cache[k]
	if !hit {
		return nil, false
	}
	if time.Since(e.at) > s.cacheTTL {
		delete(s.cache, k)
		return nil, false
	}
	return e.data, true
}

func (s *service) putCache(k cacheKey, v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[k] = cacheEntry{at: time.Now(), data: v}
	// Bounded eviction: if the cache grows past 256 entries, drop the oldest.
	if len(s.cache) > 256 {
		var oldestKey cacheKey
		var oldestAt time.Time
		for kk, vv := range s.cache {
			if oldestAt.IsZero() || vv.at.Before(oldestAt) {
				oldestKey, oldestAt = kk, vv.at
			}
		}
		delete(s.cache, oldestKey)
	}
}

// run executes cypher with the read-only client and visits each record. When
// the client is disabled, it returns nil and leaves the visitor untouched so
// callers can fall through to an empty result.
func (s *service) run(ctx context.Context, cypher string, params map[string]any, visit func(*neo4j.Record) error) error {
	if !s.IsEnabled() {
		return nil
	}
	return s.client.runReadTx(ctx, cypher, params, visit)
}

// GetAttackGraph materializes the requested view. MAIN filters by
// attack-chain labels and edge types; FULL returns every flow node, capped at
// maxNodes (newest first).
func (s *service) GetAttackGraph(ctx context.Context, flowID int64, groupID string, view AttackGraphView) (*AttackGraph, error) {
	if !s.IsEnabled() {
		return &AttackGraph{}, nil
	}

	if view == "" {
		view = AttackGraphViewMain
	}
	k := cacheKey{block: "attackgraph", flowID: flowID, view: string(view)}
	if v, ok := s.fromCache(k); ok {
		return v.(*AttackGraph), nil
	}

	out := &AttackGraph{Nodes: []AttackGraphNode{}, Edges: []AttackGraphEdge{}}

	var (
		nodesCypher, edgesCypher, countCypher string
		baseParams                            map[string]any
	)
	switch view {
	case AttackGraphViewFull:
		baseParams = map[string]any{"group_id": groupID, "cap": int64(s.maxNodes)}
		nodesCypher = attackGraphFullQuery
		edgesCypher = attackGraphFullEdgesQuery
		countCypher = attackGraphFullCountQuery
	case AttackGraphViewMain:
		// The MAIN view uses a tighter cap than FULL — it only shows the attack
		// chain backbone, which is a small fraction of the full graph.
		mainCap := s.maxNodes
		if mainCap > 200 {
			mainCap = 200
		}
		baseParams = map[string]any{
			"group_id":  groupID,
			"cap":       int64(mainCap),
			"labels":    mainViewLabels,
			"minDegree": int64(minHostDegree),
		}
		nodesCypher = attackGraphMainQuery
		edgesCypher = attackGraphMainEdgesQuery
		countCypher = attackGraphMainCountQuery
	default:
		return out, fmt.Errorf("kgdashboard: unknown attack graph view %q", view)
	}

	// 1. Total node count (before the cap) for truncation reporting.
	err := s.run(ctx, countCypher, baseParams, func(rec *neo4j.Record) error {
		out.TotalNodes = int(recInt(rec, "total"))
		return nil
	})
	if err != nil {
		logrus.WithError(err).WithField("flow_id", flowID).Warn("kgdashboard: attack graph count read failed")
		return out, nil
	}

	// 2. Nodes (capped, newest first). The cap keeps the payload predictable
	//    for the frontend layout engine.
	nodesByUUID := map[string]struct{}{}
	err = s.run(ctx, nodesCypher, baseParams, func(rec *neo4j.Record) error {
		n, ok := rec.Get("n")
		if !ok {
			return nil
		}
		neo4jNode, ok := n.(neo4j.Node)
		if !ok {
			return nil
		}
		nd := nodeFromNeo4j(neo4jNode)
		if _, dup := nodesByUUID[nd.UUID]; dup {
			return nil
		}
		nodesByUUID[nd.UUID] = struct{}{}
		out.Nodes = append(out.Nodes, nd)
		return nil
	})
	if err != nil {
		logrus.WithError(err).WithField("flow_id", flowID).Warn("kgdashboard: attack graph nodes read failed")
		return out, nil
	}
	if out.TotalNodes > len(out.Nodes) {
		out.Truncated = true
	}

	// 3. Edges. The query already returns source/target entity UUIDs, so we do
	//    not need to map internal ids. Edges whose endpoints were not in the
	//    capped node set are dropped to keep the graph consistent on screen.
	err = s.run(ctx, edgesCypher, baseParams, func(rec *neo4j.Record) error {
		srcUUID := recStr(rec, "srcUUID")
		tgtUUID := recStr(rec, "tgtUUID")
		if _, ok := nodesByUUID[srcUUID]; !ok {
			return nil
		}
		if _, ok := nodesByUUID[tgtUUID]; !ok {
			return nil
		}
		e, ok := rec.Get("edge")
		if !ok {
			return nil
		}
		rel, ok := e.(neo4j.Relationship)
		if !ok {
			return nil
		}
		var fact string
		if v, ok := rel.Props["fact"]; ok {
			if str, ok := v.(string); ok {
				fact = str
			}
		}
		out.Edges = append(out.Edges, AttackGraphEdge{
			UUID:       rel.ElementId,
			Type:       rel.Type,
			Fact:       fact,
			SourceUUID: srcUUID,
			TargetUUID: tgtUUID,
			CreatedAt:  neo4jPropTime(rel.Props, "created_at"),
		})
		return nil
	})
	if err != nil {
		logrus.WithError(err).WithField("flow_id", flowID).Warn("kgdashboard: attack graph edges read failed")
		return out, nil
	}

	out.Edges = dedupEdgesByUUID(out.Edges)
	out.TotalEdges = len(out.Edges)

	s.putCache(k, out)
	return out, nil
}
