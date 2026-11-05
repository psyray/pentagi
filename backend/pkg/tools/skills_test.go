package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pentagi/pkg/database"
	"pentagi/pkg/skills"
)

type fakeSkillStore struct {
	docs        []skills.Document
	hits        []skills.SearchHit
	files       []skills.File
	engagements []skills.EngagementMatch
	err         error
	eventErr    error
	events      []skills.Event

	lastQuery           string
	lastFilter          skills.Filter
	lastEngagementQuery skills.EngagementQuery
}

func (f *fakeSkillStore) List(_ context.Context, _ int64, filter skills.Filter) ([]skills.Document, error) {
	f.lastFilter = filter
	return f.docs, f.err
}

func (f *fakeSkillStore) Search(_ context.Context, _ int64, query string, filter skills.Filter, _ int) ([]skills.SearchHit, error) {
	f.lastQuery = query
	f.lastFilter = filter
	return f.hits, f.err
}

func (f *fakeSkillStore) Read(_ context.Context, _ int64, idOrSlug string) (*skills.Document, []skills.File, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	for i := range f.docs {
		if f.docs[i].Slug == idOrSlug || f.docs[i].ID == idOrSlug {
			return &f.docs[i], f.files, nil
		}
	}
	return nil, nil, skills.ErrNotFound
}

func (f *fakeSkillStore) SearchEngagements(_ context.Context, _ int64, query skills.EngagementQuery) ([]skills.EngagementMatch, error) {
	f.lastEngagementQuery = query
	return f.engagements, f.err
}

func (f *fakeSkillStore) RecordEvent(_ context.Context, _ int64, event skills.Event) error {
	if f.eventErr != nil {
		return f.eventErr
	}
	f.events = append(f.events, event)
	return nil
}

func newSkillDoor(t *testing.T, store SkillStore, log VectorStoreLogProvider) Tool {
	t.Helper()
	taskID, subtaskID := int64(3), int64(4)
	return NewSkillTool(1, 2, &taskID, &subtaskID, store, log, identityReplacer{})
}

func TestSkillTool_List_RendersTheCatalogue(t *testing.T) {
	store := &fakeSkillStore{docs: []skills.Document{{
		ID: "d1", Slug: "adcs-esc8", Kind: skills.KindSkill, Status: skills.StatusCanonical,
		Title: "ADCS ESC8", Description: "Relay HTTP enrollment.", Tags: []string{"adcs", "esc8"},
	}}}
	door := newSkillDoor(t, store, &recordingVectorStoreLog{})

	got, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), ListSkillsToolName,
		mustJSON(map[string]any{"message": "m", "tags": []string{"adcs"}}))
	if err != nil {
		t.Fatalf("list_skills: %v", err)
	}

	for _, want := range []string{"Available skills", "`adcs-esc8`", "Relay HTTP enrollment", "read_skill"} {
		if !strings.Contains(got, want) {
			t.Errorf("catalogue is missing %q:\n%s", want, got)
		}
	}
	if store.lastFilter.Tags[0] != "adcs" {
		t.Errorf("tags filter was not forwarded: %+v", store.lastFilter)
	}
}

func TestSkillTool_List_WithQueryUsesSearch(t *testing.T) {
	store := &fakeSkillStore{hits: []skills.SearchHit{{
		Document: skills.Document{Slug: "lpd-pjl", Kind: skills.KindSkill, Status: skills.StatusReviewed, Title: "PJL FSUPLOAD"},
		Score:    0.75, Excerpt: "send @PJL FSUPLOAD", HeadingPath: "Steps",
	}}}
	door := newSkillDoor(t, store, &recordingVectorStoreLog{})

	got, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), ListSkillsToolName,
		mustJSON(map[string]any{"message": "m", "query": "printer file read"}))
	if err != nil {
		t.Fatalf("list_skills query: %v", err)
	}
	if store.lastQuery != "printer file read" {
		t.Errorf("query reached the store as %q", store.lastQuery)
	}
	if !strings.Contains(got, "PJL FSUPLOAD") || !strings.Contains(got, "send @PJL FSUPLOAD") {
		t.Errorf("search card is incomplete:\n%s", got)
	}
}

