package flowfiles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFiles_SkillsDirectoryIsACacheSource(t *testing.T) {
	dataDir := t.TempDir()
	skillsDir := FlowSkillsDir(dataDir, 13)
	require.NoError(t, os.MkdirAll(filepath.Join(skillsDir, "adcs-esc8"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "adcs-esc8", "relay.py"), []byte("print(1)"), 0644))

	files, err := List(dataDir, 13)
	require.NoError(t, err)
	paths := make([]string, 0, len(files.Files))
	for _, file := range files.Files {
		paths = append(paths, file.Path)
	}
	assert.Contains(t, paths, "skills/adcs-esc8")
	assert.Contains(t, paths, "skills/adcs-esc8/relay.py")

	absPath, err := ResolveCachedPath(dataDir, 13, "skills/adcs-esc8/relay.py")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(skillsDir, "adcs-esc8", "relay.py"), absPath)

	listing := FileListingForPrompt(dataDir, 13)
	assert.Contains(t, listing, `<skills base="/work/skills">`)
	assert.Contains(t, listing, "adcs-esc8/relay.py\n")
}

func TestFiles_SkillsDirectoryRejectsAnEscapingPath(t *testing.T) {
	_, err := ResolveCachedPath(t.TempDir(), 13, "skills/../../escape.txt")
	require.Error(t, err)
}
