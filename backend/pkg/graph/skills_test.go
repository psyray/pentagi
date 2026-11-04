package graph

import (
	"testing"
	"time"

	"pentagi/pkg/graph/model"
	"pentagi/pkg/skills"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillFromDocument(t *testing.T) {
	now := time.Now()
	packID := "pack-1"
	sourceURI := "pentagi://legacy/abc"

	got := skillFromDocument(skills.Document{
		ID:          "id-1",
		UserID:      1,
		PackID:      &packID,
		Kind:        skills.KindReference,
		Slug:        "adcs-esc8",
		Title:       "ADCS ESC8",
		Description: "desc",
		Content:     "# body",
		SourceKind:  "legacy",
		SourceURI:   &sourceURI,
		ContentHash: "hash",
		Language:    "en",
		Version:     2,
		Status:      skills.StatusDraft,
		Visibility:  skills.VisibilityPrivate,
		Quality:     3,
		Tags:        []string{"adcs"},
		Metadata:    map[string]any{"k": "v"},
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	require.NotNil(t, got)
	assert.Equal(t, "id-1", got.ID)
	assert.Equal(t, model.SkillKindReference, got.Kind)
	assert.Equal(t, model.SkillStatusDraft, got.Status)
	assert.Equal(t, model.SkillVisibilityPrivate, got.Visibility)
	require.NotNil(t, got.Language)
	assert.Equal(t, "en", *got.Language)
	assert.Equal(t, []string{"adcs"}, got.Tags)
	assert.Equal(t, "v", got.Metadata["k"])
	assert.True(t, got.CreatedAt.Equal(now))
}

func TestSkillFromDocumentNormalizesEmptyCollections(t *testing.T) {
	got := skillFromDocument(skills.Document{
		Kind:       skills.KindSkill,
		Status:     skills.StatusDraft,
		Visibility: skills.VisibilityPrivate,
	})

	require.NotNil(t, got)
	assert.Nil(t, got.Language)
	assert.NotNil(t, got.Tags)
	assert.Empty(t, got.Tags)
}

func TestSkillFilterFromModel(t *testing.T) {
	got := skillFilterFromModel(&model.SkillFilter{
		Kinds:    []model.SkillKind{model.SkillKindReference},
		Statuses: []model.SkillStatus{model.SkillStatusDraft},
		Tags:     []string{"adcs"},
	})

	assert.Equal(t, []string{"reference"}, got.Kinds)
	assert.Equal(t, []string{"draft"}, got.Statuses)
	assert.Equal(t, []string{"adcs"}, got.Tags)
}

func TestSkillFilterFromModelNil(t *testing.T) {
	assert.Equal(t, skills.Filter{}, skillFilterFromModel(nil))
}
