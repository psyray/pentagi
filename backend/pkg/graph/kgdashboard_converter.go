package graph

import (
	"time"

	"pentagi/pkg/graph/model"
	"pentagi/pkg/kgdashboard"
)

// kgdashboard converters map the read-only Neo4j result structs to the
// gqlgen-generated GraphQL model pointers. They are intentionally trivial: the
// kgdashboard types use the same string enum values as the model enums, so
// direct casts are safe.

func ptrTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	t2 := t
	return &t2
}

func convertAttackGraph(in *kgdashboard.AttackGraph) *model.AttackGraph {
	if in == nil {
		return &model.AttackGraph{}
	}
	nodes := make([]*model.AttackGraphNode, 0, len(in.Nodes))
	for _, n := range in.Nodes {
		n := n
		nodes = append(nodes, convertAttackGraphNode(&n))
	}
	edges := make([]*model.AttackGraphEdge, 0, len(in.Edges))
	for _, e := range in.Edges {
		e := e
		edges = append(edges, convertAttackGraphEdge(&e))
	}
	return &model.AttackGraph{
		Nodes:      nodes,
		Edges:      edges,
		TotalNodes: in.TotalNodes,
		TotalEdges: in.TotalEdges,
		Truncated:  in.Truncated,
	}
}

func convertAttackGraphNode(n *kgdashboard.AttackGraphNode) *model.AttackGraphNode {
	if n == nil {
		return nil
	}
	return &model.AttackGraphNode{
		UUID:      n.UUID,
		Labels:    n.Labels,
		Name:      n.Name,
		Summary:   n.Summary,
		CreatedAt: ptrTime(n.CreatedAt),
	}
}

func convertAttackGraphEdge(e *kgdashboard.AttackGraphEdge) *model.AttackGraphEdge {
	if e == nil {
		return nil
	}
	return &model.AttackGraphEdge{
		UUID:       e.UUID,
		Type:       e.Type,
		Fact:       e.Fact,
		SourceUUID: e.SourceUUID,
		TargetUUID: e.TargetUUID,
		CreatedAt:  ptrTime(e.CreatedAt),
	}
}

func convertTagStats(in []kgdashboard.GraphitiTagStat) []*model.GraphitiTagStat {
	out := make([]*model.GraphitiTagStat, 0, len(in))
	for _, s := range in {
		s := s
		out = append(out, &model.GraphitiTagStat{Tag: s.Tag, Count: s.Count})
	}
	return out
}

func convertAttackSurface(in []kgdashboard.AttackSurfaceItem) []*model.AttackSurfaceItem {
	out := make([]*model.AttackSurfaceItem, 0, len(in))
	for _, it := range in {
		it := it
		out = append(out, &model.AttackSurfaceItem{
			Type:      it.Type,
			Name:      it.Name,
			Summary:   it.Summary,
			CreatedAt: ptrTime(it.CreatedAt),
		})
	}
	return out
}

func convertCredentialStatus(in []kgdashboard.CredentialStatusRow) []*model.CredentialStatusRow {
	out := make([]*model.CredentialStatusRow, 0, len(in))
	for _, r := range in {
		r := r
		out = append(out, &model.CredentialStatusRow{
			Status:   model.CredentialStatus(r.Status),
			Count:    r.Count,
			Examples: r.Examples,
		})
	}
	return out
}

func convertValidAccesses(in []kgdashboard.ValidAccessRow) []*model.ValidAccessRow {
	out := make([]*model.ValidAccessRow, 0, len(in))
	for _, r := range in {
		r := r
		out = append(out, &model.ValidAccessRow{
			Access:  r.Access,
			Account: r.Account,
			Host:    r.Host,
			Service: r.Service,
			Summary: r.Summary,
		})
	}
	return out
}

func convertInfraMap(in []kgdashboard.InfraMapRow) []*model.InfraMapRow {
	out := make([]*model.InfraMapRow, 0, len(in))
	for _, r := range in {
		r := r
		out = append(out, &model.InfraMapRow{Host: r.Host, Port: r.Port, Service: r.Service})
	}
	return out
}

func convertOpenPorts(in []kgdashboard.OpenPortRow) []*model.OpenPortRow {
	out := make([]*model.OpenPortRow, 0, len(in))
	for _, r := range in {
		r := r
		out = append(out, &model.OpenPortRow{Port: r.Port, Service: r.Service, Host: r.Host})
	}
	return out
}

func convertVulnBreakdown(in []kgdashboard.VulnBreakdownRow) []*model.VulnBreakdownRow {
	out := make([]*model.VulnBreakdownRow, 0, len(in))
	for _, r := range in {
		r := r
		out = append(out, &model.VulnBreakdownRow{
			Category: model.VulnCategory(r.Category),
			Count:    r.Count,
			Examples: r.Examples,
		})
	}
	return out
}

func convertDetectedCVEs(in []kgdashboard.DetectedCVERow) []*model.DetectedCVERow {
	out := make([]*model.DetectedCVERow, 0, len(in))
	for _, r := range in {
		r := r
		out = append(out, &model.DetectedCVERow{Cve: r.CVE, FoundOn: r.FoundOn, Source: r.Source})
	}
	return out
}

func convertToolUsage(in []kgdashboard.GraphitiToolUsageRow) []*model.GraphitiToolUsageRow {
	out := make([]*model.GraphitiToolUsageRow, 0, len(in))
	for _, r := range in {
		r := r
		out = append(out, &model.GraphitiToolUsageRow{Tool: r.Tool, Executions: r.Executions})
	}
	return out
}

func convertArtifacts(in []kgdashboard.ArtifactRow) []*model.ArtifactRow {
	out := make([]*model.ArtifactRow, 0, len(in))
	for _, r := range in {
		r := r
		out = append(out, &model.ArtifactRow{Artifact: r.Artifact, ProducedBy: r.ProducedBy, Summary: r.Summary})
	}
	return out
}
