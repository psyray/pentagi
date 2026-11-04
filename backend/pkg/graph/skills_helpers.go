package graph

import (
	"strings"

	"pentagi/pkg/graph/model"
	"pentagi/pkg/skills"
)

func skillFromDocument(doc skills.Document) *model.Skill {
	tags := doc.Tags
	if tags == nil {
		tags = []string{}
	}

	return &model.Skill{
		ID:          doc.ID,
		UserID:      doc.UserID,
		PackID:      doc.PackID,
		Kind:        model.SkillKind(doc.Kind),
		Slug:        doc.Slug,
		Title:       doc.Title,
		Description: doc.Description,
		Content:     doc.Content,
		SourceKind:  doc.SourceKind,
		SourceURI:   doc.SourceURI,
		ContentHash: doc.ContentHash,
		Language:    skillOptionalString(doc.Language),
		Version:     doc.Version,
		Status:      model.SkillStatus(doc.Status),
		Visibility:  model.SkillVisibility(doc.Visibility),
		Quality:     doc.Quality,
		Tags:        tags,
		Metadata:    doc.Metadata,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}
}

func skillFilterFromModel(filter *model.SkillFilter) skills.Filter {
	var out skills.Filter
	if filter == nil {
		return out
	}
	for _, kind := range filter.Kinds {
		out.Kinds = append(out.Kinds, string(kind))
	}
	for _, status := range filter.Statuses {
		out.Statuses = append(out.Statuses, string(status))
	}
	out.Tags = filter.Tags
	return out
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func enumValue[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func enumPtr[T ~string](value *T) *string {
	if value == nil {
		return nil
	}
	result := string(*value)
	return &result
}

// skillOptionalString returns nil for an empty value, so an unset language
// reads as null rather than an empty string. The name is prefixed to avoid a
// collision with reports_helpers.go when this branch is merged into enhanced.
func skillOptionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}