func TestSkillTool_Search_ReturnsCardsNotBodies(t *testing.T) {
	store := &fakeSkillStore{hits: []skills.SearchHit{{
		Document: skills.Document{Slug: "ssrf", Kind: skills.KindSkill, Status: skills.StatusDraft, Title: "SSRF out of band"},
		Score:    0.5, Excerpt: "start a callback listener", HeadingPath: "Verification",
	}}}
	door := newSkillDoor(t, store, &recordingVectorStoreLog{})

	got, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), SearchSkillsToolName,
		mustJSON(map[string]any{"message": "m", "query": "ssrf callback"}))
	if err != nil {
		t.Fatalf("search_skills: %v", err)
	}
	if !strings.Contains(got, "Skill search results") || !strings.Contains(got, `read_skill(slug="ssrf")`) {
		t.Errorf("card does not point at the full procedure:\n%s", got)
	}
	if strings.Contains(got, "start a callback listener\n\nstart") {
		t.Errorf("search returned a body: %s", got)
	}
}

func TestSkillTool_Read_ReturnsOneSection(t *testing.T) {
	store := &fakeSkillStore{docs: []skills.Document{{
		ID: "d1", Slug: "adcs-esc8", Kind: skills.KindSkill, Status: skills.StatusCanonical,
		Visibility: skills.VisibilityPrivate, Title: "ADCS ESC8", Version: 2,
		Content: "# ADCS ESC8\n\nintro\n\n## Steps\n\nrun certipy\n\n## Limits\n\nnone\n",
	}}}
	door := newSkillDoor(t, store, &recordingVectorStoreLog{})

	got, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), ReadSkillToolName,
		mustJSON(map[string]any{"message": "m", "slug": "adcs-esc8", "section": "Steps"}))
	if err != nil {
		t.Fatalf("read_skill: %v", err)
	}
	if !strings.Contains(got, "run certipy") || strings.Contains(got, "Limits") || strings.Contains(got, "intro") {
		t.Errorf("section extraction leaked other sections:\n%s", got)
	}
}

func TestSkillTool_Read_ListsAvailableSectionsWhenMissing(t *testing.T) {
	store := &fakeSkillStore{docs: []skills.Document{{
		ID: "d1", Slug: "adcs-esc8", Title: "ADCS ESC8",
		Content: "# ADCS ESC8\n\n## Steps\n\nrun\n\n## Limits\n\nnone\n",
	}}}
	door := newSkillDoor(t, store, &recordingVectorStoreLog{})

	got, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), ReadSkillToolName,
		mustJSON(map[string]any{"message": "m", "slug": "adcs-esc8", "section": "Cleanup"}))
	if err != nil {
		t.Fatalf("read_skill: %v", err)
	}
	if !strings.Contains(got, "not found") || !strings.Contains(got, "Steps") || !strings.Contains(got, "Limits") {
		t.Errorf("missing section answer is not actionable:\n%s", got)
	}
}

func TestSkillTool_Read_ClampsALongBody(t *testing.T) {
	store := &fakeSkillStore{docs: []skills.Document{{
		ID: "d1", Slug: "long", Title: "Long", Content: strings.Repeat("x", DefaultResultSizeLimit*2),
	}}}
	door := newSkillDoor(t, store, &recordingVectorStoreLog{})

	got, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), ReadSkillToolName,
		mustJSON(map[string]any{"message": "m", "slug": "long"}))
	if err != nil {
		t.Fatalf("read_skill: %v", err)
	}
	if !strings.Contains(got, "[knowledge result truncated:") {
		t.Error("a long skill reached the context whole")
	}
}

