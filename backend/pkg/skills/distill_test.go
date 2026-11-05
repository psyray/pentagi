package skills

import (
	"strings"
	"testing"
	"time"
)

func TestDetectDistillEvidence_FindsShellsAndFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		output  string
		want    []DistillEvidence
	}{
		{
			name:    "a user shell",
			command: "id",
			output:  "uid=33(www-data) gid=33(www-data) groups=33(www-data)",
			want:    []DistillEvidence{{Kind: "shell", Detail: "uid=33(www-data)"}},
		},
		{
			name:    "a root shell",
			command: "id",
			output:  "uid=0(root) gid=0(root) groups=0(root)",
			want:    []DistillEvidence{{Kind: "root", Detail: "uid=0(root)"}},
		},
		{
			name:    "a flag read through user.txt",
			command: "cat /home/lp/user.txt",
			output:  "0123456789abcdef0123456789abcdef",
			want:    []DistillEvidence{{Kind: "flag", Detail: "1 flag token(s) read"}},
		},
		{
			name:    "a standalone token is a flag",
			command: "cat result",
			output:  "\n0123456789abcdef0123456789abcdef\n",
			want:    []DistillEvidence{{Kind: "flag", Detail: "1 flag token(s) read"}},
		},
		{
			name:    "a token inside a longer line is not a flag",
			command: "cat report.txt",
			output:  "hash: 0123456789abcdef0123456789abcdef (sha256 of the archive)",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DetectDistillEvidence(tt.command, tt.output)
			if len(got) != len(tt.want) {
				t.Fatalf("evidence = %+v, want %+v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("evidence = %+v, want %+v", got, tt.want)
				}
			}
		})
	}
}

func TestDetectDistillEvidence_DeduplicatesRepeatedSignals(t *testing.T) {
	t.Parallel()

	got := DetectDistillEvidence("id", "uid=0(root)\nuid=0(root)")
	if len(got) != 1 || got[0].Kind != "root" {
		t.Fatalf("evidence = %+v", got)
	}
}

func TestBuildDistilledDraft_RendersARedactedDraft(t *testing.T) {
	t.Parallel()

	input := DistillInput{
		FlowID:      195,
		Title:       "Hack WingData machine flags",
		Language:    "en",
		GeneratedAt: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC),
		Tags:        []string{"lpd", "privesc"},
		Steps: []DistillStep{{
			SubtaskID:   1,
			Title:       "Read the user flag",
			Description: "Connect to 10.0.0.5 and read /home/lp/user.txt",
			Result:      "Flag read.",
		}},
		Commands: []DistillCommand{{
			SubtaskID: 1,
			Tool:      "terminal",
			Command:   "ssh lp@10.0.0.5 cat user.txt",
			Output:    "0123456789abcdef0123456789abcdef",
		}},
	}
	replacer := literalReplacer{{from: "10.0.0.5", to: "§*IP*§"}}

	draft := BuildDistilledDraft(input, replacer, []string{"wingdata", "WingData"})

	if draft.Slug != "distilled-flow-195" {
		t.Fatalf("slug = %q", draft.Slug)
	}
	if len(draft.Evidence) != 1 || draft.Evidence[0].Kind != "flag" {
		t.Fatalf("evidence = %+v", draft.Evidence)
	}
	if strings.Contains(draft.Markdown, "0123456789abcdef0123456789abcdef") {
		t.Fatal("the flag leaked into the draft")
	}
	if strings.Contains(draft.Markdown, "WingData") || strings.Contains(draft.Markdown, "wingdata") {
		t.Fatal("the extra redaction did not apply")
	}
	if !strings.Contains(draft.Markdown, "<REDACTED>") || !strings.Contains(draft.Markdown, "§*IP*§") {
		t.Fatalf("redactions missing:\n%s", draft.Markdown)
	}
	if !strings.Contains(draft.Markdown, "## Review checklist") || !strings.Contains(draft.Markdown, "~~~") {
		t.Fatalf("rendering is incomplete:\n%s", draft.Markdown)
	}

	fm, body, hasFrontmatter, err := parseMarkdown(draft.Markdown)
	if err != nil || !hasFrontmatter {
		t.Fatalf("frontmatter: err=%v present=%t", err, hasFrontmatter)
	}
	if fm.Kind != KindSkill || fm.Status != StatusDraft || fm.Visibility != VisibilityPrivate {
		t.Fatalf("frontmatter = %+v", fm)
	}
	if fm.Description != draft.Description || fm.Name != draft.Title {
		t.Fatalf("frontmatter name/description = %q / %q", fm.Name, fm.Description)
	}
	if len(fm.Tags) != 3 || fm.Tags[0] != "distilled" {
		t.Fatalf("tags = %v", fm.Tags)
	}
	if !strings.Contains(body, "Read the user flag") {
		t.Fatalf("body = %q", body)
	}
}

