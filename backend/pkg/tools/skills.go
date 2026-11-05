package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"pentagi/pkg/database"
	"pentagi/pkg/docker"
	"pentagi/pkg/flowfiles"
	"pentagi/pkg/skills"

	"github.com/sirupsen/logrus"
	"github.com/vxcontrol/cloud/anonymizer"
)

// SkillStore is the subset of the skills store the tools need. Keeping it an
// interface lets the handlers be tested without PostgreSQL.
type SkillStore interface {
	List(ctx context.Context, userID int64, filter skills.Filter) ([]skills.Document, error)
	Search(ctx context.Context, userID int64, query string, filter skills.Filter, limit int) ([]skills.SearchHit, error)
	Read(ctx context.Context, userID int64, idOrSlug string) (*skills.Document, []skills.File, error)
	SearchEngagements(ctx context.Context, userID int64, query skills.EngagementQuery) ([]skills.EngagementMatch, error)
}

// SkillEventRecorder persists retrieval events (searched, read) so the store can
// report which documents are suggested, read, or never used. It is optional: a
// store without it keeps the tools working, they just record nothing.
type SkillEventRecorder interface {
	RecordEvent(ctx context.Context, userID int64, event skills.Event) error
}

// maxUsageHits caps the hits kept in one event: usage analysis needs the rank
// and the score of the head of the list, not the whole result.
const maxUsageHits = 10

type skillTool struct {
	userID       int64
	flowID       int64
	taskID       *int64
	subtaskID    *int64
	store        SkillStore
	vslp         VectorStoreLogProvider
	replacer     anonymizer.Replacer
	materializer *skillMaterializer
}

// SkillToolOption customizes the skill tool. Options are variadic so existing
// call sites keep working when the tool gains a capability.
type SkillToolOption func(*skillTool)

// WithSkillMaterializer enables materialize_skill for this tool. Without it the
// tool stays read only and the materialize call explains why it is refused.
func WithSkillMaterializer(m *skillMaterializer) SkillToolOption {
	return func(t *skillTool) {
		t.materializer = m
	}
}

// NewSkillTool builds the read side of the skill layer: list, search and read.
func NewSkillTool(
	userID int64,
	flowID int64,
	taskID, subtaskID *int64,
	store SkillStore,
	vslp VectorStoreLogProvider,
	replacer anonymizer.Replacer,
	opts ...SkillToolOption,
) Tool {
	tool := &skillTool{
		userID:    userID,
		flowID:    flowID,
		taskID:    taskID,
		subtaskID: subtaskID,
		store:     store,
		vslp:      vslp,
		replacer:  replacer,
	}
	for _, opt := range opts {
		opt(tool)
	}
	return tool
}

func (t *skillTool) IsAvailable() bool {
	return t.store != nil
}

