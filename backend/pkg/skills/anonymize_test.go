package skills

import (
	"strings"
	"testing"
)

// literalReplacer replaces the pairs in order, which is enough to assert the
// contract without compiling the cloud pattern set.
type literalReplacer []struct{ from, to string }

func (r literalReplacer) ReplaceString(value string) string {
	for _, pair := range r {
		value = strings.ReplaceAll(value, pair.from, pair.to)
	}
	return value
}

func TestRedactDocument_EngagementRedactsTheFlag(t *testing.T) {
	t.Parallel()

	const flag = "0123456789abcdef0123456789abcdef"
	doc := Document{
		Kind:        KindEngagement,
		Title:       "Printer job language on 10.0.0.5",
		Description: "Write-up of 10.0.0.5",
		Content:     "The flag was " + flag + " for user.",
	}
	replacer := literalReplacer{{from: "10.0.0.5", to: "§*IP*§"}}

	got := RedactDocument(doc, replacer)

	if got.Flags != 1 {
		t.Fatalf("flags = %d, want 1", got.Flags)
	}
	if !strings.Contains(got.Content, RedactedFlag) {
		t.Fatalf("content does not carry the placeholder: %q", got.Content)
	}
	if strings.Contains(got.Content, flag) {
		t.Fatalf("content still carries the flag: %q", got.Content)
	}
	if !strings.Contains(got.Title, "§*IP*§") || !strings.Contains(got.Description, "§*IP*§") {
		t.Fatalf("title or description not redacted: %q / %q", got.Title, got.Description)
	}
	if !got.Changed(doc) {
		t.Fatal("changed = false after a redaction")
	}
}

func TestRedactDocument_ReferenceKeepsAHexHash(t *testing.T) {
	t.Parallel()

	const digest = "0123456789abcdef0123456789abcdef"
	doc := Document{
		Kind:    KindReference,
		Title:   "Sample hash",
		Content: "The sha256 prefix is " + digest + ".",
	}

	got := RedactDocument(doc, literalReplacer{{from: "nomatch", to: "x"}})

	if got.Flags != 0 {
		t.Fatalf("flags = %d, want 0", got.Flags)
	}
	if !strings.Contains(got.Content, digest) {
		t.Fatalf("a reference hash must survive: %q", got.Content)
	}
	if got.Changed(doc) {
		t.Fatal("changed = true without any replacement")
	}
}

func TestRedactDocument_NilReplacerIsTheIdentity(t *testing.T) {
	t.Parallel()

	doc := Document{Kind: KindEngagement, Title: "t", Description: "d", Content: "c"}

	got := RedactDocument(doc, nil)

	if got.Title != doc.Title || got.Description != doc.Description || got.Content != doc.Content {
		t.Fatalf("nil replacer changed the document: %+v", got)
	}
	if got.Flags != 0 {
		t.Fatalf("flags = %d, want 0", got.Flags)
	}
}

func TestRedactDocument_EveryFlagIsCounted(t *testing.T) {
	t.Parallel()

	doc := Document{
		Kind:    KindEngagement,
		Title:   "two flags",
		Content: "user 0123456789abcdef0123456789abcdef root fedcba9876543210fedcba9876543210",
	}

	got := RedactDocument(doc, literalReplacer{})

	if got.Flags != 2 {
		t.Fatalf("flags = %d, want 2", got.Flags)
	}
	if count := strings.Count(got.Content, RedactedFlag); count != 2 {
		t.Fatalf("content has %d placeholders, want 2: %q", count, got.Content)
	}
}
