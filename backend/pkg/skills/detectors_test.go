package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDetector_EmbeddedRulesCompile(t *testing.T) {
	detector, err := LoadDetector("")
	require.NoError(t, err)
	require.NotNil(t, detector)

	assert.Equal(t, []string{"adcs-esc8-ntlm-relay"}, detector.Match("PORT 80: /certsrv/ web enrollment"))
	assert.Equal(t, []string{"lpd-pjl-fsupload"}, detector.Match("9100/tcp open  jetdirect"))
	assert.Equal(t, []string{"sudo-nopasswd"}, detector.Match("(root) NOPASSWD: /usr/bin/systemctl"))
	assert.Equal(t, []string{"docker-sock-escape"}, detector.Match("srw-rw---- 1 root docker /var/run/docker.sock"))
	assert.Empty(t, detector.Match("nothing interesting here"))
}

func TestDetector_MatchIsCappedAndDeduplicated(t *testing.T) {
	detector, err := ParseDetector([]byte(`rules:
  - slug: a
    keywords: ["esc8"]
  - slug: b
    keywords: ["jetdirect"]
  - slug: c
    keywords: ["nopasswd"]
  - slug: d
    keywords: ["log4j"]
  - slug: a
    keywords: ["certsrv"]
`))
	require.NoError(t, err)

	matches := detector.Match("esc8 jetdirect nopasswd log4j certsrv")
	assert.Equal(t, []string{"a", "b", "c"}, matches, "rules are capped and a repeated slug appears once")
}

func TestDetector_RegexMatchesOnTheOriginalCase(t *testing.T) {
	detector, err := ParseDetector([]byte(`rules:
  - slug: nopass
    regex: ["\\(\\w*\\)\\s+NOPASSWD:"]
`))
	require.NoError(t, err)

	assert.Equal(t, []string{"nopass"}, detector.Match("(ALL) NOPASSWD: ALL"))
	assert.Empty(t, detector.Match("(all) nopasswd: all"), "the regex is applied to the raw text")
}

func TestParseDetector_RejectsIncompleteRules(t *testing.T) {
	_, err := ParseDetector([]byte("rules:\n  - keywords: [\"x\"]\n"))
	require.Error(t, err)

	_, err = ParseDetector([]byte("rules:\n  - slug: x\n"))
	require.Error(t, err)

	_, err = ParseDetector([]byte("rules:\n  - slug: x\n    regex: [\"(\"]\n"))
	require.Error(t, err)
}

func TestLoadDetector_ReadsAnOverrideFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "detectors.yaml")
	require.NoError(t, os.WriteFile(path, []byte("rules:\n  - slug: custom\n    keywords: [\"only here\"]\n"), 0644))

	detector, err := LoadDetector(path)
	require.NoError(t, err)

	assert.Equal(t, []string{"custom"}, detector.Match("only here"))
	assert.Empty(t, detector.Match("jetdirect"), "the override replaces the embedded rules")
}