func (t *skillTool) Handle(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if t.store == nil {
		return "", fmt.Errorf("skills are not available")
	}

	logger := logrus.WithContext(ctx).WithFields(enrichLogrusFields(t.flowID, t.taskID, t.subtaskID, logrus.Fields{
		"tool": name,
	}))

	switch name {
	case ListSkillsToolName:
		var action ListSkillsAction
		if err := json.Unmarshal(args, &action); err != nil {
			return "", fmt.Errorf("failed to unmarshal %s arguments: %w", name, err)
		}

		filter := skills.Filter{
			Kinds:    nonEmpty(action.Kind),
			Statuses: nonEmpty(action.Status),
			Tags:     action.Tags,
			Limit:    intValue(action.Limit, 20),
		}

		var (
			result string
			query  string
		)
		if strings.TrimSpace(action.Query) != "" {
			query = action.Query
			hits, err := t.store.Search(ctx, t.userID, query, filter, filter.Limit)
			if err != nil {
				return "", err
			}
			t.recordSearch(ctx, name, query, filter, hits)
			result = formatSkillHits(hits)
		} else {
			docs, err := t.store.List(ctx, t.userID, filter)
			if err != nil {
				return "", err
			}
			result = formatSkillList(docs)
		}

		result = clampKnowledgeResult(result, DefaultResultSizeLimit)
		t.log(ctx, name, query, filter, result)
		logger.WithField("result_bytes", len(result)).Debug("listed skills")
		return result, nil

	case SearchSkillsToolName:
		var action SearchSkillsAction
		if err := json.Unmarshal(args, &action); err != nil {
			return "", fmt.Errorf("failed to unmarshal %s arguments: %w", name, err)
		}

		filter := skills.Filter{
			Kinds:    nonEmpty(action.Kind),
			Statuses: nonEmpty(action.Status),
			Tags:     action.Tags,
			Limit:    intValue(action.Limit, 5),
		}
		hits, err := t.store.Search(ctx, t.userID, action.Query, filter, filter.Limit)
		if err != nil {
			return "", err
		}
		t.recordSearch(ctx, name, action.Query, filter, hits)

		result := clampKnowledgeResult(formatSkillHits(hits), DefaultResultSizeLimit)
		t.log(ctx, name, action.Query, filter, result)
		logger.WithFields(logrus.Fields{
			"hits":         len(hits),
			"result_bytes": len(result),
		}).Debug("searched skills")
		return result, nil

	case ReadSkillToolName:
		var action ReadSkillAction
		if err := json.Unmarshal(args, &action); err != nil {
			return "", fmt.Errorf("failed to unmarshal %s arguments: %w", name, err)
		}

		doc, files, err := t.store.Read(ctx, t.userID, action.Slug)
		if err != nil {
			return "", err
		}

		result, err := formatSkillRead(doc, files, action.Section)
		if err != nil {
			return "", err
		}
		result = clampKnowledgeResult(result, DefaultResultSizeLimit)
		t.recordRead(ctx, name, doc, action.Section, len(result))
		t.log(ctx, name, action.Slug, skills.Filter{}, result)
		logger.WithFields(logrus.Fields{
			"slug":         doc.Slug,
			"version":      doc.Version,
			"result_bytes": len(result),
		}).Debug("read skill")
		return result, nil

	case SearchEngagementsToolName:
		var action SearchEngagementsAction
		if err := json.Unmarshal(args, &action); err != nil {
			return "", fmt.Errorf("failed to unmarshal %s arguments: %w", name, err)
		}

		query := skills.EngagementQuery{
			Services: action.Services,
			OS:       action.OS,
			Platform: action.Platform,
			Tags:     action.Tags,
			Limit:    intValue(action.Limit, skills.DefaultEngagementLimit),
		}
		matches, err := t.store.SearchEngagements(ctx, t.userID, query)
		if err != nil {
			return "", err
		}

		result := clampKnowledgeResult(formatEngagementMatches(matches), DefaultResultSizeLimit)
		t.recordEngagementSearch(ctx, name, query, matches)
		t.log(ctx, name, strings.Join(query.Services, ", "), skills.Filter{Tags: query.Tags}, result)
		logger.WithFields(logrus.Fields{
			"matches":      len(matches),
			"result_bytes": len(result),
		}).Debug("searched engagements")
		return result, nil

	case MaterializeSkillToolName:
		var action MaterializeSkillAction
		if err := json.Unmarshal(args, &action); err != nil {
			return "", fmt.Errorf("failed to unmarshal %s arguments: %w", name, err)
		}

		if t.materializer == nil {
			return "", fmt.Errorf("materialize_skill is not available in this flow: no skill cache is mounted")
		}

		doc, files, err := t.store.Read(ctx, t.userID, action.Slug)
		if err != nil {
			return "", err
		}

		materialized, err := t.materializer.materialize(ctx, doc, files, action.Files, bool(action.Overwrite))
		if err != nil {
			return "", err
		}

		result := formatSkillMaterialization(materialized)
		t.recordRead(ctx, name, doc, "", len(result))
		t.log(ctx, name, action.Slug, skills.Filter{}, result)
		logger.WithFields(logrus.Fields{
			"slug":    materialized.Slug,
			"files":   len(materialized.Files),
			"skipped": len(materialized.Skipped),
			"pushed":  materialized.Pushed,
			"bytes":   materialized.TotalBytes,
		}).Debug("materialized skill")
		return result, nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// recordEvent writes one usage event. A metrics failure is logged and dropped:
// retrieval must never fail because the audit table is unavailable.
func (t *skillTool) recordEvent(ctx context.Context, event skills.Event) {
	recorder, ok := t.store.(SkillEventRecorder)
	if !ok {
		return
	}

	flowID := t.flowID
	event.FlowID = &flowID
	event.TaskID = t.taskID
	event.SubtaskID = t.subtaskID

	if err := recorder.RecordEvent(ctx, t.userID, event); err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(enrichLogrusFields(t.flowID, t.taskID, t.subtaskID, logrus.Fields{
			"event": event.Name,
		})).Warn("failed to record skill usage event")
	}
}

func (t *skillTool) recordSearch(ctx context.Context, name, query string, filter skills.Filter, hits []skills.SearchHit) {
	details := map[string]any{
		"tool":       name,
		"query_hash": queryHash(query),
		"hit_count":  len(hits),
		"hits":       searchHitUsage(hits),
	}
	if len(filter.Kinds) > 0 {
		details["kinds"] = filter.Kinds
	}
	if len(filter.Statuses) > 0 {
		details["statuses"] = filter.Statuses
	}
	if len(filter.Tags) > 0 {
		details["tags"] = filter.Tags
	}

	t.recordEvent(ctx, skills.Event{Name: skills.EventSearched, Details: details})
}

func (t *skillTool) recordEngagementSearch(ctx context.Context, name string, query skills.EngagementQuery, matches []skills.EngagementMatch) {
	hits := make([]skillHitUsage, 0, len(matches))
	for i, match := range matches {
		if i >= maxUsageHits {
			break
		}
		hits = append(hits, skillHitUsage{Slug: match.Document.Slug, Rank: i + 1, Score: match.Score})
	}

	details := map[string]any{
		"tool":         name,
		"query_hash":   queryHash(strings.Join(query.Services, ",")),
		"hit_count":    len(matches),
		"hits":         hits,
		"service_list": query.Services,
	}
	if query.OS != "" {
		details["os"] = query.OS
	}
	if query.Platform != "" {
		details["platform"] = query.Platform
	}
	if len(query.Tags) > 0 {
		details["tags"] = query.Tags
	}

	t.recordEvent(ctx, skills.Event{Name: skills.EventSearched, Details: details})
}

func (t *skillTool) recordRead(ctx context.Context, name string, doc *skills.Document, section string, size int) {
	if doc == nil {
		return
	}

	details := map[string]any{
		"tool":    name,
		"slug":    doc.Slug,
		"version": doc.Version,
		"bytes":   size,
	}
	if section != "" {
		details["section"] = section
	}

	t.recordEvent(ctx, skills.Event{DocumentID: doc.ID, Name: skills.EventRead, Details: details})
}

// skillHitUsage is the compact hit kept in an event: the raw query is not stored,
// only its hash, so usage analysis never duplicates target data.
type skillHitUsage struct {
	Slug  string  `json:"slug"`
	Rank  int     `json:"rank"`
	Score float64 `json:"score"`
}

func searchHitUsage(hits []skills.SearchHit) []skillHitUsage {
	limit := len(hits)
	if limit > maxUsageHits {
		limit = maxUsageHits
	}

	usage := make([]skillHitUsage, 0, limit)
	for i, hit := range hits[:limit] {
		usage = append(usage, skillHitUsage{Slug: hit.Document.Slug, Rank: i + 1, Score: hit.Score})
	}

	return usage
}

// queryHash keeps a stable, short fingerprint of a query for grouping without
// storing the query itself.
func queryHash(query string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(query))))
	return hex.EncodeToString(sum[:8])
}

