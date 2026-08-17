package kgdashboard

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/sirupsen/logrus"
)

// cveRe matches a CVE identifier anywhere in a string (case-insensitive).
var cveRe = regexp.MustCompile(`(?i)\bCVE-\d{4}-\d{4,7}\b`)

// severityKeywords maps a vulnerability bucket to the keywords we look for in
// the node name + summary when no CVE id is present. Order matters: the first
// matching bucket wins, and the slice is walked Critical -> Info.
var severityKeywords = []struct {
	cat      VulnCategory
	keywords []string
}{
	{VulnCategoryCritical, []string{"critical", "rce", "remote code execution"}},
	{VulnCategoryHigh, []string{"high", "privilege escalation", "sql injection", "auth bypass"}},
	{VulnCategoryMedium, []string{"medium", "xss", "ssrf", "lfi", "rfi"}},
	{VulnCategoryLow, []string{"low"}},
	{VulnCategoryInfo, []string{"info", "informational", "note", "disclosure"}},
}

// vulnRow is the intermediate shape returned by allVulnerabilitiesQuery.
type vulnRow struct {
	name    string
	summary string
	foundOn string
	source  string
}

// GetTagStats counts entities per taxonomy tag.
func (s *service) GetTagStats(ctx context.Context, flowID int64, groupID string) ([]GraphitiTagStat, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	k := cacheKey{block: "tagstats", flowID: flowID}
	if v, ok := s.fromCache(k); ok {
		return v.([]GraphitiTagStat), nil
	}

	out := make([]GraphitiTagStat, 0)
	err := s.run(ctx, tagStatsQuery, map[string]any{"group_id": groupID}, func(rec *neo4j.Record) error {
		out = append(out, GraphitiTagStat{
			Tag:   recStr(rec, "tag"),
			Count: int(recInt(rec, "count")),
		})
		return nil
	})
	if err != nil {
		logrus.WithError(err).WithField("flow_id", flowID).Warn("kgdashboard: tag stats read failed")
		return nil, nil
	}
	s.putCache(k, out)
	return out, nil
}

// GetAttackSurface lists the most recent entities of the watched tags.
func (s *service) GetAttackSurface(ctx context.Context, flowID int64, groupID string) ([]AttackSurfaceItem, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	k := cacheKey{block: "attacksurface", flowID: flowID}
	if v, ok := s.fromCache(k); ok {
		return v.([]AttackSurfaceItem), nil
	}

	out := make([]AttackSurfaceItem, 0, s.rowLimit)
	err := s.run(ctx, attackSurfaceQuery, map[string]any{
		"group_id":     groupID,
		"labels":       attackSurfaceLabels,
		"limit":        int64(s.rowLimit),
		"minEdgeTypes": int64(minHostEdgeTypes),
	}, func(rec *neo4j.Record) error {
		labels := recLabels(rec, "labels")
		out = append(out, AttackSurfaceItem{
			Type:      primaryLabel(labels),
			Name:      recStr(rec, "name"),
			Summary:   recStr(rec, "summary"),
			CreatedAt: recTime(rec, "createdAt"),
		})
		return nil
	})
	if err != nil {
		logrus.WithError(err).WithField("flow_id", flowID).Warn("kgdashboard: attack surface read failed")
		return nil, nil
	}
	s.putCache(k, out)
	return out, nil
}