func TestSkillTool_LogsTheAnonymizedQuery(t *testing.T) {
	store := &fakeSkillStore{hits: []skills.SearchHit{{Document: skills.Document{Slug: "s", Title: "S"}}}}
	log := &recordingVectorStoreLog{}
	taskID, subtaskID := int64(3), int64(4)
	door := NewSkillTool(1, 2, &taskID, &subtaskID, store, log, hostReplacer{})

	_, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), SearchSkillsToolName,
		mustJSON(map[string]any{"message": "m", "query": "scan " + secretHost + " for esc8"}))
	if err != nil {
		t.Fatalf("search_skills: %v", err)
	}
	if len(log.entries) != 1 {
		t.Fatalf("%d log entries, want 1", len(log.entries))
	}
	if strings.Contains(log.entries[0].query, secretHost) || !strings.Contains(log.entries[0].query, "<REDACTED>") {
		t.Errorf("raw query reached the vector store log: %q", log.entries[0].query)
	}
	if log.entries[0].action != database.VecstoreActionTypeRetrieve {
		t.Errorf("skill read logged as %s", log.entries[0].action)
	}
}

func TestSkillTool_SearchEngagements_RendersTheFingerprint(t *testing.T) {
	store := &fakeSkillStore{engagements: []skills.EngagementMatch{{
		Document: skills.Document{
			ID: "m1", Slug: "htb-example", Kind: skills.KindEngagement, Status: skills.StatusReviewed,
			Title: "Printer job language", Tags: []string{"lpd"},
			Metadata: map[string]any{
				"platform": []string{"HTB"},
				"os":       "linux",
				"machine":  "example",
				"services": []string{"22/ssh", "9100/jetdirect"},
				"outcome":  "user",
			},
		},
		Score:           3,
		MatchedServices: []string{"9100/jetdirect"},
	}}}
	door := newSkillDoor(t, store, &recordingVectorStoreLog{})

	got, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), SearchEngagementsToolName,
		mustJSON(map[string]any{
			"message":  "m",
			"services": []string{"9100/JetDirect"},
			"os":       "linux",
			"limit":    3,
		}))
	if err != nil {
		t.Fatalf("search_engagements: %v", err)
	}

	for _, want := range []string{
		"Engagement matches (1)", "`htb-example` (score 3.00)", "9100/jetdirect*",
		"platform HTB", "os linux", "outcome user", `read_skill(slug="htb-example")`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("card misses %q:\n%s", want, got)
		}
	}

	if len(store.lastEngagementQuery.Services) != 1 || store.lastEngagementQuery.Services[0] != "9100/JetDirect" {
		t.Errorf("query services = %v", store.lastEngagementQuery.Services)
	}
	if store.lastEngagementQuery.OS != "linux" || store.lastEngagementQuery.Limit != 3 {
		t.Errorf("query = %+v", store.lastEngagementQuery)
	}
}

func TestSkillTool_SearchEngagements_ExplainsAnEmptyResult(t *testing.T) {
	door := newSkillDoor(t, &fakeSkillStore{}, &recordingVectorStoreLog{})

	got, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), SearchEngagementsToolName,
		mustJSON(map[string]any{"message": "m"}))
	if err != nil {
		t.Fatalf("search_engagements: %v", err)
	}
	if !strings.Contains(got, "no past engagement") {
		t.Fatalf("empty result is not actionable: %q", got)
	}
}

func TestSkillTool_RefusesAnUnknownTool(t *testing.T) {
	door := newSkillDoor(t, &fakeSkillStore{}, &recordingVectorStoreLog{})
	if _, err := door.Handle(context.Background(), "store_skill", nil); err == nil {
		t.Fatal("an unknown tool was accepted")
	}
}

