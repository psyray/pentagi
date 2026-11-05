package skills

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func treePackFS() fstest.MapFS {
	return fstest.MapFS{
		"pack.yaml": {Data: []byte(`name: web-app
description: Web application playbooks.
version: 1.2.0
license: private
visibility: private
skills:
  - ssrf-out-of-band
  - jwt-alg-none
references:
  - wstg
engagements:
  - htb-paperwork
`)},
		"skills/ssrf-out-of-band/SKILL.md":                {Data: []byte("# SSRF out of band\n\nStart a callback listener.\n")},
		"skills/ssrf-out-of-band/files/ssrf_probe.py":     {Data: []byte("print('probe')\n")},
		"skills/ssrf-out-of-band/files/data/wordlist.txt": {Data: []byte("admin\nroot\n")},
		"skills/jwt-alg-none/SKILL.md":                    {Data: []byte("# JWT alg=none\n\nForge a token.\n")},
		"skills/not-listed/SKILL.md":                      {Data: []byte("# Not listed\n\nShould stay out.\n")},
		"references/wstg/chapter-01.md":                   {Data: []byte("# Chapter one\n\nInjection.\n")},
		"references/wstg/chapter-02.md":                   {Data: []byte("Access control.\n")},
		"references/cheatsheet.md":                        {Data: []byte("# Cheatsheet\n\nShort one.\n")},
		"engagements/htb-paperwork.md":                    {Data: []byte("# Paperwork\n\nuser and root\n")},
		"references/binary.pdf":                           {Data: []byte("%PDF")},
	}
}

func TestManifestIsTree_DistinguishesTheFormats(t *testing.T) {
	assert.True(t, ManifestIsTree([]byte("name: x\nskills: [a]\n")))
	assert.False(t, ManifestIsTree([]byte("name: x\ndocuments:\n  - slug: a\n    file: a.md\n")))
}

func TestParsePackManifest_ValidatesTheFieldsTheImportCannotInvent(t *testing.T) {
	_, err := ParsePackManifest([]byte("description: no name\n"))
	require.Error(t, err)

	_, err = ParsePackManifest([]byte("name: x\nvisibility: secret\n"))
	require.Error(t, err)

	_, err = ParsePackManifest([]byte("name: x\nstatus: archived\n"))
	require.Error(t, err)

	manifest, err := ParsePackManifest([]byte("name: x\nvisibility: shared\nstatus: reviewed\n"))
	require.NoError(t, err)
	assert.Equal(t, "x", manifest.Name)
}

func TestParseTreePack_ReadsSkillsReferencesAndEngagements(t *testing.T) {
	pack, err := ParseTreePack(treePackFS())
	require.NoError(t, err)

	assert.Equal(t, "web-app", pack.Manifest.Name)
	require.Len(t, pack.Documents, 4, "ssrf, jwt, wstg, paperwork; cheatsheet is not listed")

	bySlug := map[string]PackDocument{}
	for _, doc := range pack.Documents {
		bySlug[doc.Slug] = doc
	}

	ssrf := bySlug["ssrf-out-of-band"]
	assert.Equal(t, KindSkill, ssrf.Kind)
	assert.Equal(t, "SSRF out of band", ssrf.Title)
	require.Len(t, ssrf.Files, 2)
	paths := []string{ssrf.Files[0].Path, ssrf.Files[1].Path}
	assert.Contains(t, paths, "files/ssrf_probe.py")
	assert.Contains(t, paths, "files/data/wordlist.txt")

	assert.Equal(t, KindSkill, bySlug["jwt-alg-none"].Kind)
	assert.Equal(t, KindReference, bySlug["wstg"].Kind)
	assert.Contains(t, bySlug["wstg"].Markdown, "# Chapter one")
	assert.Contains(t, bySlug["wstg"].Markdown, "# chapter-02", "a chapter without a heading gets one")
	assert.Equal(t, KindEngagement, bySlug["htb-paperwork"].Kind)

	assert.NotContains(t, bySlug, "not-listed", "the manifest list is an allowlist")
	assert.NotContains(t, bySlug, "cheatsheet", "an unlisted reference is left out")
	assert.NotContains(t, bySlug, "binary", "a binary reference is skipped with a warning")
	assert.NotEmpty(t, pack.Warnings)
}

func TestParseTreePack_WarnsOnAMissingSkillBody(t *testing.T) {
	fsys := fstest.MapFS{
		"pack.yaml":              {Data: []byte("name: x\nskills: [broken]\n")},
		"skills/broken/notes.md": {Data: []byte("# Notes\n")},
	}

	_, err := ParseTreePack(fsys)
	require.Error(t, err)
}

func TestParseTreePack_RefusesAnEmptyPack(t *testing.T) {
	fsys := fstest.MapFS{"pack.yaml": {Data: []byte("name: x\nskills: []\nreferences: [none]\n")}}

	_, err := ParseTreePack(fsys)
	require.Error(t, err)
}

func TestRenderPack_LaysOutTheTreeAndRoundTrips(t *testing.T) {
	manifest := PackManifest{Name: "web-app", Description: "Playbooks.", Version: "1.2.0", Visibility: "private"}
	documents := []Document{
		{ID: "d1", Kind: KindSkill, Slug: "ssrf-out-of-band", Content: "# SSRF\n\nBody.\n"},
		{ID: "d2", Kind: KindReference, Slug: "wstg", Content: "# WSTG\n\nBody.\n"},
		{ID: "d3", Kind: KindEngagement, Slug: "htb-paperwork", Content: "# Paperwork\n\nBody.\n"},
	}
	files := map[string][]PackFile{
		"d1": {{Path: "files/ssrf_probe.py", Content: []byte("print(1)\n")}},
	}

	export := RenderPack(manifest, documents, files)

	assert.Contains(t, export.Files, "pack.yaml")
	assert.Contains(t, export.Files, "skills/ssrf-out-of-band/SKILL.md")
	assert.Contains(t, export.Files, "skills/ssrf-out-of-band/files/ssrf_probe.py")
	assert.Contains(t, export.Files, "references/wstg.md")
	assert.Contains(t, export.Files, "engagements/htb-paperwork.md")

	manifestYAML := string(export.Files["pack.yaml"])
	assert.Contains(t, manifestYAML, "skills:\n    - ssrf-out-of-band")
	assert.Contains(t, manifestYAML, "references:\n    - wstg")
	assert.Contains(t, manifestYAML, "engagements:\n    - htb-paperwork")

	fsys := fstest.MapFS{}
	for name, content := range export.Files {
		fsys[name] = &fstest.MapFile{Data: content}
	}

	roundTrip, err := ParseTreePack(fsys)
	require.NoError(t, err)
	require.Len(t, roundTrip.Documents, 3)

	slugs := []string{roundTrip.Documents[0].Slug, roundTrip.Documents[1].Slug, roundTrip.Documents[2].Slug}
	assert.Contains(t, slugs, "ssrf-out-of-band")
	assert.Contains(t, slugs, "wstg")
	assert.Contains(t, slugs, "htb-paperwork")
}