// GetCredentialsStatus infers each credential's status (Compromised vs
// Discovered) via the YIELDED_ACCESS / AUTHENTICATES_TO / BELONGS_TO_ACCOUNT
// heuristic validated with the user, then aggregates per status with up to
// defaultExamples example names.
func (s *service) GetCredentialsStatus(ctx context.Context, flowID int64, groupID string) ([]CredentialStatusRow, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	k := cacheKey{block: "credentials", flowID: flowID}
	if v, ok := s.fromCache(k); ok {
		return v.([]CredentialStatusRow), nil
	}

	compromisedUUIDs := make(map[string]struct{})
	err := s.run(ctx, credentialsByCompromisedQuery, map[string]any{"group_id": groupID}, func(rec *neo4j.Record) error {
		compromisedUUIDs[recStr(rec, "uuid")] = struct{}{}
		return nil
	})
	if err != nil {
		logrus.WithError(err).WithField("flow_id", flowID).Warn("kgdashboard: compromised credentials read failed")
		return nil, nil
	}

	type credInfo struct {
		uuid string
		name string
	}
	all := make([]credInfo, 0)
	err = s.run(ctx, allCredentialsQuery, map[string]any{"group_id": groupID}, func(rec *neo4j.Record) error {
		all = append(all, credInfo{uuid: recStr(rec, "uuid"), name: recStr(rec, "name")})
		return nil
	})
	if err != nil {
		logrus.WithError(err).WithField("flow_id", flowID).Warn("kgdashboard: credentials read failed")
		return nil, nil
	}

	buckets := map[CredentialStatus][]string{}
	for _, c := range all {
		status := CredentialStatusDiscovered
		if _, ok := compromisedUUIDs[c.uuid]; ok {
			status = CredentialStatusCompromised
		}
		buckets[status] = append(buckets[status], c.name)
	}

	out := make([]CredentialStatusRow, 0, len(buckets))
	for _, status := range []CredentialStatus{CredentialStatusCompromised, CredentialStatusDiscovered, CredentialStatusUnknown} {
		names := buckets[status]
		if len(names) == 0 && status == CredentialStatusDiscovered && len(all) == 0 {
			continue
		}
		examples := names
		if len(examples) > defaultExamples {
			examples = examples[:defaultExamples]
		}
		out = append(out, CredentialStatusRow{
			Status:   status,
			Count:    len(names),
			Examples: examples,
		})
	}
	s.putCache(k, out)
	return out, nil
}

// GetValidAccesses returns each ValidAccess with its reachable Account, the
// backing Service and the Host that runs the service.
func (s *service) GetValidAccesses(ctx context.Context, flowID int64, groupID string) ([]ValidAccessRow, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	k := cacheKey{block: "validaccess", flowID: flowID}
	if v, ok := s.fromCache(k); ok {
		return v.([]ValidAccessRow), nil
	}

	out := make([]ValidAccessRow, 0)
	err := s.run(ctx, validAccessesQuery, map[string]any{
		"group_id":     groupID,
		"minEdgeTypes": int64(minHostEdgeTypes),
	}, func(rec *neo4j.Record) error {
		out = append(out, ValidAccessRow{
			Access:  recStr(rec, "access"),
			Account: recStr(rec, "account"),
			Host:    recStr(rec, "host"),
			Service: recStr(rec, "service"),
			Summary: recStr(rec, "summary"),
		})
		return nil
	})
	if err != nil {
		logrus.WithError(err).WithField("flow_id", flowID).Warn("kgdashboard: valid accesses read failed")
		return nil, nil
	}
	s.putCache(k, out)
	return out, nil
}

// GetInfrastructureMap returns Host → Port → Service rows.
func (s *service) GetInfrastructureMap(ctx context.Context, flowID int64, groupID string) ([]InfraMapRow, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	k := cacheKey{block: "infra", flowID: flowID}
	if v, ok := s.fromCache(k); ok {
		return v.([]InfraMapRow), nil
	}
	out := make([]InfraMapRow, 0)
	err := s.run(ctx, infraMapQuery, map[string]any{
		"group_id":     groupID,
		"minEdgeTypes": int64(minHostEdgeTypes),
	}, func(rec *neo4j.Record) error {
		out = append(out, InfraMapRow{
			Host:    recStr(rec, "host"),
			Port:    recStr(rec, "port"),
			Service: recStr(rec, "service"),
		})
		return nil
	})
	if err != nil {
		logrus.WithError(err).WithField("flow_id", flowID).Warn("kgdashboard: infra map read failed")
		return nil, nil
	}
	s.putCache(k, out)
	return out, nil
}

