package tools

import (
	"strings"
	"testing"
	"unicode/utf8"

	"pentagi/pkg/database"

	"github.com/vxcontrol/langchaingo/schema"
)

func TestKnowledgeFingerprint_IgnoresLayoutOnly(t *testing.T) {
	base := "Question:\nhow?\n\nGuide:\nstep one\n\nstep two"
	same := "Question:\r\nhow?  \r\n\r\n\r\nGuide:\r\nstep one\t\r\n\r\nstep two   "

	if got, want := knowledgeFingerprint(same), knowledgeFingerprint(base); got != want {
		t.Errorf("layout-only edits changed the fingerprint: %s != %s", got, want)
	}
	if knowledgeFingerprint("Question:\nHOW?\n\nGuide:\nstep one") == knowledgeFingerprint(base) {
		t.Error("case is meaningful, and the fingerprint must keep it")
	}
	if knowledgeFingerprint("Question:\nhow?\n\nGuide:\nstep one") == knowledgeFingerprint(base) {
		t.Error("a missing step is a different document")
	}
}

func TestKnowledgeScopeFilters_PrivateKeepsTheCaller(t *testing.T) {
	if got := knowledgeScopeFilters(7, true); len(got) != 0 {
		t.Errorf("shared scope added filters: %v", got)
	}

	got := knowledgeScopeFilters(7, false)
	if got["user_id"] != "7" {
		t.Errorf("private scope filtered on %v, want user_id 7", got)
	}
}

