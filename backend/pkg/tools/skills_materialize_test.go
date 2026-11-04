package tools

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pentagi/pkg/database"
	"pentagi/pkg/flowfiles"
	"pentagi/pkg/resources"
	"pentagi/pkg/skills"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skillBlob(t *testing.T, dataDir, content string) string {
	t.Helper()

	sum := md5.Sum([]byte(content))
	hash := hex.EncodeToString(sum[:])
	require.NoError(t, resources.EnsureResourcesDir(dataDir))
	require.NoError(t, os.WriteFile(resources.BlobPath(dataDir, hash), []byte(content), 0644))

	return hash
}

func materializeDoor(t *testing.T, store SkillStore, dataDir string, flowID int64) Tool {
	t.Helper()

	taskID, subtaskID := int64(3), int64(4)

	return NewSkillTool(1, flowID, &taskID, &subtaskID, store, &recordingVectorStoreLog{}, identityReplacer{},
		WithSkillMaterializer(newSkillMaterializer(dataDir, flowID, "", nil)))
}

func esc8Store(t *testing.T, dataDir string) *fakeSkillStore {
	t.Helper()

	script := "print('relay')\n"

	return &fakeSkillStore{
		docs: []skills.Document{{
			ID: "d1", Slug: "adcs-esc8", Kind: skills.KindSkill, Status: skills.StatusCanonical,
			Visibility: skills.VisibilityPrivate, Title: "ADCS ESC8", Version: 2,
			Content: "# ADCS ESC8\n\nrun certipy\n",
		}},
		files: []skills.File{{
			Path: "tools/relay.py", MIME: "text/x-python", Size: int64(len(script)),
			SHA256: "deadbeef", BlobRef: skillBlob(t, dataDir, script),
		}},
	}
}

func TestSkillTool_Materialize_WritesTheManifestAndTheAttachments(t *testing.T) {
	dataDir := t.TempDir()
	door := materializeDoor(t, esc8Store(t, dataDir), dataDir, 21)

	got, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), MaterializeSkillToolName,
		mustJSON(map[string]any{"message": "m", "slug": "adcs-esc8"}))
	require.NoError(t, err)

	assert.Contains(t, got, "/work/skills/adcs-esc8/SKILL.md")
	assert.Contains(t, got, "/work/skills/adcs-esc8/tools/relay.py")
	assert.Contains(t, got, "cached")

	flowSkillsDir := flowfiles.FlowSkillsDir(dataDir, 21)
	script, err := os.ReadFile(filepath.Join(flowSkillsDir, "adcs-esc8", "tools", "relay.py"))
	require.NoError(t, err)
	assert.Equal(t, "print('relay')\n", string(script))

	manifest, err := os.ReadFile(filepath.Join(flowSkillsDir, "adcs-esc8", "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(manifest), "run certipy")
}

func TestSkillTool_Materialize_KeepsAnEditedFileUnlessOverwriteIsSet(t *testing.T) {
	dataDir := t.TempDir()
	door := materializeDoor(t, esc8Store(t, dataDir), dataDir, 22)
	ctx := PutAgentContext(t.Context(), database.MsgchainTypePentester)

	_, err := door.Handle(ctx, MaterializeSkillToolName, mustJSON(map[string]any{"message": "m", "slug": "adcs-esc8"}))
	require.NoError(t, err)

	scriptPath := filepath.Join(flowfiles.FlowSkillsDir(dataDir, 22), "adcs-esc8", "tools", "relay.py")
	require.NoError(t, os.WriteFile(scriptPath, []byte("# edited by the agent\n"), 0644))

	got, err := door.Handle(ctx, MaterializeSkillToolName, mustJSON(map[string]any{"message": "m", "slug": "adcs-esc8"}))
	require.NoError(t, err)
	assert.Contains(t, got, "Left untouched")

	kept, err := os.ReadFile(scriptPath)
	require.NoError(t, err)
	assert.Equal(t, "# edited by the agent\n", string(kept))

	_, err = door.Handle(ctx, MaterializeSkillToolName,
		mustJSON(map[string]any{"message": "m", "slug": "adcs-esc8", "overwrite": true}))
	require.NoError(t, err)

	restored, err := os.ReadFile(scriptPath)
	require.NoError(t, err)
	assert.Equal(t, "print('relay')\n", string(restored))
}

func TestSkillTool_Materialize_RefusesAnUnknownAttachment(t *testing.T) {
	dataDir := t.TempDir()
	door := materializeDoor(t, esc8Store(t, dataDir), dataDir, 23)

	_, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), MaterializeSkillToolName,
		mustJSON(map[string]any{"message": "m", "slug": "adcs-esc8", "files": []string{"tools/other.py"}}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown attachment")
}

func TestSkillTool_Materialize_RefusesWithoutAMaterializer(t *testing.T) {
	door := newSkillDoor(t, &fakeSkillStore{}, &recordingVectorStoreLog{})

	_, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), MaterializeSkillToolName,
		mustJSON(map[string]any{"message": "m", "slug": "adcs-esc8"}))
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "not available"), err.Error())
}

func TestSkillTool_Read_PointsAtMaterializeForAttachments(t *testing.T) {
	store := &fakeSkillStore{
		docs: []skills.Document{{
			ID: "d1", Slug: "adcs-esc8", Title: "ADCS ESC8", Content: "# ADCS ESC8\n\nrun\n",
		}},
		files: []skills.File{{
			Path: "tools/relay.py", Size: 12, SHA256: "0123456789abcdef",
		}},
	}
	door := newSkillDoor(t, store, &recordingVectorStoreLog{})

	got, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), ReadSkillToolName,
		mustJSON(map[string]any{"message": "m", "slug": "adcs-esc8"}))
	require.NoError(t, err)

	assert.Contains(t, got, "materialize_skill")
	assert.Contains(t, got, "tools/relay.py (12 bytes, sha256 0123456789ab)")
}
