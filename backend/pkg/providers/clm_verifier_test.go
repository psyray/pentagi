package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"pentagi/pkg/clmrank"
	"pentagi/pkg/config"
	"pentagi/pkg/providers/pconfig"

	"github.com/sirupsen/logrus"
	"github.com/vxcontrol/langchaingo/llms"
)

func testLogger() *logrus.Entry {
	logger := logrus.New()
	logger.SetLevel(logrus.FatalLevel)
	return logrus.NewEntry(logger)
}

func toolCallChoice(id, name, args string) *llms.ContentChoice {
	return &llms.ContentChoice{
		ToolCalls: []llms.ToolCall{{
			ID:           id,
			Type:         "function",
			FunctionCall: &llms.FunctionCall{Name: name, Arguments: args},
		}},
	}
}

func TestNewCLMVerifierGating(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		if v := newCLMVerifier(&config.Config{}); v != nil {
			t.Error("expected nil verifier when CLMVerifierEnabled=false")
		}
	})

	t.Run("missing server URL disables with warning", func(t *testing.T) {
		v := newCLMVerifier(&config.Config{CLMVerifierEnabled: true})
		if v != nil {
			t.Error("expected nil verifier when CLM_SERVER_URL is empty")
		}
	})

	t.Run("agent gating and N clamping", func(t *testing.T) {
		v := newCLMVerifier(&config.Config{
			CLMVerifierEnabled: true,
			CLMServerURL:       "http://clm.local:8700",
			CLMModel:           "clm-latest",
			CLMBestOfN:         42, // beyond clmMaxBestOfN
			CLMTimeoutSec:      5,
			CLMAgents:          "pentester, primary_agent , ,",
		})
		if v == nil {
			t.Fatal("expected non-nil verifier")
		}
		if v.bestOfN != clmMaxBestOfN {
			t.Errorf("bestOfN = %d, want clamp to %d", v.bestOfN, clmMaxBestOfN)
		}
		if !v.appliesTo(pconfig.OptionsTypePentester) {
			t.Error("pentester should be gated in")
		}
		if !v.appliesTo(pconfig.OptionsTypePrimaryAgent) {
			t.Error("primary_agent should be gated in")
		}
		if v.appliesTo(pconfig.OptionsTypeCoder) {
			t.Error("coder should be gated out")
		}
	})

	t.Run("empty agent list disables", func(t *testing.T) {
		v := newCLMVerifier(&config.Config{
			CLMVerifierEnabled: true,
			CLMServerURL:       "http://clm.local:8700",
			CLMBestOfN:         3,
			CLMAgents:          " , ,",
		})
		if v != nil {
			t.Error("expected nil verifier when no agent type remains after parsing")
		}
	})
}

func TestClipRunes(t *testing.T) {
	if got := clipRunes("short", 100); got != "short" {
		t.Errorf("clipRunes short = %q", got)
	}
	long := ""
	for i := 0; i < 50; i++ {
		long += "é" // multi-byte on purpose
	}
	got := clipRunes(long, 10)
	if utf8.RuneCountInString(got) != 13 { // 10 runes + "..."
		t.Errorf("rune count after clip = %d, want 13", utf8.RuneCountInString(got))
	}
	if !utf8.ValidString(got) {
		t.Error("clipRunes must never produce invalid UTF-8")
	}
	if got := clipRunes("abc", 0); got != "abc" {
		t.Errorf("clipRunes n=0 = %q, want untouched", got)
	}
}

func TestRenderCandidate(t *testing.T) {
	t.Run("tool call rendering", func(t *testing.T) {
		res := &callResult{
			content: "I will scan the target",
			funcCalls: []llms.ToolCall{{
				FunctionCall: &llms.FunctionCall{Name: "terminal", Arguments: `{"input":"nmap -sV 10.10.11.1"}`},
			}},
		}
		got := renderCandidate(res)
		if got == clmNoContentPlaceholder {
			t.Fatal("unexpected placeholder")
		}
		for _, want := range []string{"I will scan the target", "[tool] terminal", "nmap -sV"} {
			if !strings.Contains(got, want) {
				t.Errorf("rendered candidate missing %q: %q", want, got)
			}
		}
	})

	t.Run("empty candidate gets placeholder", func(t *testing.T) {
		if got := renderCandidate(&callResult{}); got != clmNoContentPlaceholder {
			t.Errorf("got %q, want placeholder", got)
		}
	})
}

func TestBuildCLMContext(t *testing.T) {
	chain := []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "Hack the box Paperwork. Current subtask: enumerate web services."}}},
		{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{
			llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: "terminal", Arguments: `{"input":"nmap 10.10.11.1"}`}},
		}},
		{Role: llms.ChatMessageTypeTool, Parts: []llms.ContentPart{
			llms.ToolCallResponse{Name: "terminal", Content: "22/tcp open ssh\n80/tcp open http"},
		}},
	}

	fp := &flowProvider{}
	got := buildCLMContext(chain, fp.getLastHumanMessage(chain))

	for _, want := range []string{"Hack the box Paperwork", "Recent conversation steps", "nmap 10.10.11.1", "80/tcp open http"} {
		if !strings.Contains(got, want) {
			t.Errorf("context missing %q:\n%s", want, got)
		}
	}
}

// newVerifierForTest wires a verifier against a stub clm-serve.
func newVerifierForTest(t *testing.T, handler http.HandlerFunc) *flowProvider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &flowProvider{
		cfg: &config.Config{CLMTimeoutSec: 5},
		clm: &clmVerifier{
			client:  clmrank.New(srv.URL, "key", "clm-latest", 5*time.Second),
			agents:  map[pconfig.ProviderOptionsType]bool{pconfig.OptionsTypePentester: true},
			bestOfN: 3,
		},
	}
}