func (t *skillTool) log(ctx context.Context, name, query string, filter skills.Filter, result string) {
	logKnowledgeOperation(ctx, t.vslp, t.replacer, t.taskID, t.subtaskID, map[string]any{
		"tool":     name,
		"kinds":    filter.Kinds,
		"statuses": filter.Statuses,
		"tags":     filter.Tags,
	}, query, database.VecstoreActionTypeRetrieve, result)
}

func nonEmpty(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{value}
}

func intValue(value *Int64, fallback int) int {
	if value == nil {
		return fallback
	}
	return int(*value)
}

func formatSkillList(documents []skills.Document) string {
	if len(documents) == 0 {
		return "no skill or reference is stored for this scope"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Available skills and references (%d)\n\n", len(documents))
	for _, doc := range documents {
		fmt.Fprintf(&b, "- `%s` [%s/%s] %s\n", doc.Slug, doc.Kind, doc.Status, doc.Title)
		if doc.Description != "" {
			fmt.Fprintf(&b, "  %s\n", doc.Description)
		}
		if len(doc.Tags) > 0 {
			fmt.Fprintf(&b, "  tags: %s\n", strings.Join(doc.Tags, ", "))
		}
	}
	b.WriteString("\nUse read_skill(slug=\"...\") for the full procedure.\n")
	return b.String()
}

func formatSkillHits(hits []skills.SearchHit) string {
	if len(hits) == 0 {
		return "no matching skill or reference was found for this scope"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Skill search results (%d)\n\n", len(hits))
	for i, hit := range hits {
		doc := hit.Document
		fmt.Fprintf(&b, "## %d. `%s` (score %.4f)\n", i+1, doc.Slug, hit.Score)
		fmt.Fprintf(&b, "%s [%s/%s]\n", doc.Title, doc.Kind, doc.Status)
		if doc.Description != "" {
			b.WriteString(doc.Description)
			b.WriteString("\n")
		}
		if len(doc.Tags) > 0 {
			fmt.Fprintf(&b, "Tags: %s\n", strings.Join(doc.Tags, ", "))
		}
		if hit.HeadingPath != "" {
			fmt.Fprintf(&b, "Matched section: %s\n", hit.HeadingPath)
		}
		if hit.Excerpt != "" {
			fmt.Fprintf(&b, "Excerpt: %s\n", hit.Excerpt)
		}
		fmt.Fprintf(&b, "Next: read_skill(slug=%q)\n\n", doc.Slug)
	}
	return b.String()
}

func formatEngagementMatches(matches []skills.EngagementMatch) string {
	if len(matches) == 0 {
		return "no past engagement matches this target fingerprint"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Engagement matches (%d)\n\n", len(matches))
	for i, match := range matches {
		doc := match.Document
		if match.Score > 0 {
			fmt.Fprintf(&b, "## %d. `%s` (score %.2f)\n", i+1, doc.Slug, match.Score)
		} else {
			fmt.Fprintf(&b, "## %d. `%s`\n", i+1, doc.Slug)
		}
		fmt.Fprintf(&b, "%s [%s/%s]\n", doc.Title, doc.Kind, doc.Status)
		if doc.Description != "" {
			b.WriteString(doc.Description)
			b.WriteString("\n")
		}

		if fingerprint := engagementFingerprint(match); fingerprint != "" {
			fmt.Fprintf(&b, "Fingerprint: %s\n", fingerprint)
		}
		if len(doc.Tags) > 0 {
			fmt.Fprintf(&b, "Tags: %s\n", strings.Join(doc.Tags, ", "))
		}
		fmt.Fprintf(&b, "Next: read_skill(slug=%q)\n\n", doc.Slug)
	}

	return b.String()
}

// engagementFingerprint renders the engagement metadata on one line, marking the
// services that matched so the agent sees why the card surfaced.
func engagementFingerprint(match skills.EngagementMatch) string {
	doc := match.Document
	parts := make([]string, 0, 5)

	if platform := strings.Join(doc.MetadataStrings("platform"), ", "); platform != "" {
		parts = append(parts, "platform "+platform)
	}
	if os := doc.MetadataString("os"); os != "" {
		parts = append(parts, "os "+os)
	}
	if machine := doc.MetadataString("machine"); machine != "" {
		parts = append(parts, "machine "+machine)
	}

	if services := doc.MetadataStrings("services"); len(services) > 0 {
		matched := make(map[string]struct{}, len(match.MatchedServices))
		for _, service := range match.MatchedServices {
			matched[service] = struct{}{}
		}

		rendered := make([]string, 0, len(services))
		for _, service := range services {
			if _, ok := matched[service]; ok {
				service += "*"
			}
			rendered = append(rendered, service)
		}
		parts = append(parts, "services "+strings.Join(rendered, ", "))
	}

	if outcome := doc.MetadataString("outcome"); outcome != "" {
		parts = append(parts, "outcome "+outcome)
	}

	return strings.Join(parts, " | ")
}

func formatSkillRead(doc *skills.Document, files []skills.File, section string) (string, error) {
	if doc == nil {
		return "", fmt.Errorf("skill not found")
	}

	body := doc.Content
	if strings.TrimSpace(section) != "" {
		extracted, ok := skills.ExtractSection(doc.Content, section)
		if !ok {
			available := make([]string, 0)
			for _, s := range skills.Sections(doc.Content) {
				available = append(available, s.Heading)
			}
			return fmt.Sprintf(
				"section %q not found in `%s`; available sections: %s",
				section, doc.Slug, strings.Join(available, ", "),
			), nil
		}
		body = extracted
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# `%s` v%d [%s/%s/%s]\n\n", doc.Slug, doc.Version, doc.Kind, doc.Status, doc.Visibility)
	fmt.Fprintf(&b, "## %s\n\n", doc.Title)
	if doc.Description != "" {
		b.WriteString(doc.Description)
		b.WriteString("\n\n")
	}
	if len(doc.Tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n\n", strings.Join(doc.Tags, ", "))
	}
	if len(files) > 0 {
		fmt.Fprintf(&b, "Attachments (materialize_skill(slug=%q) copies them into %s/%s/):\n",
			doc.Slug, docker.WorkFolderPathInContainer, path.Join(flowfiles.SkillsDirName, doc.Slug))
		for _, file := range files {
			digest := file.SHA256
			if len(digest) > 12 {
				digest = digest[:12]
			}
			if digest != "" {
				fmt.Fprintf(&b, "- %s (%d bytes, sha256 %s)\n", file.Path, file.Size, digest)
			} else {
				fmt.Fprintf(&b, "- %s (%d bytes)\n", file.Path, file.Size)
			}
		}
		b.WriteString("\n")
	}
	if strings.TrimSpace(section) == "" && len(doc.Content) > DefaultResultSizeLimit/2 {
		if sections := skills.Sections(doc.Content); len(sections) > 0 {
			b.WriteString("Sections (use read_skill with section to load one):\n")
			for _, item := range sections {
				fmt.Fprintf(&b, "- %s\n", item.Heading)
			}
			b.WriteString("\n")
		}
	}
	if strings.TrimSpace(section) != "" {
		fmt.Fprintf(&b, "## Section: %s\n\n", section)
	}
	b.WriteString(body)
	b.WriteString("\n")

	return b.String(), nil
}

func formatSkillMaterialization(result *skillMaterializeResult) string {
	if result == nil {
		return "no skill was materialized"
	}

	dir := path.Join(docker.WorkFolderPathInContainer, result.Directory)

	var b strings.Builder
	fmt.Fprintf(&b, "# Skill `%s` materialized in %s\n\n", result.Slug, dir)
	b.WriteString("Layout:\n")
	fmt.Fprintf(&b, "- %s (skill body)\n", path.Join(dir, skillManifestName))
	for _, file := range result.Files {
		fmt.Fprintf(&b, "- %s\n", path.Join(dir, file))
	}

	if len(result.Skipped) > 0 {
		b.WriteString("\nLeft untouched because they already exist (pass overwrite=true to replace them):\n")
		for _, file := range result.Skipped {
			fmt.Fprintf(&b, "- %s\n", path.Join(dir, file))
		}
	}

	if len(result.Files) == 0 && len(result.Skipped) == 0 {
		b.WriteString("\nThis skill has no attachment. Its procedure is available with read_skill.\n")
		return b.String()
	}

	if result.Pushed {
		fmt.Fprintf(&b, "\nThe files are in the flow container. Run them from that directory, for example: cd %s\n", dir)
		return b.String()
	}

	fmt.Fprintf(&b, "\nThe flow container is not running yet: the files were cached and will be copied to %s when it starts.\n", dir)
	return b.String()
}
