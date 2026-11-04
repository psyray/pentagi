package skills

import (
	"strings"
	"testing"
)

func TestParseMarkdown_SeparatesFrontmatterAndBody(t *testing.T) {
	fm, body, found, err := parseMarkdown(`---
name: adcs-esc8-ntlm-relay
description: Exploit ESC8 with NTLM relay.
tags: [adcs, esc8, ntlm]
platform: [windows, ad]
---
# ADCS ESC8

Relay the enrollment traffic.
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !found || fm.Name != "adcs-esc8-ntlm-relay" || fm.Description == "" {
		t.Fatalf("frontmatter not read: %+v found=%v", fm, found)
	}
	if len(fm.Tags) != 3 || len(fm.Platform) != 2 {
		t.Fatalf("tags/platform not read: %+v", fm)
	}
	if !strings.HasPrefix(body, "# ADCS ESC8") {
		t.Fatalf("body starts with %q", body[:min(len(body), 30)])
	}
}

func TestParseMarkdown_AcceptsAScalarPlatform(t *testing.T) {
	fm, _, found, err := parseMarkdown(`---
name: htb-example
platform: HTB
tags: [lpd]
---
# Example
`)
	if err != nil || !found {
		t.Fatalf("parse: %v found=%v", err, found)
	}
	if len(fm.Platform) != 1 || fm.Platform[0] != "HTB" {
		t.Fatalf("platform = %v", fm.Platform)
	}
}

func TestParseMarkdown_LeavesPlainMarkdownAlone(t *testing.T) {
	for name, content := range map[string]string{
		"no frontmatter":   "# Guide\n\nbody",
		"unclosed marker":  "---\nname: x\n\n# Guide",
		"windows newlines": "# Guide\r\n\r\nbody",
	} {
		t.Run(name, func(t *testing.T) {
			_, body, found, err := parseMarkdown(content)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if found {
				t.Error("a document without a closed frontmatter was treated as having one")
			}
			if !strings.Contains(body, "Guide") {
				t.Errorf("body lost its content: %q", body)
			}
		})
	}
}

func TestParseMarkdown_RefusesBrokenYAML(t *testing.T) {
	if _, _, _, err := parseMarkdown("---\nname: [\n---\nbody"); err == nil {
		t.Fatal("broken frontmatter was accepted")
	}
}

func TestSlugify_IsStableAndReadable(t *testing.T) {
	for input, want := range map[string]string{
		"ADCS ESC8 / NTLM relay": "adcs-esc8-ntlm-relay",
		"  Déjà vu  ":            "déjà-vu",
		"---":                    "untitled",
		"":                       "untitled",
	} {
		if got := slugify(input); got != want {
			t.Errorf("slugify(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSplitContent_KeepsCodeAndAttributesHeadings(t *testing.T) {
	content := strings.Repeat("# Chapter\n\nA paragraph of text.\n\n", 1) +
		"## Steps\n\n```bash\nnmap -sV target\n```\n\n" +
		strings.Repeat("more text. ", 400)

	chunks, err := splitContent(content, 400, 40)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}

	paths := headingPaths(content, chunks)
	if len(paths) != len(chunks) {
		t.Fatalf("%d paths for %d chunks", len(paths), len(chunks))
	}
	foundSteps := false
	for _, path := range paths {
		if strings.Contains(path, "Steps") {
			foundSteps = true
		}
	}
	if !foundSteps {
		t.Errorf("no chunk is attributed to the Steps heading: %v", paths)
	}
}

func TestExtractSection_ReturnsTheHeadingBody(t *testing.T) {
	content := "# Skill\n\nintro\n\n## Steps\n\nstep one\n\n## Limits\n\nnone\n"

	section, ok := ExtractSection(content, "Steps")
	if !ok {
		t.Fatal("Steps section not found")
	}
	if !strings.Contains(section, "step one") || strings.Contains(section, "Limits") {
		t.Errorf("section body = %q", section)
	}

	if _, ok := ExtractSection(content, "Missing"); ok {
		t.Error("a missing section was found")
	}
}

func TestEmbeddingText_KeepsTheContextAndBoundsTheBytes(t *testing.T) {
	got := embeddingText("Title", "Chapter > Steps", "run certipy", 4096)
	for _, want := range []string{"Title", "Chapter > Steps", "run certipy"} {
		if !strings.Contains(got, want) {
			t.Errorf("embedding text is missing %q: %q", want, got)
		}
	}

	limited := embeddingText("Title", "Chapter", strings.Repeat("я", 100), 40)
	if len(limited) > 40 {
		t.Errorf("bounded text is %d bytes, want at most 40", len(limited))
	}
}

func TestExcerpt_CollapsesWhitespaceAndBoundsTheCard(t *testing.T) {
	got := excerpt("line one\n\nline two\tline three", 100)
	if strings.ContainsAny(got, "\n\t") {
		t.Errorf("excerpt kept layout: %q", got)
	}
	if len(excerpt(strings.Repeat("x", 1000), 50)) != 50 {
		t.Error("excerpt was not bounded")
	}
}
