package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"pentagi/pkg/database"
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
}

type skillTool struct {
	userID    int64
	flowID    int64
	taskID    *int64
	subtaskID *int64
	store     SkillStore
	vslp      VectorStoreLogProvider
	replacer  anonymizer.Replacer
}

// NewSkillTool builds the read side of the skill layer: list, search and read.
func NewSkillTool(
	userID int64,
	flowID int64,
	taskID, subtaskID *int64,
	store SkillStore,
	vslp VectorStoreLogProvider,
	replacer anonymizer.Replacer,
) Tool {
	return &skillTool{
		userID:    userID,
		flowID:    flowID,
		taskID:    taskID,
		subtaskID: subtaskID,
		store:     store,
		vslp:      vslp,
		replacer:  replacer,
	}
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
		t.log(ctx, name, action.Slug, skills.Filter{}, result)
		logger.WithFields(logrus.Fields{
			"slug":         doc.Slug,
			"version":      doc.Version,
			"result_bytes": len(result),
		}).Debug("read skill")
		return result, nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
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
		b.WriteString("Attachments:\n")
		for _, file := range files {
			fmt.Fprintf(&b, "- %s (%d bytes)\n", file.Path, file.Size)
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
