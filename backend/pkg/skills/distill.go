package skills

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// DistillSourceKind marks a document produced by flow distillation.
	DistillSourceKind = "distilled"

	maxDistillSteps           = 20
	maxDistillCommandsPerStep = 10
	maxDistillTotalCommands   = 40
	maxDistillDescription     = 500
	maxDistillStepText        = 700
	maxDistillOutput          = 900
	distillEvidenceWindow     = 2
)

var (
	distillFlagToken   = regexp.MustCompile(`\b[0-9a-fA-F]{32}\b`)
	distillShellLine   = regexp.MustCompile(`uid=(\d+)\(([^)]*)\)`)
	distillFlagContext = regexp.MustCompile(`(?i)\b(flag|user\.txt|root\.txt|capture)\b`)
)

// DistillEvidence is one success signal found in a flow.
type DistillEvidence struct {
	Kind   string // flag, shell, root or objective
	Detail string
}

// DistillCommand is one command executed in the flow, with its output.
type DistillCommand struct {
	SubtaskID int64
	Tool      string
	Command   string
	Output    string
}

// DistillStep is one subtask of the flow.
type DistillStep struct {
	SubtaskID   int64
	Title       string
	Description string
	Result      string
	Evidence    []string
}

// DistillInput is everything the renderer needs about a flow.
type DistillInput struct {
	FlowID      int64
	Title       string
	Language    string
	Steps       []DistillStep
	Commands    []DistillCommand
	Tags        []string
	GeneratedAt time.Time
}

// DistilledDraft is a skill draft built from a flow. It is never published.
type DistilledDraft struct {
	Slug        string
	Title       string
	Description string
	Markdown    string
	Evidence    []DistillEvidence
	Commands    int
}

// DetectDistillEvidence reports the success signals of one command/output pair.
// A shell prompt and a root prompt are unambiguous. A flag needs context: the
// command must mention a flag or a flag file, or the output must contain a 32
// character hexadecimal token alone on its line, which is how cat user.txt
// returns it. A bare token in a longer output is not treated as a flag.
func DetectDistillEvidence(command, output string) []DistillEvidence {
	var evidence []DistillEvidence
	seen := make(map[string]struct{})

	add := func(current DistillEvidence) {
		key := current.Kind + "|" + current.Detail
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		evidence = append(evidence, current)
	}

	for _, match := range distillShellLine.FindAllStringSubmatch(output, -1) {
		kind := "shell"
		if match[1] == "0" {
			kind = "root"
		}
		add(DistillEvidence{Kind: kind, Detail: "uid=" + match[1] + "(" + match[2] + ")"})
	}

	if distillFlagContext.MatchString(command) || hasStandaloneFlagLine(output) {
		count := len(distillFlagToken.FindAllString(output, -1))
		if count > 0 {
			add(DistillEvidence{
				Kind:   "flag",
				Detail: fmt.Sprintf("%d flag token(s) read", count),
			})
		}
	}

	return evidence
}

func hasStandaloneFlagLine(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) == 32 && distillFlagToken.MatchString(trimmed) {
			return true
		}
	}
	return false
}

