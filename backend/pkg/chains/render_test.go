package chains

import (
	"strings"
	"testing"
)

func TestChain_Render_StatesTheSafeVerificationContract(t *testing.T) {
	t.Parallel()

	c := Chain{
		ID: "demo", Name: "Demo", Description: "A demo chain.", CVSS: 7.5, Severity: SeverityHigh,
		CVEs: []string{"cve-2020-0001"}, Product: "DemoProduct", Tags: []string{"web"},
		Fingerprint: Fingerprint{Paths: []string{"/x"}, Signals: []string{"a signal"}, Notes: "a note"},
		Links:       []Link{{Name: "step one", CVE: "CVE-2020-0001", Enables: "something", Verify: "read a header"}},
		Remediation: "Patch it.",
	}

	out := c.Render()

	for _, want := range []string{
		"# Exploit chain: Demo (demo)",
		"CVSS 7.5",
		"CVE-2020-0001",
		"## Fingerprint (non-intrusive)",
		"## Safe verification steps (detection only, no destructive payload)",
		"Verify (SAFE): read a header",
		"## Remediation",
		"Patch it.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered chain is missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderMatches_EmptyListSaysSo(t *testing.T) {
	t.Parallel()

	if got := RenderMatches(nil); got != "No exploit chain matches this query." {
		t.Fatalf("unexpected notice: %q", got)
	}
}

func TestRenderMatches_JoinsChainsWithASeparator(t *testing.T) {
	t.Parallel()

	lib, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	out := RenderMatches(lib.All())
	if !strings.Contains(out, "\n---\n") {
		t.Fatalf("expected a separator between rendered chains:\n%s", out)
	}
}
