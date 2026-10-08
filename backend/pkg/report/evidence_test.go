package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"pentagi/pkg/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// evidenceFakeQuerier serves only the per-subtask evidence queries the pack
// builder reads; anything else panics, which is what a test wants to see.
type evidenceFakeQuerier struct {
	database.Querier

	toolcalls map[int64][]database.Toolcall
	termlogs  map[int64][]database.Termlog
	shots     map[int64][]database.Screenshot
}

func (q *evidenceFakeQuerier) GetSubtaskToolcalls(_ context.Context, subtaskID sql.NullInt64) ([]database.Toolcall, error) {
	return q.toolcalls[subtaskID.Int64], nil
}

func (q *evidenceFakeQuerier) GetSubtaskTermLogs(_ context.Context, subtaskID sql.NullInt64) ([]database.Termlog, error) {
	return q.termlogs[subtaskID.Int64], nil
}

func (q *evidenceFakeQuerier) GetSubtaskScreenshots(_ context.Context, subtaskID sql.NullInt64) ([]database.Screenshot, error) {
	return q.shots[subtaskID.Int64], nil
}

func TestBuildEvidencePack_CollectsToolcallsTermlogsAndScreenshots(t *testing.T) {
	q := &evidenceFakeQuerier{
		toolcalls: map[int64][]database.Toolcall{
			11: {{
				ID:     1,
				Name:   "terminal",
				Args:   json.RawMessage(`{"input":"cat /work/results/foothold.txt"}`),
				Result: "lp:x:1000:1000::/home/lp:/bin/sh",
			}},
		},
		termlogs: map[int64][]database.Termlog{
			11: {
				{ID: 7, Type: database.TermlogTypeStdin, Text: "cat /work/results/foothold.txt"},
				{ID: 8, Type: database.TermlogTypeStdout, Text: "uid=0(root) gid=0(root)"},
			},
		},
		shots: map[int64][]database.Screenshot{
			11: {{ID: 12, Name: "foothold as lp"}},
		},
	}

	pack, candidates, err := buildEvidencePack(context.Background(), q, []int64{11}, EvidenceCaps{})
	require.NoError(t, err)

	// stdin is skipped: the command already lives in the toolcall candidate.
	assert.Contains(t, pack, `[E1] toolcall "terminal"`)
	assert.Contains(t, pack, "lp:x:1000:1000")
	assert.Contains(t, pack, "[E2] terminal stdout")
	assert.Contains(t, pack, "uid=0(root)")
	assert.Contains(t, pack, `[E3] screenshot "foothold as lp" (id 12)`)
	assert.NotContains(t, pack, "[E4]")

	require.Len(t, candidates, 3)
	assert.Equal(t, EvidenceRef{Index: 1, Kind: EvidenceKindToolcall, Label: `toolcall "terminal"`, Path: "/work/results/foothold.txt"}, candidates[0])
	assert.Equal(t, EvidenceKindTermlog, candidates[1].Kind)
	assert.Equal(t, EvidenceRef{Index: 3, Kind: EvidenceKindScreenshot, Label: `screenshot "foothold as lp" (id 12)`, ScreenshotID: 12, ScreenshotName: "foothold as lp"}, candidates[2])
}

func TestBuildEvidencePack_DeduplicatesRepeatedCandidates(t *testing.T) {
	toolcall := database.Toolcall{ID: 5, Name: "nmap", Result: "open"}
	q := &evidenceFakeQuerier{
		toolcalls: map[int64][]database.Toolcall{
			1: {toolcall},
			2: {toolcall},
		},
	}

	pack, candidates, err := buildEvidencePack(context.Background(), q, []int64{1, 2}, EvidenceCaps{})
	require.NoError(t, err)

	assert.Equal(t, 1, strings.Count(pack, "[E1]"))
	assert.NotContains(t, pack, "[E2]")
	assert.Len(t, candidates, 1)
}

func TestBuildEvidencePack_CapsItemsBytesAndPack(t *testing.T) {
	toolcalls := make([]database.Toolcall, 0, 5)
	for id := int64(1); id <= 5; id++ {
		toolcalls = append(toolcalls, database.Toolcall{
			ID:     id,
			Name:   "terminal",
			Result: strings.Repeat("A", 200),
		})
	}
	q := &evidenceFakeQuerier{toolcalls: map[int64][]database.Toolcall{1: toolcalls}}

	pack, candidates, err := buildEvidencePack(context.Background(), q, []int64{1}, EvidenceCaps{ItemBytes: 40, MaxItems: 2})
	require.NoError(t, err)

	assert.Len(t, candidates, 2)
	assert.Contains(t, pack, "bytes truncated")
	assert.NotContains(t, pack, "[E3]")
}

func TestBuildEvidencePack_NoSourcesIsEmpty(t *testing.T) {
	pack, candidates, err := buildEvidencePack(context.Background(), &evidenceFakeQuerier{}, nil, EvidenceCaps{})
	require.NoError(t, err)
	assert.Empty(t, pack)
	assert.Empty(t, candidates)
}

func TestResolveCitations_MapsCitedIndexesToTheirCandidates(t *testing.T) {
	candidates := []EvidenceRef{
		{Index: 1, Kind: EvidenceKindToolcall, Label: `toolcall "nmap"`},
		{Index: 2, Kind: EvidenceKindScreenshot, Label: `screenshot "shell"`, ScreenshotID: 12},
		{Index: 3, Kind: EvidenceKindTermlog, Label: "terminal stdout"},
	}

	got := resolveCitations("We saw [E2] then [E1], again [E2] and an unknown [E9].", candidates)

	require.Len(t, got, 2)
	assert.Equal(t, 1, got[0].Index)
	assert.Equal(t, 2, got[1].Index)
	assert.Equal(t, int64(12), got[1].ScreenshotID)
}

func TestEncodeEvidenceRefs_EmptyStaysAValidArray(t *testing.T) {
	blob, err := encodeEvidenceRefs(nil)
	require.NoError(t, err)
	assert.JSONEq(t, "[]", string(blob))

	blob, err = encodeEvidenceRefs([]EvidenceRef{{Index: 1, Kind: EvidenceKindToolcall, Label: `toolcall "nmap"`}})
	require.NoError(t, err)
	assert.Contains(t, string(blob), `"kind":"toolcall"`)
}
