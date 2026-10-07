package report

import (
	"context"
	"fmt"
	"strings"

	"pentagi/pkg/database"
	obs "pentagi/pkg/observability"
)

// digestCaps bound how much of one result the record carries. The planner and
// the writers receive the same digest, so it must stay inside a few hundred
// kilobytes even for long engagements (see REPORT_MAX_RECORD_BYTES).
const (
	digestSubtaskResultCap = 12000 // bytes per subtask result
	digestTaskResultCap    = 20000 // bytes per task-level result
	digestReportLogCap     = 12000 // bytes per task reporter message
)

// truncate runs a soft-cap on a text block: most of the head, a tail slice to
// keep conclusions, and an explicit marker so models know text is elided.
func truncate(text string, cap int) string {
	text = strings.TrimSpace(text)
	if cap <= 0 || len(text) <= cap {
		return text
	}

	head := cap * 2 / 3
	tail := cap / 4
	if tail > len(text)-head {
		tail = len(text) - head
	}

	return fmt.Sprintf("%s\n[... %d bytes truncated ...]\n%s",
		text[:head], len(text)-head-tail, text[len(text)-tail:])
}

// recordStats summarizes what the digest actually carried; published in the
// control log so operators can see the record shape at plan time.
type recordStats struct {
	Tasks        int
	Subtasks     int
	Results      int
	ReporterLogs int
	Screenshots  int
	Bytes        int
}

// buildDigest assembles the engagement record for one flow: flow header,
// tasks with their subtask results, the task-level reporter messages
// (msglogs type=report), and screenshot titles. Termlogs and toolcalls are
// intentionally not carried (v1): task/subtask results are the closing
// write-ups of what happened. The digest is capped byte-wise by
// REPORT_MAX_RECORD_BYTES; oversize elides subtask results first.
func buildDigest(
	ctx context.Context,
	db database.Querier,
	maxBytes int,
	flowID int64,
) (string, recordStats, error) {
	ctx, span := obs.Observer.NewSpan(ctx, obs.SpanKindInternal, "report.buildDigest")
	defer span.End()

	stats := recordStats{}

	flow, err := db.GetFlow(ctx, flowID)
	if err != nil {
		return "", stats, fmt.Errorf("failed to load flow %d: %w", flowID, err)
	}

	lang := flow.Language
	if lang == "" {
		lang = "English"
	}

	b := &strings.Builder{}
	fmt.Fprintf(b, "<engagement>\n<id>%d</id>\n<title>%s</title>\n<language>%s</language>\n<status>%s</status>\n",
		flow.ID, xmlEsc(flow.Title), xmlEsc(lang), xmlEsc(string(flow.Status)))
	fmt.Fprintf(b, "<model_provider>%s (%s)</model_provider>\n</engagement>\n",
		xmlEsc(flow.ModelProviderName), xmlEsc(string(flow.ModelProviderType)))

	tasks, err := db.GetFlowTasks(ctx, flow.ID)
	if err != nil {
		return "", stats, fmt.Errorf("failed to load flow %d tasks: %w", flow.ID, err)
	}
	stats.Tasks = len(tasks)

	subtasks, err := db.GetFlowSubtasks(ctx, flow.ID)
	if err != nil {
		return "", stats, fmt.Errorf("failed to load flow %d subtasks: %w", flow.ID, err)
	}
	stats.Subtasks = len(subtasks)

	subtasksByTask := make(map[int64][]database.Subtask, len(tasks))
	for _, st := range subtasks {
		subtasksByTask[st.TaskID] = append(subtasksByTask[st.TaskID], st)
	}

	reports, err := db.GetFlowMsgLogs(ctx, flow.ID)
	if err != nil {
		return "", stats, fmt.Errorf("failed to load flow %d msglogs: %w", flow.ID, err)
	}
	lastReporter := make(map[int64]string, len(tasks))
	for _, log := range reports {
		if log.Type != database.MsglogTypeReport || !log.TaskID.Valid || log.Result == "" {
			continue
		}
		// The latest reporter message per task wins (earlier drafts exist).
		lastReporter[log.TaskID.Int64] = log.Result
	}

	for _, task := range tasks {
		fmt.Fprintf(b, "<task id=\"%d\" status=\"%s\">\n", task.ID, xmlEsc(string(task.Status)))
		if strings.TrimSpace(task.Input) != "" {
			fmt.Fprintf(b, "<input>%s</input>\n", xmlEsc(digestOf(task.Input, digestSubtaskResultCap)))
		}
		if strings.TrimSpace(task.Result) != "" {
			fmt.Fprintf(b, "<result>%s</result>\n", xmlEsc(digestOf(task.Result, digestTaskResultCap)))
		}
		stats.Results += 1

		list := subtasksByTask[task.ID]
		if len(list) > 0 {
			b.WriteString("<subtasks>\n")
			for _, st := range list {
				fmt.Fprintf(b, "<subtask id=\"%d\" status=\"%s\" title=\"%s\">\n",
					st.ID, xmlEsc(string(st.Status)), xmlEsc(st.Title))
				if strings.TrimSpace(st.Result) != "" && st.Status == database.SubtaskStatusFinished {
					fmt.Fprintf(b, "<result>%s</result>\n", xmlEsc(digestOf(st.Result, digestSubtaskResultCap)))
				}
				b.WriteString("</subtask>\n")
			}
			b.WriteString("</subtasks>\n")
		}

		if text, ok := lastReporter[task.ID]; ok {
			stats.ReporterLogs++
			fmt.Fprintf(b, "<task_reporter_report>%s</task_reporter_report>\n", xmlEsc(digestOf(text, digestReportLogCap)))
		}

		b.WriteString("</task>\n")
	}

	screenshots, err := db.GetFlowScreenshots(ctx, flow.ID)
	if err != nil {
		return "", stats, fmt.Errorf("failed to load flow %d screenshots: %w", flow.ID, err)
	}

	stats.Screenshots = len(screenshots)
	if len(screenshots) > 0 {
		fmt.Fprintf(b, "<screenshots count=\"%d\">\n", len(screenshots))
		for _, shot := range screenshots {
			fmt.Fprintf(b, "<screenshot id=\"%d\" name=\"%s\"/>\n", shot.ID, xmlEsc(shot.Name))
		}
		b.WriteString("</screenshots>\n")
	}

	digest := b.String()
	stats.Bytes = len(digest)

	// A last-resort cap: extremely long engagements keep the head (flow +
	// earliest tasks) rather than overflow every writer's window.
	if maxBytes > 0 && len(digest) > maxBytes {
		digest = truncate(digest, maxBytes)
	}

	return digest, stats, nil
}

// digestOf caps one text block (the per-item caps keep the digest flat as the
// engagement grows; buildDigest applies the global cap at the end).
func digestOf(text string, cap int) string {
	return truncate(text, cap)
}

// xmlEsc keeps the digest structurally valid against stray angle brackets
// inside results, without escaping anything else (readability first).
func xmlEsc(text string) string {
	text = strings.ReplaceAll(text, "<", "&lt;")
	return strings.ReplaceAll(text, ">", "&gt;")
}