// GetOpenPorts returns one row per discovered port.
func (s *service) GetOpenPorts(ctx context.Context, flowID int64, groupID string) ([]OpenPortRow, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	k := cacheKey{block: "openports", flowID: flowID}
	if v, ok := s.fromCache(k); ok {
		return v.([]OpenPortRow), nil
	}
	out := make([]OpenPortRow, 0)
	err := s.run(ctx, openPortsQuery, map[string]any{
		"group_id":     groupID,
		"minEdgeTypes": int64(minHostEdgeTypes),
	}, func(rec *neo4j.Record) error {
		out = append(out, OpenPortRow{
			Port:    recStr(rec, "port"),
			Service: recStr(rec, "service"),
			Host:    recStr(rec, "host"),
		})
		return nil
	})
	if err != nil {
		logrus.WithError(err).WithField("flow_id", flowID).Warn("kgdashboard: open ports read failed")
		return nil, nil
	}
	s.putCache(k, out)
	return out, nil
}

// GetVulnerabilityBreakdown buckets vulnerabilities by category (CVE first,
// then severity inferred from name + summary).
func (s *service) GetVulnerabilityBreakdown(ctx context.Context, flowID int64, groupID string) ([]VulnBreakdownRow, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	k := cacheKey{block: "vulnbreakdown", flowID: flowID}
	if v, ok := s.fromCache(k); ok {
		return v.([]VulnBreakdownRow), nil
	}

	rows, err := s.fetchVulnerabilities(ctx, flowID, groupID)
	if err != nil {
		return nil, nil
	}

	buckets := map[VulnCategory][]string{}
	for _, r := range rows {
		cat := categorizeVuln(r)
		buckets[cat] = append(buckets[cat], r.name)
	}

	out := make([]VulnBreakdownRow, 0, len(buckets))
	for _, cat := range []VulnCategory{
		VulnCategoryCVE, VulnCategoryCritical, VulnCategoryHigh,
		VulnCategoryMedium, VulnCategoryLow, VulnCategoryInfo,
	} {
		names, ok := buckets[cat]
		if !ok || len(names) == 0 {
			continue
		}
		examples := names
		if len(examples) > defaultExamples {
			examples = examples[:defaultExamples]
		}
		out = append(out, VulnBreakdownRow{
			Category: cat,
			Count:    len(names),
			Examples: examples,
		})
	}
	s.putCache(k, out)
	return out, nil
}

