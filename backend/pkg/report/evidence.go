package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"pentagi/pkg/database"
	obs "pentagi/pkg/observability"
)

// EvidenceKind classifies one candidate in a writer's evidence pack.
type EvidenceKind string

const (
	EvidenceKindToolcall   EvidenceKind = "toolcall"
	EvidenceKindTermlog    EvidenceKind = "termlog"
	EvidenceKindScreenshot EvidenceKind = "screenshot"
)

// EvidenceRef is one numbered candidate injected into a writer's evidence pack
// ([E1], [E2], ...). When the writer cites a candidate, the resolved ref is what
// the archive bundles (plan § 6.10).
type EvidenceRef struct {
	Index        int          `json:"index"` // 1-based; [E1] is Index 1
	Kind         EvidenceKind `json:"kind"`
	Label        string       `json:"label"`
	Path         string       `json:"path,omitempty"`         // container path when a file is referenced
	ScreenshotID int64        `json:"screenshotId,omitempty"` // set for screenshot candidates
}

// EvidenceCaps bound one pack so a chatty engagement cannot overflow the
// writer's window. Defaults are conservative; the engine threads config in.
type EvidenceCaps struct {
	ItemBytes int // per candidate body
	MaxItems  int // candidates per pack
	PackBytes int // whole pack
}

const (
	defaultEvidenceItemBytes = 4000
	defaultEvidenceMaxItems  = 40
	defaultEvidencePackBytes = 48000
)

func (c EvidenceCaps) withDefaults() EvidenceCaps {
	if c.ItemBytes <= 0 {
		c.ItemBytes = defaultEvidenceItemBytes
	}
	if c.MaxItems <= 0 {
		c.MaxItems = defaultEvidenceMaxItems
	}
	if c.PackBytes <= 0 {
		c.PackBytes = defaultEvidencePackBytes
	}
	return c
}

// containerPathRx pulls /work/...-style paths out of a toolcall's arguments or
// output. Reading them from the structured fields (never from report prose) is
// what makes the archive file list deterministic.
var containerPathRx = regexp.MustCompile(`/work/[\w./@%+-]+`)

func firstContainerPath(texts ...string) string {
	for _, text := range texts {
		if match := containerPathRx.FindString(text); match != "" {
			return strings.TrimRight(match, ".,;)`'\"")
		}
	}
	return ""
}

