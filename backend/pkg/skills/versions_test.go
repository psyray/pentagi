package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func version(version int, content string) *DocumentVersion {
	return &DocumentVersion{Version: version, Content: content}
}

func TestDiffDocuments_IdenticalContentHasNoPatch(t *testing.T) {
	diff, err := DiffDocuments(version(1, "a\nb\n"), version(2, "a\nb\n"))
	require.NoError(t, err)

	assert.False(t, diff.Changed)
	assert.Empty(t, diff.Diff)
	assert.Equal(t, 1, diff.From)
	assert.Equal(t, 2, diff.To)
}

func TestDiffDocuments_RendersAUnifiedPatch(t *testing.T) {
	before := "# Skill\n\nRun certipy.\n\n## Limits\n\nnone\n"
	after := "# Skill\n\nRun certipy relay.\n\n## Limits\n\nnone\n\n## Cleanup\n\nremove the relay.\n"

	diff, err := DiffDocuments(version(3, before), version(4, after))
	require.NoError(t, err)

	assert.True(t, diff.Changed)
	assert.Contains(t, diff.Diff, "--- v3")
	assert.Contains(t, diff.Diff, "+++ v4")
	assert.Contains(t, diff.Diff, "-Run certipy.")
	assert.Contains(t, diff.Diff, "+Run certipy relay.")
	assert.Contains(t, diff.Diff, "+## Cleanup")
	assert.Equal(t, 1, diff.Removed)
	assert.GreaterOrEqual(t, diff.Added, 2)
}

func TestDiffDocuments_RefusesAMissingRevision(t *testing.T) {
	_, err := DiffDocuments(nil, version(2, "x"))
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestDiffDocuments_RefusesAHugePair(t *testing.T) {
	big := strings.Repeat("x", maxDiffSourceChars+1)

	_, err := DiffDocuments(version(1, big), version(2, "small"))
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestPublishAllowed_FollowsTheReviewLifecycle(t *testing.T) {
	cases := []struct {
		from, to string
		allowed  bool
	}{
		{StatusDraft, StatusReviewed, true},
		{StatusDraft, StatusCanonical, false},
		{StatusReviewed, StatusCanonical, true},
		{StatusReviewed, StatusDraft, true},
		{StatusCanonical, StatusDeprecated, true},
		{StatusCanonical, StatusDraft, false},
		{StatusDeprecated, StatusReviewed, true},
		{StatusDeprecated, StatusCanonical, false},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.allowed, publishAllowed(tc.from, tc.to), "%s -> %s", tc.from, tc.to)
	}
}