// GetDetectedCVEs returns the vulnerabilities whose name contains a CVE id.
func (s *service) GetDetectedCVEs(ctx context.Context, flowID int64, groupID string) ([]DetectedCVERow, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	k := cacheKey{block: "cves", flowID: flowID}
	if v, ok := s.fromCache(k); ok {
		return v.([]DetectedCVERow), nil
	}

	rows, err := s.fetchVulnerabilities(ctx, flowID, groupID)
	if err != nil {
		return nil, nil
	}

	out := make([]DetectedCVERow, 0)
	seen := make(map[string]struct{})
	for _, r := range rows {
		cve := extractCVE(r.name)
		if cve == "" {
			cve = extractCVE(r.summary)
		}
		if cve == "" {
			continue
		}
		if _, ok := seen[cve]; ok {
			continue
		}
		seen[cve] = struct{}{}
		out = append(out, DetectedCVERow{
			CVE:     cve,
			FoundOn: r.foundOn,
			Source:  r.source,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CVE < out[j].CVE })
	s.putCache(k, out)
	return out, nil
}

// fetchVulnerabilities is shared between breakdown and CVE detection so the
// vulnerability scan runs at most once per cache TTL.
func (s *service) fetchVulnerabilities(ctx context.Context, flowID int64, groupID string) ([]vulnRow, error) {
	// Reuse the vuln cache populated by whichever block ran first.
	k := cacheKey{block: "vulns", flowID: flowID}
	if v, ok := s.fromCache(k); ok {
		if rows, ok := v.([]vulnRow); ok {
			return rows, nil
		}
	}

	out := make([]vulnRow, 0)
	err := s.run(ctx, allVulnerabilitiesQuery, map[string]any{
		"group_id":     groupID,
		"minEdgeTypes": int64(minHostEdgeTypes),
	}, func(rec *neo4j.Record) error {
		out = append(out, vulnRow{
			name:    recStr(rec, "name"),
			summary: recStr(rec, "summary"),
			foundOn: recStr(rec, "foundOn"),
			source:  recStr(rec, "source"),
		})
		return nil
	})
	if err != nil {
		logrus.WithError(err).WithField("flow_id", flowID).Warn("kgdashboard: vulnerabilities read failed")
		return nil, err
	}
	s.putCache(k, out)
	return out, nil
}

// GetToolUsage counts episodes named tool_execution_<tool> per tool.
func (s *service) GetToolUsage(ctx context.Context, flowID int64, groupID string) ([]GraphitiToolUsageRow, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	k := cacheKey{block: "toolusage", flowID: flowID}
	if v, ok := s.fromCache(k); ok {
		return v.([]GraphitiToolUsageRow), nil
	}
	out := make([]GraphitiToolUsageRow, 0)
	err := s.run(ctx, toolUsageQuery, map[string]any{"group_id": groupID}, func(rec *neo4j.Record) error {
		out = append(out, GraphitiToolUsageRow{
			Tool:       recStr(rec, "tool"),
			Executions: int(recInt(rec, "executions")),
		})
		return nil
	})
	if err != nil {
		logrus.WithError(err).WithField("flow_id", flowID).Warn("kgdashboard: tool usage read failed")
		return nil, nil
	}
	s.putCache(k, out)
	return out, nil
}

// GetArtifacts returns artifacts produced during the flow.
func (s *service) GetArtifacts(ctx context.Context, flowID int64, groupID string) ([]ArtifactRow, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	k := cacheKey{block: "artifacts", flowID: flowID}
	if v, ok := s.fromCache(k); ok {
		return v.([]ArtifactRow), nil
	}
	out := make([]ArtifactRow, 0, s.rowLimit)
	err := s.run(ctx, artifactsQuery, map[string]any{
		"group_id": groupID,
		"limit":    int64(s.rowLimit),
	}, func(rec *neo4j.Record) error {
		out = append(out, ArtifactRow{
			Artifact:   recStr(rec, "artifact"),
			ProducedBy: recStr(rec, "producedBy"),
			Summary:    recStr(rec, "summary"),
		})
		return nil
	})
	if err != nil {
		logrus.WithError(err).WithField("flow_id", flowID).Warn("kgdashboard: artifacts read failed")
		return nil, nil
	}
	s.putCache(k, out)
	return out, nil
}

// primaryLabel returns the first non-generic label, or "Entity" if none.
// The attack surface query already drops the generic Entity label via
// filterLabels; this is a defensive fallback.
func primaryLabel(labels []string) string {
	for _, l := range labels {
		if l == "" || l == "Entity" {
			continue
		}
		return l
	}
	if len(labels) > 0 {
		return labels[0]
	}
	return "Entity"
}

// categorizeVuln buckets one vulnerability. CVE id wins; otherwise severity is
// inferred from name + summary keywords; default bucket is Info.
func categorizeVuln(r vulnRow) VulnCategory {
	if extractCVE(r.name) != "" || extractCVE(r.summary) != "" {
		return VulnCategoryCVE
	}
	haystack := strings.ToLower(r.name + " " + r.summary)
	for _, b := range severityKeywords {
		for _, kw := range b.keywords {
			if strings.Contains(haystack, kw) {
				return b.cat
			}
		}
	}
	return VulnCategoryInfo
}

// extractCVE returns the first CVE id found in s, or "".
func extractCVE(s string) string {
	return cveRe.FindString(s)
}