func rankHandler(t *testing.T, answersOrder []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Answers []string `json:"answers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode rank request: %v", err)
		}
		// rank candidates in the order given by answersOrder
		probs := map[string]float64{}
		p := 0.9
		ranked := []map[string]any{}
		for i, cand := range answersOrder {
			probs[cand] = p
			p -= 0.3
			ranked = append(ranked, map[string]any{"rank": i + 1, "candidate": cand, "prob": probs[cand]})
		}
		w.Header().Set("X-CLM-Latency-Ms", "2.5")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "clm-latest", "ranked": ranked})
	}
}

func TestChooseBestOfN(t *testing.T) {
	chain := []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "Objective: own the box"}}},
	}

	choices := []*llms.ContentChoice{
		toolCallChoice("c0", "terminal", `{"input":"nikto -h http://target"}`),
		toolCallChoice("c1", "terminal", `{"input":"gobuster dir -u http://target"}`),
		toolCallChoice("c2", "terminal", `{"input":"nmap -sV target"}`),
	}

	t.Run("winner is the CLM top candidate", func(t *testing.T) {
		// the stub ranks the LAST submitted answer first; find which render it is:
		// candidates render as "\n[tool] terminal {...}", so rank choice 2 first.
		var captured []string
		fp := newVerifierForTest(t, func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Answers []string `json:"answers"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			captured = req.Answers

			ranked := []map[string]any{}
			// reverse order: last candidate wins
			for i := len(req.Answers) - 1; i >= 0; i-- {
				ranked = append(ranked, map[string]any{
					"rank": len(req.Answers) - i, "candidate": req.Answers[i], "prob": 0.5,
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "clm-latest", "ranked": ranked})
		})

		winner, err := fp.chooseBestOfN(context.Background(), chain, pconfig.OptionsTypePentester, choices, testLogger())
		if err != nil {
			t.Fatalf("chooseBestOfN() error = %v", err)
		}
		if len(captured) != 3 {
			t.Fatalf("clm should have received 3 unique answers, got %d", len(captured))
		}
		// winner must be the candidate whose rendered text was ranked first:
		// the reversed last answer = rendered[2] = choice c2 (nmap).
		if len(winner.funcCalls) != 1 || winner.funcCalls[0].ID != "c2" {
			t.Errorf("winner tool call = %+v, want choice c2", winner.funcCalls)
		}
	})

	t.Run("duplicates are ranked once and map back", func(t *testing.T) {
		dupChoices := []*llms.ContentChoice{
			toolCallChoice("d0", "terminal", `{"input":"id"}`),
			toolCallChoice("d1", "terminal", `{"input":"id"}`), // exact duplicate of d0
			toolCallChoice("d2", "terminal", `{"input":"whoami"}`),
		}

		var captured []string
		fp := newVerifierForTest(t, func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Answers []string `json:"answers"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			captured = req.Answers

			ranked := []map[string]any{}
			for i, ans := range req.Answers {
				ranked = append(ranked, map[string]any{"rank": i + 1, "candidate": ans, "prob": 0.5})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "clm-latest", "ranked": ranked})
		})

		winner, err := fp.chooseBestOfN(context.Background(), chain, pconfig.OptionsTypePentester, dupChoices, testLogger())
		if err != nil {
			t.Fatalf("chooseBestOfN() error = %v", err)
		}
		if len(captured) != 2 {
			t.Errorf("duplicates should be deduped before ranking, got %d answers", len(captured))
		}
		// stub ranks input order, so winner = first candidate (d0)
		if winner.funcCalls[0].ID != "d0" {
			t.Errorf("winner = %s, want d0", winner.funcCalls[0].ID)
		}
	})

	t.Run("rank failure keeps first candidate (fail open)", func(t *testing.T) {
		fp := newVerifierForTest(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`boom`))
		})

		winner, err := fp.chooseBestOfN(context.Background(), chain, pconfig.OptionsTypePentester, choices, testLogger())
		if err != nil {
			t.Fatalf("chooseBestOfN() error = %v, want fail-open success", err)
		}
		if winner.funcCalls[0].ID != "c0" {
			t.Errorf("fail-open winner = %s, want c0 (model's first choice)", winner.funcCalls[0].ID)
		}
	})

	t.Run("single usable choice is returned without ranking", func(t *testing.T) {
		calls := 0
		fp := newVerifierForTest(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusOK)
		})
		single := []*llms.ContentChoice{choices[0], {Content: ""}} // 2nd empty -> skipped

		winner, err := fp.chooseBestOfN(context.Background(), chain, pconfig.OptionsTypePentester, single, testLogger())
		if err != nil {
			t.Fatalf("chooseBestOfN() error = %v", err)
		}
		if winner.funcCalls[0].ID != "c0" {
			t.Errorf("winner = %s, want c0", winner.funcCalls[0].ID)
		}
		if calls != 0 {
			t.Errorf("CLM server should not be called for a single usable candidate, got %d calls", calls)
		}
	})

	t.Run("no usable choice is an error", func(t *testing.T) {
		fp := newVerifierForTest(t, rankHandler(t, nil))
		empty := []*llms.ContentChoice{{Content: ""}, {Content: "   "}}
		if _, err := fp.chooseBestOfN(context.Background(), chain, pconfig.OptionsTypePentester, empty, testLogger()); err == nil {
			t.Fatal("expected error when no choice carries content or tool calls")
		}
	})
}
