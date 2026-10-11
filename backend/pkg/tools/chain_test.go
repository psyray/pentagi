package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	obs "pentagi/pkg/observability"

	"github.com/sirupsen/logrus"
)

func newExploitChainRig() Tool {
	obs.InitObserver(context.Background(), nil, nil, []logrus.Level{})
	return NewExploitChainTool()
}

func TestExploitChain_IsAvailable(t *testing.T) {
	tool := newExploitChainRig()
	if !tool.IsAvailable() {
		t.Fatal("the embedded exploit-chain library should always be available")
	}
}

func TestExploitChain_Handle_ByCVE(t *testing.T) {
	tool := newExploitChainRig()

	out, err := tool.Handle(context.Background(), SearchExploitChainToolName,
		mustJSON(map[string]any{"cve": "cve-2021-44228", "message": "check log4shell"}))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	for _, want := range []string{"log4shell-jndi-callback", "Safe verification steps", "Verify (SAFE)"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output is missing %q\n%s", want, out)
		}
	}
}

func TestExploitChain_Handle_ById(t *testing.T) {
	tool := newExploitChainRig()

	out, err := tool.Handle(context.Background(), SearchExploitChainToolName,
		mustJSON(map[string]any{"id": "jwt-alg-none-accepted", "message": "jwt"}))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !strings.Contains(out, "jwt-alg-none-accepted") {
		t.Fatalf("by-id lookup should render the chain\n%s", out)
	}
}

func TestExploitChain_Handle_RequiresASelector(t *testing.T) {
	tool := newExploitChainRig()

	_, err := tool.Handle(context.Background(), SearchExploitChainToolName,
		mustJSON(map[string]any{"message": "no selector"}))
	if err == nil {
		t.Fatal("a lookup without any selector must be refused")
	}
}

func TestExploitChain_Handle_NoMatchIsANotice(t *testing.T) {
	tool := newExploitChainRig()

	out, err := tool.Handle(context.Background(), SearchExploitChainToolName,
		mustJSON(map[string]any{"cve": "CVE-0000-0000", "message": "unknown"}))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !strings.Contains(out, "No exploit chain matches") {
		t.Fatalf("a miss should render the notice\n%s", out)
	}
}

func TestExploitChain_Handle_UnknownTool(t *testing.T) {
	tool := newExploitChainRig()

	if _, err := tool.Handle(context.Background(), "not_a_tool", json.RawMessage(`{}`)); err == nil {
		t.Fatal("an unknown tool name must error")
	}
}

func TestSelectExploitChains_IntersectsSelectors(t *testing.T) {
	lib, err := loadExploitChains()
	if err != nil {
		t.Fatalf("load library: %v", err)
	}

	got, err := selectExploitChains(lib, SearchExploitChainAction{Query: "jwt none"})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(got) != 1 || got[0].ID != "jwt-alg-none-accepted" {
		t.Fatalf("query 'jwt none' returned %#v", chainIDs(got))
	}

	got, err = selectExploitChains(lib, SearchExploitChainAction{CVE: "CVE-2021-44228", Tag: "web"})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("conflicting selectors should intersect to nothing, got %#v", chainIDs(got))
	}
}