// BuildDistilledDraft renders a flow into a draft skill. The target name and
// any internal identifier must be passed as extra reductions: the generic
// anonymiser knows IPv4, domains, URLs and credentials, not the box name.
//
// The draft keeps the winning path, not the whole flow: around every success
// signal it keeps a small window of commands, plus the end of the subtasks the
// harness marked objective-achieved. A flow without any signal is only rendered
// when the caller forces it, and then keeps the first commands.
func BuildDistilledDraft(input DistillInput, replacer Replacer, extraRedactions []string) *DistilledDraft {
	generatedAt := input.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	evidence, winningSteps, keepIndexes := selectDistillPath(input.Steps, input.Commands)
	hasEvidence := len(evidence) > 0

	rendered := make([]DistillCommand, 0, len(input.Commands))
	for _, command := range input.Commands {
		rendered = append(rendered, DistillCommand{
			SubtaskID: command.SubtaskID,
			Tool:      command.Tool,
			Command:   redactDistillText(command.Command, replacer, extraRedactions),
			Output:    redactDistillText(command.Output, replacer, extraRedactions),
		})
	}

	selectedCommands := make([]DistillCommand, 0)
	selectedSteps := make([]DistillStep, 0)
	if hasEvidence {
		for i, command := range rendered {
			if keepIndexes[i] {
				selectedCommands = append(selectedCommands, command)
			}
		}
		for _, step := range input.Steps {
			if !winningSteps[step.SubtaskID] && !stepHasSelectedCommand(step.SubtaskID, selectedCommands) {
				continue
			}
			selectedSteps = append(selectedSteps, step)
		}
	} else {
		if len(rendered) > maxDistillTotalCommands {
			rendered = rendered[:maxDistillTotalCommands]
		}
		selectedCommands = rendered
		selectedSteps = input.Steps
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = fmt.Sprintf("Distilled flow %d", input.FlowID)
	}
	title = truncateRunes(redactDistillText(title, replacer, extraRedactions), maxTitleLen)

	description := truncateRunes(fmt.Sprintf(
		"Draft distilled from flow %d: %d evidence signal(s) and %d command(s). Review before publishing.",
		input.FlowID, len(evidence), len(selectedCommands),
	), maxDistillDescription)

	slug := fmt.Sprintf("distilled-flow-%d", input.FlowID)
	language := normaliseDistillLanguage(input.Language)

	tags := uniqueStrings(append([]string{"distilled"}, input.Tags...))
	body := renderDistillBody(input, title, evidence, selectedSteps, selectedCommands, replacer, extraRedactions, generatedAt)

	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", yamlScalar(title))
	fmt.Fprintf(&b, "description: %s\n", yamlScalar(description))
	fmt.Fprintf(&b, "kind: %s\n", KindSkill)
	fmt.Fprintf(&b, "status: %s\n", StatusDraft)
	fmt.Fprintf(&b, "visibility: %s\n", VisibilityPrivate)
	fmt.Fprintf(&b, "language: %s\n", language)
	fmt.Fprintf(&b, "source: pentagi://flow/%d\n", input.FlowID)
	b.WriteString("anonymized: true\n")
	if len(tags) > 0 {
		b.WriteString("tags:\n")
		for _, tag := range tags {
			fmt.Fprintf(&b, "  - %s\n", yamlScalar(tag))
		}
	}
	b.WriteString("---\n\n")
	b.WriteString(body)

	return &DistilledDraft{
		Slug:        slug,
		Title:       title,
		Description: description,
		Markdown:    b.String(),
		Evidence:    evidence,
		Commands:    len(selectedCommands),
	}
}

// selectDistillPath returns the evidence signals, the winning subtasks and the
// indexes of the commands to keep. Without any signal it returns no index, and
// the caller falls back to the first commands.
func selectDistillPath(steps []DistillStep, commands []DistillCommand) ([]DistillEvidence, map[int64]bool, map[int]bool) {
	evidence := make([]DistillEvidence, 0)
	winning := make(map[int64]bool)
	commandEvidence := make([][]DistillEvidence, len(commands))

	for i, command := range commands {
		items := DetectDistillEvidence(command.Command, command.Output)
		commandEvidence[i] = items
		for _, item := range items {
			evidence = appendUniqueEvidence(evidence, item)
		}
	}

	for _, step := range steps {
		items := DetectDistillEvidence("", step.Result)
		for _, tag := range step.Evidence {
			if strings.Contains(strings.ToLower(tag), "objective-achieved") {
				items = append(items, DistillEvidence{
					Kind:   "objective",
					Detail: fmt.Sprintf("subtask %d marked objective-achieved", step.SubtaskID),
				})
			}
		}
		if len(items) > 0 {
			winning[step.SubtaskID] = true
		}
		for _, item := range items {
			evidence = appendUniqueEvidence(evidence, item)
		}
	}

	keep := make(map[int]bool)
	if len(evidence) == 0 {
		return evidence, winning, keep
	}

	for i := range commands {
		if len(commandEvidence[i]) == 0 {
			continue
		}
		low, high := i-distillEvidenceWindow, i+distillEvidenceWindow
		if low < 0 {
			low = 0
		}
		if high >= len(commands) {
			high = len(commands) - 1
		}
		for j := low; j <= high; j++ {
			keep[j] = true
		}
	}

	// The end of a winning subtask often holds the verification command that the
	// harness counted as the objective, so keep its last commands too.
	for _, step := range steps {
		if !winning[step.SubtaskID] {
			continue
		}
		seen := 0
		for i := len(commands) - 1; i >= 0 && seen <= distillEvidenceWindow; i-- {
			if commands[i].SubtaskID == step.SubtaskID {
				keep[i] = true
				seen++
			}
		}
	}

	if len(keep) > maxDistillTotalCommands {
		ordered := make([]int, 0, len(keep))
		for index := range keep {
			ordered = append(ordered, index)
		}
		sort.Ints(ordered)
		for _, index := range ordered[:len(ordered)-maxDistillTotalCommands] {
			delete(keep, index)
		}
	}

	return evidence, winning, keep
}

func stepHasSelectedCommand(subtaskID int64, commands []DistillCommand) bool {
	for _, command := range commands {
		if command.SubtaskID == subtaskID {
			return true
		}
	}
	return false
}