func TestBuildDistilledDraft_KeepsOnlyTheWinningPath(t *testing.T) {
	t.Parallel()

	input := DistillInput{
		FlowID:      42,
		GeneratedAt: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC),
		Steps: []DistillStep{
			{SubtaskID: 1, Title: "Recon", Description: "scan the target"},
			{SubtaskID: 2, Title: "Exploit", Result: "uid=33(www-data)"},
		},
		Commands: []DistillCommand{
			{SubtaskID: 1, Tool: "terminal", Command: "nmap -p- 10.0.0.5", Output: "22/tcp open"},
			{SubtaskID: 1, Tool: "terminal", Command: "gobuster dir -u http://10.0.0.5", Output: "/admin"},
			{SubtaskID: 1, Tool: "terminal", Command: "dirb http://10.0.0.5", Output: "/upload"},
			{SubtaskID: 1, Tool: "terminal", Command: "nikto -h 10.0.0.5", Output: "outdated server"},
			{SubtaskID: 2, Tool: "terminal", Command: "python3 exploit.py 10.0.0.5", Output: "uid=33(www-data)"},
		},
	}
	replacer := literalReplacer{{from: "10.0.0.5", to: "§*IP*§"}}

	draft := BuildDistilledDraft(input, replacer, nil)

	if !strings.Contains(draft.Markdown, "Exploit") || !strings.Contains(draft.Markdown, "python3 exploit.py") {
		t.Fatal("the winning step or command is missing")
	}
	if strings.Contains(draft.Markdown, "nmap -p-") || strings.Contains(draft.Markdown, "gobuster dir") {
		t.Fatalf("a command outside the evidence window leaked:\n%s", draft.Markdown)
	}
	if !strings.Contains(draft.Markdown, "nikto -h") {
		t.Fatal("the context immediately before the win is missing")
	}
	if len(draft.Evidence) != 1 || draft.Evidence[0].Kind != "shell" {
		t.Fatalf("evidence = %+v", draft.Evidence)
	}
	if draft.Commands != 3 {
		t.Fatalf("commands = %d, want 3", draft.Commands)
	}
}

func TestBuildDistilledDraft_KeepsTheObjectiveSubtask(t *testing.T) {
	t.Parallel()

	input := DistillInput{
		FlowID:      43,
		GeneratedAt: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC),
		Steps: []DistillStep{
			{SubtaskID: 7, Title: "Initial access", Evidence: []string{"objective-achieved-evidence", "injection-evidence"}},
			{SubtaskID: 8, Title: "Report"},
		},
		Commands: []DistillCommand{
			{SubtaskID: 7, Tool: "terminal", Command: "curl -d @payload http://10.0.0.5/login", Output: "session expired"},
			{SubtaskID: 8, Tool: "terminal", Command: "writeup", Output: "done"},
		},
	}

	draft := BuildDistilledDraft(input, nil, nil)

	if !strings.Contains(draft.Markdown, "Initial access") {
		t.Fatal("the objective subtask is missing")
	}
	if strings.Contains(draft.Markdown, "Report") {
		t.Fatalf("the report step should not be in the draft:\n%s", draft.Markdown)
	}
	if !strings.Contains(draft.Markdown, "objective-achieved") {
		t.Fatalf("the objective evidence is missing:\n%s", draft.Markdown)
	}
}

func TestBuildDistilledDraft_QuotesATitleWithAColon(t *testing.T) {
	t.Parallel()

	draft := BuildDistilledDraft(DistillInput{
		FlowID:      7,
		Title:       "Reach root: the LPD chain",
		GeneratedAt: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC),
	}, nil, nil)

	fm, _, ok, err := parseMarkdown(draft.Markdown)
	if err != nil || !ok {
		t.Fatalf("frontmatter: err=%v present=%t", err, ok)
	}
	if fm.Name != "Reach root: the LPD chain" {
		t.Fatalf("name = %q", fm.Name)
	}
}

func TestBuildDistilledDraft_HandlesAFlowWithoutCommands(t *testing.T) {
	t.Parallel()

	draft := BuildDistilledDraft(DistillInput{
		FlowID:      3,
		GeneratedAt: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC),
	}, nil, nil)

	if draft.Commands != 0 || len(draft.Evidence) != 0 {
		t.Fatalf("draft = %+v", draft)
	}
	if !strings.Contains(draft.Markdown, "No success signal was detected") {
		t.Fatalf("empty evidence is not explained:\n%s", draft.Markdown)
	}
	if !strings.Contains(draft.Markdown, "### Sequence") {
		t.Fatalf("missing sequence heading:\n%s", draft.Markdown)
	}
}

func TestBuildDistilledDraft_CapsStepsAndCommands(t *testing.T) {
	t.Parallel()

	steps := make([]DistillStep, maxDistillSteps+5)
	for i := range steps {
		steps[i] = DistillStep{SubtaskID: int64(i + 1), Title: "step"}
	}
	commands := make([]DistillCommand, maxDistillCommandsPerStep+3)
	for i := range commands {
		commands[i] = DistillCommand{SubtaskID: 1, Tool: "terminal", Command: "id", Output: "uid=33(www-data)"}
	}

	draft := BuildDistilledDraft(DistillInput{
		FlowID:      9,
		Steps:       steps,
		Commands:    commands,
		GeneratedAt: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC),
	}, nil, nil)

	if count := strings.Count(draft.Markdown, "### "); count > maxDistillSteps+1 {
		t.Fatalf("steps are not capped: %d headings", count)
	}
	if count := strings.Count(draft.Markdown, "Command:"); count > maxDistillCommandsPerStep {
		t.Fatalf("commands are not capped: %d", count)
	}
}
