package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func ref(id, slug string) DocumentRef {
	return DocumentRef{ID: id, Slug: slug, Kind: KindSkill}
}

func TestGroupDuplicates_ChainsPairsIntoOneComponent(t *testing.T) {
	pairs := []DuplicatePair{
		{Left: ref("1", "a"), Right: ref("2", "b"), Similarity: 0.95},
		{Left: ref("2", "b"), Right: ref("3", "c"), Similarity: 0.99},
	}

	groups := groupDuplicates(pairs)

	if len(groups) != 1 {
		t.Fatalf("groups = %+v", groups)
	}
	if len(groups[0].Documents) != 3 {
		t.Fatalf("documents = %+v", groups[0].Documents)
	}
	if groups[0].MaxSimilarity != 0.99 {
		t.Fatalf("max similarity = %v", groups[0].MaxSimilarity)
	}
	assert.Equal(t, []string{"a", "b", "c"}, []string{
		groups[0].Documents[0].Slug, groups[0].Documents[1].Slug, groups[0].Documents[2].Slug,
	})
}

func TestGroupDuplicates_KeepsDisjointComponentsApart(t *testing.T) {
	pairs := []DuplicatePair{
		{Left: ref("1", "a"), Right: ref("2", "b"), Similarity: 0.94},
		{Left: ref("3", "c"), Right: ref("4", "d"), Similarity: 0.97},
	}

	groups := groupDuplicates(pairs)

	if len(groups) != 2 {
		t.Fatalf("groups = %+v", groups)
	}
	if groups[0].MaxSimilarity != 0.97 {
		t.Fatalf("the strongest group must come first: %+v", groups)
	}
}

func TestGroupDuplicates_SortsBySizeThenSimilarity(t *testing.T) {
	pairs := []DuplicatePair{
		{Left: ref("1", "a"), Right: ref("2", "b"), Similarity: 0.94},
		{Left: ref("3", "c"), Right: ref("4", "d"), Similarity: 0.99},
		{Left: ref("4", "d"), Right: ref("5", "e"), Similarity: 0.98},
	}

	groups := groupDuplicates(pairs)

	if len(groups) != 2 {
		t.Fatalf("groups = %+v", groups)
	}
	if len(groups[0].Documents) != 3 {
		t.Fatalf("the larger group must come first: %+v", groups)
	}
}

func TestGroupDuplicates_ReturnsNothingForAnEmptyGraph(t *testing.T) {
	assert.Empty(t, groupDuplicates(nil))
}

func TestMergedAttachmentPath_KeepsFilesUnderTheSkillDirectory(t *testing.T) {
	assert.Equal(t, "files/merged/b/probe.py", mergedAttachmentPath("b", "files/probe.py"))
	assert.Equal(t, "files/merged/b/notes.md", mergedAttachmentPath("b", "notes.md"))
}