func TestSkillTool_Search_RecordsAUsageEvent(t *testing.T) {
	store := &fakeSkillStore{hits: []skills.SearchHit{
		{Document: skills.Document{ID: "d1", Slug: "lpd-pjl"}, Score: 0.8},
		{Document: skills.Document{ID: "d2", Slug: "ssrf"}, Score: 0.4},
	}}
	door := newSkillDoor(t, store, &recordingVectorStoreLog{})

	_, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), SearchSkillsToolName,
		mustJSON(map[string]any{"message": "m", "query": "printer file read", "kind": "skill"}))
	if err != nil {
		t.Fatalf("search_skills: %v", err)
	}

	if len(store.events) != 1 {
		t.Fatalf("events = %d, want 1", len(store.events))
	}
	event := store.events[0]
	if event.Name != skills.EventSearched {
		t.Errorf("event name = %q", event.Name)
	}
	if event.DocumentID != "" {
		t.Errorf("a search must not be tied to one document, got %q", event.DocumentID)
	}
	if event.FlowID == nil || *event.FlowID != 2 || event.TaskID == nil || *event.TaskID != 3 {
		t.Errorf("event scope = flow %v task %v", event.FlowID, event.TaskID)
	}
	if got := event.Details["hit_count"]; got != 2 {
		t.Errorf("hit_count = %v", got)
	}
	if _, ok := event.Details["query"]; ok {
		t.Error("the raw query must not be stored, only its hash")
	}
	if hash, _ := event.Details["query_hash"].(string); len(hash) != 16 {
		t.Errorf("query_hash = %q", hash)
	}

	hits, ok := event.Details["hits"].([]skillHitUsage)
	if !ok || len(hits) != 2 {
		t.Fatalf("hits = %#v", event.Details["hits"])
	}
	if hits[0].Slug != "lpd-pjl" || hits[0].Rank != 1 || hits[1].Rank != 2 {
		t.Errorf("hits = %+v", hits)
	}
}

func TestSkillTool_Read_RecordsAUsageEvent(t *testing.T) {
	store := &fakeSkillStore{docs: []skills.Document{{
		ID: "d1", Slug: "adcs-esc8", Title: "ADCS ESC8", Version: 2,
		Content: "# ADCS ESC8\n\nrun certipy\n",
	}}}
	door := newSkillDoor(t, store, &recordingVectorStoreLog{})

	_, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), ReadSkillToolName,
		mustJSON(map[string]any{"message": "m", "slug": "adcs-esc8"}))
	if err != nil {
		t.Fatalf("read_skill: %v", err)
	}

	if len(store.events) != 1 {
		t.Fatalf("events = %d, want 1", len(store.events))
	}
	event := store.events[0]
	if event.Name != skills.EventRead || event.DocumentID != "d1" {
		t.Errorf("event = %+v", event)
	}
	if event.Details["slug"] != "adcs-esc8" || event.Details["version"] != 2 {
		t.Errorf("details = %v", event.Details)
	}
}

func TestSkillTool_SearchEngagements_RecordsAUsageEvent(t *testing.T) {
	store := &fakeSkillStore{engagements: []skills.EngagementMatch{{
		Document: skills.Document{ID: "d9", Slug: "htb-example"}, Score: 3,
	}}}
	door := newSkillDoor(t, store, &recordingVectorStoreLog{})

	_, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), SearchEngagementsToolName,
		mustJSON(map[string]any{"message": "m", "services": []string{"9100/JetDirect"}}))
	if err != nil {
		t.Fatalf("search_engagements: %v", err)
	}

	if len(store.events) != 1 {
		t.Fatalf("events = %d, want 1", len(store.events))
	}
	if store.events[0].Name != skills.EventSearched || store.events[0].Details["hit_count"] != 1 {
		t.Errorf("event = %+v", store.events[0])
	}
}

func TestSkillTool_UsageEventFailureDoesNotFailTheCall(t *testing.T) {
	store := &fakeSkillStore{
		docs:     []skills.Document{{ID: "d1", Slug: "adcs-esc8", Title: "ADCS ESC8", Content: "# ADCS\n\nrun\n"}},
		eventErr: errors.New("audit table is down"),
	}
	door := newSkillDoor(t, store, &recordingVectorStoreLog{})

	got, err := door.Handle(PutAgentContext(t.Context(), database.MsgchainTypePentester), ReadSkillToolName,
		mustJSON(map[string]any{"message": "m", "slug": "adcs-esc8"}))
	if err != nil {
		t.Fatalf("a metrics failure must not fail the read: %v", err)
	}
	if !strings.Contains(got, "adcs-esc8") {
		t.Fatalf("read result was not rendered: %q", got)
	}
}