// buildEvidencePack assembles the numbered evidence candidates for one section
// from the toolcalls, terminal output and screenshots of the subtasks it was
// derived from. It returns the pack text (injected into the writer prompt) and
// the candidate list (index -> ref) used to resolve the writer's citations.
func buildEvidencePack(
	ctx context.Context,
	db database.Querier,
	subtaskIDs []int64,
	caps EvidenceCaps,
) (string, []EvidenceRef, error) {
	ctx, span := obs.Observer.NewSpan(ctx, obs.SpanKindInternal, "report.buildEvidencePack")
	defer span.End()

	caps = caps.withDefaults()
	if len(subtaskIDs) == 0 {
		return "", nil, nil
	}

	b := &strings.Builder{}
	candidates := make([]EvidenceRef, 0, caps.MaxItems)
	seen := make(map[string]struct{})
	total := 0
	full := false

	// add appends one candidate unless it is a duplicate or the pack is full.
	add := func(dedupeKey, label, body string, ref EvidenceRef) {
		if full {
			return
		}
		if _, ok := seen[dedupeKey]; ok {
			return
		}
		body = strings.TrimSpace(body)
		if body == "" {
			return
		}
		body = truncate(body, caps.ItemBytes)
		ref.Index = len(candidates) + 1
		ref.Label = label
		entry := fmt.Sprintf("[E%d] %s\n%s\n", ref.Index, label, body)
		if total+len(entry) > caps.PackBytes {
			full = true
			return
		}
		seen[dedupeKey] = struct{}{}
		total += len(entry)
		candidates = append(candidates, ref)
		b.WriteString(entry)
		if len(candidates) >= caps.MaxItems {
			full = true
		}
	}

	for _, subtaskID := range subtaskIDs {
		nullID := sql.NullInt64{Int64: subtaskID, Valid: true}

		toolcalls, err := db.GetSubtaskToolcalls(ctx, nullID)
		if err != nil {
			return "", nil, fmt.Errorf("failed to load toolcalls for subtask %d: %w", subtaskID, err)
		}
		for _, tc := range toolcalls {
			body := formatToolcall(tc)
			add(fmt.Sprintf("toolcall:%d", tc.ID), fmt.Sprintf("toolcall %q", tc.Name), body, EvidenceRef{
				Kind: EvidenceKindToolcall,
				Path: firstContainerPath(string(tc.Args), tc.Result),
			})
		}

		termlogs, err := db.GetSubtaskTermLogs(ctx, nullID)
		if err != nil {
			return "", nil, fmt.Errorf("failed to load termlogs for subtask %d: %w", subtaskID, err)
		}
		for _, tl := range termlogs {
			if tl.Type == database.TermlogTypeStdin {
				// The command line is already in the toolcall candidate.
				continue
			}
			add(fmt.Sprintf("termlog:%d", tl.ID), fmt.Sprintf("terminal %s", tl.Type), tl.Text, EvidenceRef{
				Kind: EvidenceKindTermlog,
				Path: firstContainerPath(tl.Text),
			})
		}

		screenshots, err := db.GetSubtaskScreenshots(ctx, nullID)
		if err != nil {
			return "", nil, fmt.Errorf("failed to load screenshots for subtask %d: %w", subtaskID, err)
		}
		for _, shot := range screenshots {
			label := shot.Name
			if label == "" {
				label = fmt.Sprintf("screenshot %d", shot.ID)
			}
			add(fmt.Sprintf("screenshot:%d", shot.ID), fmt.Sprintf("screenshot %q (id %d)", label, shot.ID),
				fmt.Sprintf("Screenshot captured during the subtask: %s.", label), EvidenceRef{
					Kind:         EvidenceKindScreenshot,
					ScreenshotID: shot.ID,
				})
		}
	}

	return strings.TrimRight(b.String(), "\n"), candidates, nil
}

func formatToolcall(tc database.Toolcall) string {
	b := &strings.Builder{}
	if len(tc.Args) > 0 {
		fmt.Fprintf(b, "args: %s\n", string(tc.Args))
	}
	if strings.TrimSpace(tc.Result) != "" {
		fmt.Fprintf(b, "output:\n%s", tc.Result)
	}
	return b.String()
}

var citationRx = regexp.MustCompile(`\[E(\d+)\]`)

// resolveCitations maps the [Ex] markers a writer cited back to their
// candidates. It only ever reads the engine's own closed candidate set: unknown
// or repeated markers are ignored, never guessed from the prose.
func resolveCitations(content string, candidates []EvidenceRef) []EvidenceRef {
	if len(candidates) == 0 || content == "" {
		return nil
	}

	byIndex := make(map[int]EvidenceRef, len(candidates))
	for _, candidate := range candidates {
		byIndex[candidate.Index] = candidate
	}

	seen := make(map[int]struct{})
	out := make([]EvidenceRef, 0, len(candidates))

	for _, match := range citationRx.FindAllStringSubmatch(content, -1) {
		index, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		if _, ok := seen[index]; ok {
			continue
		}
		candidate, ok := byIndex[index]
		if !ok {
			continue
		}
		seen[index] = struct{}{}
		out = append(out, candidate)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })

	return out
}

// encodeEvidenceRefs serializes a manifest for report_sections.evidence_refs,
// keeping the column a valid JSON array even when nothing was cited.
func encodeEvidenceRefs(refs []EvidenceRef) (json.RawMessage, error) {
	if len(refs) == 0 {
		return json.RawMessage("[]"), nil
	}
	blob, err := json.Marshal(refs)
	if err != nil {
		return nil, fmt.Errorf("failed to encode evidence refs: %w", err)
	}
	return blob, nil
}
