package tools

import (
	"context"
	"strings"
	"sync"

	"pentagi/pkg/skills"
)

const (
	// maxSurfacedSkills bounds the cards appended to one tool result, as the
	// chain surfacing does.
	maxSurfacedSkills = 3
	// surfacedSkillChars keeps a card short enough to stay a hint.
	surfacedSkillChars = 220
)

// skillSurfacing appends a skill hint to a tool result. Implementations must be
// cheap: they run on every result of the tools they accept.
type skillSurfacing interface {
	Surface(ctx context.Context, toolName, result string) string
}

// skillSurfacer turns the raw output of a tool into a small block pointing at
// the matching skills. It never repeats a skill twice in the same flow, so the
// hint stays useful and does not flood the context.
type skillSurfacer struct {
	userID   int64
	store    SkillStore
	detector *skills.Detector

	mu   sync.Mutex
	seen map[string]struct{}
}

func newSkillSurfacer(userID int64, store SkillStore, detector *skills.Detector) *skillSurfacer {
	return &skillSurfacer{
		userID:   userID,
		store:    store,
		detector: detector,
		seen:     map[string]struct{}{},
	}
}

func (s *skillSurfacer) Surface(ctx context.Context, toolName, result string) string {
	if s == nil || s.detector == nil || s.store == nil || len(result) == 0 {
		return ""
	}
	if !isSurfacingTool(toolName) {
		return ""
	}

	slugs := s.detector.Match(result)
	if len(slugs) == 0 {
		return ""
	}

	cards := make([]string, 0, maxSurfacedSkills)
	for _, slug := range slugs {
		if len(cards) >= maxSurfacedSkills {
			break
		}
		if !s.claim(slug) {
			continue
		}

		doc, _, err := s.store.Read(ctx, s.userID, slug)
		if err != nil {
			// A rule can point at a skill that is not imported yet: stay silent
			// and let a later flow try again.
			s.forget(slug)
			continue
		}

		cards = append(cards, formatSurfacedSkill(doc))
	}

	if len(cards) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n<relevant_skills>\n")
	b.WriteString("The stored skills below match what this output just showed. ")
	b.WriteString("Read the matching one with read_skill before continuing.\n")
	for _, card := range cards {
		b.WriteString(card)
		b.WriteString("\n")
	}
	b.WriteString("</relevant_skills>")

	return b.String()
}

// claim reserves a slug for this flow. It returns false when the skill was
// already surfaced, which is what keeps the block from repeating.
func (s *skillSurfacer) claim(slug string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.seen[slug]; ok {
		return false
	}
	s.seen[slug] = struct{}{}

	return true
}

func (s *skillSurfacer) forget(slug string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.seen, slug)
}

// applySkillSurfacing appends the skill hint to a result when the executor has
// a surfacer. It is a no-op otherwise, so the executor stays usable without a
// skills store.
func (ce *customExecutor) applySkillSurfacing(ctx context.Context, name, result string) string {
	if ce == nil || ce.surfacer == nil || result == "" {
		return result
	}

	if extra := ce.surfacer.Surface(ctx, name, result); extra != "" {
		return result + extra
	}

	return result
}

func isSurfacingTool(name string) bool {
	switch name {
	case TerminalToolName, FileToolName, WebSearchToolName:
		return true
	default:
		return false
	}
}

func formatSurfacedSkill(doc *skills.Document) string {
	label := oneLineText(doc.Description, surfacedSkillChars)
	if label == "" {
		label = oneLineText(doc.Title, surfacedSkillChars)
	}

	card := "- `" + doc.Slug + "` [" + doc.Kind + "/" + doc.Status + "]"
	if title := oneLineText(doc.Title, 90); title != "" && title != label {
		card += " " + title + "."
	}
	if label != "" {
		card += " " + label
	}

	return card
}

// oneLineText collapses whitespace and truncates on a rune boundary.
func oneLineText(value string, limit int) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	joined := strings.Join(fields, " ")
	runes := []rune(joined)
	if len(runes) <= limit {
		return joined
	}
	return strings.TrimRight(string(runes[:limit]), " ,.;:") + "..."
}
