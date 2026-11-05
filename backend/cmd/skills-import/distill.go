package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/skills"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)

// runDistill turns a flow into a draft skill. It only reads the flow tables and
// imports a draft: nothing is published, and the operator has to review it.
func runDistill(
	ctx context.Context,
	pool *pgxpool.Pool,
	store *skills.Store,
	cfg *config.Config,
	userID, flowID int64,
	redact, tags []string,
	force, dryRun bool,
) error {
	replacer, err := buildReplacer(cfg)
	if err != nil {
		return err
	}

	var (
		ownerID  int64
		title    string
		language string
	)
	err = pool.QueryRow(ctx, `
SELECT user_id, COALESCE(title, ''), COALESCE(language, 'en')
FROM flows
WHERE id = $1 AND deleted_at IS NULL`, flowID).Scan(&ownerID, &title, &language)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("flow %d not found", flowID)
	}
	if err != nil {
		return fmt.Errorf("read flow %d: %w", flowID, err)
	}
	if ownerID != userID {
		return fmt.Errorf("flow %d belongs to another user", flowID)
	}

	steps, err := loadDistillSteps(ctx, pool, flowID)
	if err != nil {
		return err
	}
	commands, err := loadDistillCommands(ctx, pool, flowID)
	if err != nil {
		return err
	}

	if len(redact) == 0 {
		logrus.Warn("no --distill-redact given: review the draft for the target name, hostname and internal identifiers before publishing")
	}

	draft := skills.BuildDistilledDraft(skills.DistillInput{
		FlowID:      flowID,
		Title:       title,
		Language:    language,
		Steps:       steps,
		Commands:    commands,
		Tags:        tags,
		GeneratedAt: time.Now().UTC(),
	}, replacer, redact)

	if len(draft.Evidence) == 0 && !force {
		return fmt.Errorf("flow %d carries no success evidence; pass --distill-force to import it anyway", flowID)
	}

	if dryRun {
		fmt.Println(draft.Markdown)
		return nil
	}

	if !force {
		duplicate, err := findDistillDuplicate(ctx, store, userID, draft)
		if err != nil {
			logrus.WithError(err).Warn("duplicate lookup failed; importing the draft anyway")
		}
		if duplicate != nil {
			fmt.Printf("skipped: a similar document already exists: %s (score %.3f); pass --distill-force to import anyway\n",
				duplicate.Document.Slug, duplicate.Score)
			return nil
		}
	}

	document, err := store.ImportMarkdown(ctx, userID, skills.ImportInput{
		Slug:        draft.Slug,
		Content:     draft.Markdown,
		Kind:        skills.KindSkill,
		Status:      skills.StatusDraft,
		Visibility:  skills.VisibilityPrivate,
		SourceKind:  skills.DistillSourceKind,
		SourceURI:   fmt.Sprintf("pentagi://flow/%d", flowID),
		Language:    "en",
		DefaultTags: tags,
		Metadata: map[string]any{
			"distill_flow_id":      flowID,
			"distill_commands":     draft.Commands,
			"distill_evidence":     evidenceKinds(draft.Evidence),
			"distill_generated_at": time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"slug":     document.Slug,
		"version":  document.Version,
		"evidence": len(draft.Evidence),
		"commands": draft.Commands,
	}).Info("flow distilled into a draft skill")

	fmt.Printf("distilled flow %d into %s (v%d, draft/private): %d evidence signal(s), %d command(s)\n",
		flowID, document.Slug, document.Version, len(draft.Evidence), draft.Commands)
	fmt.Printf("next: complete the context and the review checklist, then run --publish %s --publish-to reviewed\n", document.Slug)

	return nil
}

func loadDistillSteps(ctx context.Context, pool *pgxpool.Pool, flowID int64) ([]skills.DistillStep, error) {
	rows, err := pool.Query(ctx, `
SELECT s.id, COALESCE(s.title, ''), COALESCE(s.description, ''), COALESCE(s.result, ''), COALESCE(s.evidence, '{}')
FROM subtasks s
JOIN tasks t ON t.id = s.task_id
WHERE t.flow_id = $1
ORDER BY s.id`, flowID)
	if err != nil {
		return nil, fmt.Errorf("read flow subtasks: %w", err)
	}
	defer rows.Close()

	steps := make([]skills.DistillStep, 0)
	for rows.Next() {
		var (
			step     skills.DistillStep
			evidence []string
		)
		if err := rows.Scan(&step.SubtaskID, &step.Title, &step.Description, &step.Result, &evidence); err != nil {
			return nil, fmt.Errorf("scan flow subtask: %w", err)
		}
		step.Evidence = evidence
		steps = append(steps, step)
	}

	return steps, rows.Err()
}

func loadDistillCommands(ctx context.Context, pool *pgxpool.Pool, flowID int64) ([]skills.DistillCommand, error) {
	rows, err := pool.Query(ctx, `
SELECT COALESCE(subtask_id, 0), name, COALESCE(args->>'input', ''), COALESCE(result, '')
FROM toolcalls
WHERE flow_id = $1 AND status = 'finished' AND name = 'terminal'
ORDER BY id`, flowID)
	if err != nil {
		return nil, fmt.Errorf("read flow commands: %w", err)
	}
	defer rows.Close()

	commands := make([]skills.DistillCommand, 0)
	for rows.Next() {
		var command skills.DistillCommand
		if err := rows.Scan(&command.SubtaskID, &command.Tool, &command.Command, &command.Output); err != nil {
			return nil, fmt.Errorf("scan flow command: %w", err)
		}
		commands = append(commands, command)
	}

	return commands, rows.Err()
}

func findDistillDuplicate(ctx context.Context, store *skills.Store, userID int64, draft *skills.DistilledDraft) (*skills.SearchHit, error) {
	hits, err := store.Search(ctx, userID, draft.Title+" "+draft.Description, skills.Filter{
		Kinds: []string{skills.KindSkill, skills.KindReference},
		Limit: 1,
	}, 1)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 || hits[0].Score < skills.DefaultDuplicateThreshold {
		return nil, nil
	}

	return &hits[0], nil
}

func evidenceKinds(evidence []skills.DistillEvidence) []string {
	kinds := make([]string, 0, len(evidence))
	for _, item := range evidence {
		kinds = append(kinds, item.Kind)
	}
	return kinds
}