func TestClampKnowledgeResult_CutsBeforeTheLimitAndSaysSo(t *testing.T) {
	short := "a guide that fits"
	if got := clampKnowledgeResult(short, 4096); got != short {
		t.Errorf("clamped a result that already fit: %q", got)
	}

	limit := 128
	content := strings.Repeat("я", 100) // two bytes per rune: a cut at 128 would split one.
	got := clampKnowledgeResult(content, limit)

	if len(got) <= limit {
		t.Fatalf("the marker did not survive the clamp: %d bytes", len(got))
	}
	if !strings.Contains(got, "[knowledge result truncated:") {
		t.Errorf("the agent cannot tell the result was cut: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Error("the clamp split a multi-byte character")
	}
}

// exactStoredGuide is the composed document store_guide writes for q/g.
const exactStoredGuide = "Question:\nq\n\nGuide:\ng"

func TestGuide_Handle_SkipsAnExactDuplicate(t *testing.T) {
	rig := newKnowledgeRig(t)
	rig.conn.results = []vectorResult{{docs: []schema.Document{{
		PageContent: exactStoredGuide,
		Score:       1,
		Metadata: map[string]any{
			"question":     "q",
			"guide_type":   "use",
			"content_hash": knowledgeFingerprint(exactStoredGuide),
		},
	}}}}

	got := rig.handle(t, guideDoor(rig, identityReplacer{}, 0), StoreGuideToolName, mustJSON(map[string]any{
		"guide": "g", "message": "m", "question": "q", "type": "use",
	}))

	if !strings.Contains(got, "identical guide already exists") {
		t.Errorf("store_guide answered %q", got)
	}
	if len(rig.conn.added) != 0 || rig.db.document != "" {
		t.Errorf("a duplicate was still written: %d vector rows, %.60q in the database",
			len(rig.conn.added), rig.db.document)
	}
	if len(rig.known.created) != 0 {
		t.Errorf("a skipped duplicate was announced to the knowledge page")
	}
	if len(rig.log.entries) != 1 || rig.log.entries[0].action != database.VecstoreActionTypeStore {
		t.Errorf("the skip did not reach the vector store log: %+v", rig.log.entries)
	}
}

func TestGuide_Handle_SkipsANearDuplicate(t *testing.T) {
	rig := newKnowledgeRig(t)
	rig.conn.results = []vectorResult{{docs: []schema.Document{{
		PageContent: "an earlier guide",
		Score:       0.95,
		Metadata:    map[string]any{"question": "how do I do the same thing?", "guide_type": "use"},
	}}}}

	got := rig.handle(t, guideDoor(rig, identityReplacer{}, 0), StoreGuideToolName, mustJSON(map[string]any{
		"guide": "g", "message": "m", "question": "q", "type": "use",
	}))

	if !strings.Contains(got, "a similar guide already exists") {
		t.Errorf("store_guide answered %q", got)
	}
	if len(rig.conn.added) != 0 || rig.db.document != "" {
		t.Error("a near-duplicate was still written")
	}
}

func TestGuide_Handle_StoresWhenTheClosestDocumentIsUnrelated(t *testing.T) {
	rig := newKnowledgeRig(t)
	rig.conn.results = []vectorResult{{docs: []schema.Document{{
		PageContent: "an unrelated guide",
		Score:       0.5,
		Metadata:    map[string]any{"question": "something else", "guide_type": "use"},
	}}}}

	got := rig.handle(t, guideDoor(rig, identityReplacer{}, 0), StoreGuideToolName, mustJSON(map[string]any{
		"guide": "g", "message": "m", "question": "q", "type": "use",
	}))

	if got != "guide stored successfully" {
		t.Errorf("store_guide answered %q", got)
	}
	_, stored, meta := rig.stored(t)
	if stored != exactStoredGuide {
		t.Errorf("stored %q, want %q", stored, exactStoredGuide)
	}
	if meta["content_hash"] != knowledgeFingerprint(exactStoredGuide) {
		t.Error("the stored document carries no fingerprint for the next lookup")
	}
}

func TestGuide_Handle_ScopesDeduplicationAndSearchToTheCaller(t *testing.T) {
	rig := newKnowledgeRig(t)
	taskID, subtaskID := int64(3), int64(4)
	door := NewGuideTool(1, 2, &taskID, &subtaskID, identityReplacer{}, rig.store, rig.doorEmbedder,
		rig.db, 0, rig.log, rig.known, WithSharedKnowledgeSearch(false))

	rig.handle(t, door, StoreGuideToolName, mustJSON(map[string]any{
		"guide": "g", "message": "m", "question": "q", "type": "use",
	}))
	rig.handle(t, door, SearchGuideToolName, mustJSON(map[string]any{
		"questions": []string{"q"}, "message": "m", "type": "use",
	}))

	for i, label := range []string{"deduplication", "search"} {
		filters := rig.conn.search(t, i).filters
		if filters["user_id"] != "1" {
			t.Errorf("%s filtered on %v, want user_id 1", label, filters)
		}
	}
}

func TestGuide_Handle_LogsAMiss(t *testing.T) {
	rig := newKnowledgeRig(t)
	rig.conn.results = []vectorResult{{}}

	got := rig.handle(t, guideDoor(rig, identityReplacer{}, 0), SearchGuideToolName, mustJSON(map[string]any{
		"questions": []string{"q"}, "message": "m", "type": "use",
	}))

	if got != guideNotFoundMessage {
		t.Fatalf("search_guide answered %q", got)
	}
	if len(rig.log.entries) != 1 {
		t.Fatalf("%d log entries for an empty search, want 1", len(rig.log.entries))
	}
	entry := rig.log.entries[0]
	if entry.action != database.VecstoreActionTypeRetrieve || entry.result != guideNotFoundMessage {
		t.Errorf("the miss was logged as %s / %q", entry.action, entry.result)
	}
}

func TestGuide_Handle_ClampsAnOversizeRetrieval(t *testing.T) {
	rig := newKnowledgeRig(t)
	rig.conn.results = []vectorResult{{docs: []schema.Document{{
		PageContent: strings.Repeat("x", DefaultResultSizeLimit*2),
		Score:       0.9,
		Metadata:    map[string]any{"question": "q", "guide_type": "use"},
	}}}}

	got := rig.handle(t, guideDoor(rig, identityReplacer{}, 0), SearchGuideToolName, mustJSON(map[string]any{
		"questions": []string{"q"}, "message": "m", "type": "use",
	}))

	if !strings.Contains(got, "[knowledge result truncated:") {
		t.Fatal("an oversize result reached the agent whole")
	}
	if len(got) >= DefaultResultSizeLimit*2 {
		t.Errorf("clamped result is still %d bytes", len(got))
	}
}

func TestSearchAndCode_Handle_SkipDuplicates(t *testing.T) {
	t.Run("answer", func(t *testing.T) {
		rig := newKnowledgeRig(t)
		rig.conn.results = []vectorResult{{docs: []schema.Document{{
			PageContent: "the answer",
			Score:       1,
			Metadata: map[string]any{
				"question":     "q",
				"answer_type":  "tool",
				"content_hash": knowledgeFingerprint("the answer"),
			},
		}}}}

		got := rig.handle(t, searchDoor(rig, identityReplacer{}, 0), StoreAnswerToolName, mustJSON(map[string]any{
			"answer": "the answer", "message": "m", "question": "q", "type": "tool",
		}))

		if !strings.Contains(got, "identical answer already exists") {
			t.Errorf("store_answer answered %q", got)
		}
		if len(rig.conn.added) != 0 || rig.db.document != "" {
			t.Error("a duplicate answer was still written")
		}
	})

	t.Run("code", func(t *testing.T) {
		rig := newKnowledgeRig(t)
		rig.conn.results = []vectorResult{{docs: []schema.Document{{
			PageContent: "the code",
			Score:       0.96,
			Metadata:    map[string]any{"question": "a similar snippet", "code_lang": "python"},
		}}}}

		got := rig.handle(t, codeDoor(rig, identityReplacer{}, 0), StoreCodeToolName, mustJSON(map[string]any{
			"code": "print(1)", "message": "m", "question": "q", "lang": "python",
			"explanation": "e", "description": "d",
		}))

		if !strings.Contains(got, "a similar code sample already exists") {
			t.Errorf("store_code answered %q", got)
		}
		if len(rig.conn.added) != 0 || rig.db.document != "" {
			t.Error("a near-duplicate code sample was still written")
		}
	})
}