func renderDistillBody(
	input DistillInput,
	title string,
	evidence []DistillEvidence,
	steps []DistillStep,
	commands []DistillCommand,
	replacer Replacer,
	extraRedactions []string,
	generatedAt time.Time,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "## Context\n\nDistilled from a completed flow on %s. The target name, hosts and credentials are redacted; complete this section with the reusable trigger during review.\n\n",
		generatedAt.Format("2006-01-02"))

	b.WriteString("## Evidence\n\n")
	if len(evidence) == 0 {
		b.WriteString("No success signal was detected automatically. Confirm the flow actually succeeded before publishing.\n\n")
	} else {
		for _, item := range evidence {
			fmt.Fprintf(&b, "- %s: %s\n", item.Kind, item.Detail)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Steps\n\n")
	if len(steps) > maxDistillSteps {
		steps = steps[:maxDistillSteps]
	}

	stepsBySubtask := make(map[int64][]DistillCommand)
	for _, command := range commands {
		stepsBySubtask[command.SubtaskID] = append(stepsBySubtask[command.SubtaskID], command)
	}

	if len(steps) == 0 {
		b.WriteString("### Sequence\n\n")
		renderDistillCommands(&b, commands)
	} else {
		for i, step := range steps {
			stepTitle := truncateRunes(redactDistillText(step.Title, replacer, extraRedactions), maxTitleLen)
			if stepTitle == "" {
				stepTitle = fmt.Sprintf("Step %d", i+1)
			}
			fmt.Fprintf(&b, "### %d. %s\n\n", i+1, stepTitle)
			if description := truncateRunes(redactDistillText(step.Description, replacer, extraRedactions), maxDistillStepText); description != "" {
				fmt.Fprintf(&b, "%s\n\n", description)
			}
			renderDistillCommands(&b, stepsBySubtask[step.SubtaskID])
			if result := truncateRunes(redactDistillText(step.Result, replacer, extraRedactions), maxDistillStepText); result != "" {
				fmt.Fprintf(&b, "Outcome: %s\n\n", collapseWhitespace(result))
			}
		}
	}

	b.WriteString("## Review checklist\n\n")
	b.WriteString("- [ ] Confirm this is the winning path, not a dead end kept in the transcript.\n")
	b.WriteString("- [ ] Replace the generic context with the reusable trigger and pre-flight checks.\n")
	b.WriteString("- [ ] Set the tags, then promote to reviewed when the procedure is verified.\n\n")
	fmt.Fprintf(&b, "## Limits\n\nAutomatically distilled from flow %d; commands and outputs are excerpts, not the full transcript.\n", input.FlowID)

	return b.String()
}

func renderDistillCommands(b *strings.Builder, commands []DistillCommand) {
	if len(commands) == 0 {
		return
	}
	if len(commands) > maxDistillCommandsPerStep {
		commands = commands[:maxDistillCommandsPerStep]
	}

	for _, command := range commands {
		if strings.TrimSpace(command.Command) != "" {
			b.WriteString("Command:\n\n~~~\n")
			b.WriteString(fenceSafe(command.Command))
			b.WriteString("\n~~~\n\n")
		}
		output := truncateRunes(command.Output, maxDistillOutput)
		if strings.TrimSpace(output) != "" {
			b.WriteString("Output:\n\n~~~\n")
			b.WriteString(fenceSafe(collapseWhitespace(output)))
			b.WriteString("\n~~~\n\n")
		}
	}
}

func redactDistillText(value string, replacer Replacer, extraRedactions []string) string {
	for _, literal := range extraRedactions {
		literal = strings.TrimSpace(literal)
		if literal == "" {
			continue
		}
		value = strings.ReplaceAll(value, literal, "<REDACTED>")
	}
	value = distillFlagToken.ReplaceAllString(value, RedactedFlag)
	if replacer != nil {
		value = replacer.ReplaceString(value)
	}
	return value
}

func appendUniqueEvidence(values []DistillEvidence, candidate DistillEvidence) []DistillEvidence {
	for _, value := range values {
		if value.Kind == candidate.Kind && value.Detail == candidate.Detail {
			return values
		}
	}
	return append(values, candidate)
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func fenceSafe(value string) string {
	return strings.ReplaceAll(value, "~~~", "'''")
}

func normaliseDistillLanguage(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "english", "en", "en-us", "en-gb":
		return "en"
	case "french", "français", "fr":
		return "fr"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

// yamlScalar quotes a value when YAML would otherwise misread it.
func yamlScalar(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return `""`
	}
	if strings.ContainsAny(trimmed, ":#{}[],&*!|>'\"%@`") || strings.HasPrefix(trimmed, "-") ||
		strings.Contains(trimmed, "\n") {
		return fmt.Sprintf("%q", trimmed)
	}
	return trimmed
}
