package providers

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"pentagi/pkg/clmrank"
	"pentagi/pkg/config"
	obs "pentagi/pkg/observability"
	"pentagi/pkg/observability/langfuse"
	"pentagi/pkg/providers/pconfig"

	"github.com/sirupsen/logrus"
	"github.com/vxcontrol/langchaingo/llms"
)

// Bounds for the CLM encoder context window (server side: 2048 tokens max).
// Everything below is in runes to keep UTF-8 intact when truncating.
const (
	clmMaxHumanContextChars     = 1500 // last human message (objective/subtask)
	clmMaxRecentMessages        = 8    // sliding window of the chain tail
	clmMaxRecentMessageChars    = 300  // per rendered message in the window
	clmMaxCandidateContentChars = 600  // per candidate content
	clmMaxCandidateToolArgChars = 300  // per tool call arguments in a candidate
	clmMaxBestOfN               = 5    // hard cap for CLM_BEST_OF_N
)

// clmRankQuestion is the instruction the CLM state head sees after the context;
// the action head sees each candidate verbatim. Worded to reward concrete
// progress and penalize blind repetition of failed actions.
const clmRankQuestion = "Which proposed next action is the most likely to produce concrete progress " +
	"toward the objective (new information, new access, credential or flag capture), " +
	"while avoiding blind repetition of actions that already failed?"

const clmNoContentPlaceholder = "(no content)"

// clmVerifier gates and runs CLM best-of-N verification for the agent types
// listed in CLM_BEST_OF_N_AGENTS. Construction is side-effect free; a nil
// verifier means the feature is disabled and all callers must be fail-open.
type clmVerifier struct {
	client  *clmrank.Client
	agents  map[pconfig.ProviderOptionsType]bool
	bestOfN int
}

// newCLMVerifier builds the verifier from env config; it returns nil (feature
// off) when disabled or misconfigured, logging the reason once at startup or
// flow creation time.
func newCLMVerifier(cfg *config.Config) *clmVerifier {
	if !cfg.CLMVerifierEnabled {
		return nil
	}

	if strings.TrimSpace(cfg.CLMServerURL) == "" {
		logrus.Warn("CLM_VERIFIER_ENABLED is set but CLM_SERVER_URL is empty: CLM verifier disabled")
		return nil
	}

	agents := make(map[pconfig.ProviderOptionsType]bool)
	for _, name := range strings.Split(cfg.CLMAgents, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			agents[pconfig.ProviderOptionsType(name)] = true
		}
	}
	if len(agents) == 0 {
		logrus.WithField("CLM_BEST_OF_N_AGENTS", cfg.CLMAgents).
			Warn("CLM verifier has no valid agent type to gate on: CLM verifier disabled")
		return nil
	}

	n := cfg.CLMBestOfN
	if n < 2 {
		n = 2
	}
	if n > clmMaxBestOfN {
		n = clmMaxBestOfN
	}

	logrus.WithFields(logrus.Fields{
		"server_url":  cfg.CLMServerURL,
		"model":       cfg.CLMModel,
		"best_of_n":   n,
		"agents":      cfg.CLMAgents,
		"timeout_sec": cfg.CLMTimeoutSec,
	}).Info("CLM best-of-N verifier enabled")

	return &clmVerifier{
		client:  clmrank.New(cfg.CLMServerURL, cfg.CLMAPIKey, cfg.CLMModel, time.Duration(cfg.CLMTimeoutSec)*time.Second),
		agents:  agents,
		bestOfN: n,
	}
}

// appliesTo reports whether the best-of-N path should trigger for this agent.
func (v *clmVerifier) appliesTo(optAgentType pconfig.ProviderOptionsType) bool {
	return v != nil && v.agents[optAgentType]
}

// clipRunes truncates s to at most n runes, appending an ellipsis marker so a
// truncated candidate is never silently identical to an untruncated one.
func clipRunes(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}

	runes := []rune(s)
	return string(runes[:n]) + "..."
}

// buildCLMContext renders the state the CLM state head will see: the current
// objective/subtask (last human message) plus a compact tail of the chain.
func buildCLMContext(chain []llms.MessageContent, lastHuman string) string {
	var sb strings.Builder

	if human := strings.TrimSpace(lastHuman); human != "" {
		sb.WriteString("Current objective and task context:\n")
		sb.WriteString(clipRunes(human, clmMaxHumanContextChars))
		sb.WriteString("\n\n")
	}

	start := len(chain) - clmMaxRecentMessages
	if start < 0 {
		start = 0
	}

	sb.WriteString("Recent conversation steps (oldest first):")
	for _, msg := range chain[start:] {
		line := renderMessageForCLM(msg)
		if line == "" {
			continue
		}
		sb.WriteString("\n- ")
		sb.WriteString(string(msg.Role))
		sb.WriteString(": ")
		sb.WriteString(line)
	}

	return sb.String()
}

// renderMessageForCLM renders a single chain message as one compact line.
func renderMessageForCLM(msg llms.MessageContent) string {
	var parts []string

	for _, part := range msg.Parts {
		switch p := part.(type) {
		case llms.TextContent:
			if text := strings.TrimSpace(p.Text); text != "" {
				parts = append(parts, text)
			}
		case llms.ToolCall:
			if p.FunctionCall != nil {
				parts = append(parts, fmt.Sprintf("[call] %s %s",
					p.FunctionCall.Name, clipRunes(p.FunctionCall.Arguments, clmMaxRecentMessageChars)))
			}
		case llms.ToolCallResponse:
			parts = append(parts, fmt.Sprintf("[result] %s: %s",
				p.Name, clipRunes(p.Content, clmMaxRecentMessageChars)))
		}
	}

	return clipRunes(strings.Join(parts, " | "), clmMaxRecentMessageChars)
}

// renderCandidate renders one proposed agent step (content + tool calls) as the
// CLM answer string to rank.
func renderCandidate(result *callResult) string {
	var sb strings.Builder

	if content := strings.TrimSpace(result.content); content != "" {
		sb.WriteString(clipRunes(content, clmMaxCandidateContentChars))
	}

	for _, toolCall := range result.funcCalls {
		if toolCall.FunctionCall == nil {
			continue
		}
		fmt.Fprintf(&sb, "\n[tool] %s %s",
			toolCall.FunctionCall.Name,
			clipRunes(toolCall.FunctionCall.Arguments, clmMaxCandidateToolArgChars))
	}

	if sb.Len() == 0 {
		return clmNoContentPlaceholder
	}

	return sb.String()
}

// chooseBestOfN splits a multi-choice response into one callResult per choice,
// ranks the distinct candidates with CLM, and returns the winning callResult.
//
// It is fail-open by design: unusable choices are skipped, any ranking problem
// keeps candidate 0 (the model's first completion, i.e. the behavior the chain
// would have had without the verifier), and every decision is logged to logrus
// and Langfuse for offline analysis.
func (fp *flowProvider) chooseBestOfN(
	ctx context.Context,
	chain []llms.MessageContent,
	optAgentType pconfig.ProviderOptionsType,
	choices []*llms.ContentChoice,
	logger *logrus.Entry,
) (result *callResult, err error) {
	candidates := make([]*callResult, 0, len(choices))
	for _, choice := range choices {
		var candidate callResult
		if fillErr := fp.fillCallResult(&candidate, &llms.ContentResponse{
			Choices: []*llms.ContentChoice{choice},
		}, logger); fillErr != nil {
			logger.WithError(fillErr).Debug("clm verifier: skipping unusable response choice")
			continue
		}
		candidates = append(candidates, &candidate)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("clm verifier: no usable choice among %d", len(choices))
	}

	if len(candidates) == 1 {
		return candidates[0], nil
	}

	rendered := make([]string, len(candidates))
	unique := make([]string, 0, len(candidates))
	firstIdxByCandidate := make(map[string]int, len(candidates))
	duplicates := 0

	for idx, candidate := range candidates {
		rendered[idx] = renderCandidate(candidate)
		if _, seen := firstIdxByCandidate[rendered[idx]]; !seen {
			firstIdxByCandidate[rendered[idx]] = idx
			unique = append(unique, rendered[idx])
		} else {
			duplicates++
		}
	}

	winner := 0
	var (
		rankModel   string
		rankLatency float64 = -1
		rankErr     error
		probs       []float64
	)

	if len(unique) >= 2 {
		var res *clmrank.RankResult
		clmCtx, cancel := context.WithTimeout(ctx, fp.clmTimeout())
		res, rankErr = fp.clm.client.Rank(
			clmCtx, buildCLMContext(chain, fp.getLastHumanMessage(chain)), clmRankQuestion, unique,
		)
		cancel()

		switch {
		case rankErr != nil:
			logger.WithError(rankErr).Warn("clm verifier rank failed, keeping the model's first candidate (fail-open)")
		case len(res.Ranked) == 0:
			rankErr = fmt.Errorf("clm verifier returned an empty ranking")
			logger.Warn("clm verifier returned an empty ranking, keeping the model's first candidate (fail-open)")
		default:
			rankModel, rankLatency = res.Model, res.LatencyMs
			probs = make([]float64, 0, len(res.Ranked))
			for _, ranked := range res.Ranked {
				probs = append(probs, ranked.Prob)
			}
			if uniqueIdx, ok := firstIdxByCandidate[res.Ranked[0].Candidate]; ok {
				winner = uniqueIdx
			}
		}
	}

	fields := logrus.Fields{
		"agent":      optAgentType,
		"candidates": len(candidates),
		"unique":     len(unique),
		"duplicates": duplicates,
		"winner":     winner,
		"probs":      probs,
		"latency_ms": rankLatency,
	}
	if rankModel != "" {
		fields["clm_model"] = rankModel
	}
	if rankErr != nil {
		fields["rank_error"] = rankErr.Error()
		logger.WithFields(fields).Warn("clm verifier: degraded decision")
	} else {
		logger.WithFields(fields).Info("clm verifier decision")
	}

	_, observation := obs.Observer.NewObservation(ctx)
	level := langfuse.ObservationLevelDefault
	status := "success"
	if rankErr != nil {
		level = langfuse.ObservationLevelWarning
		status = rankErr.Error()
	}
	observation.Event(
		langfuse.WithEventName("clm verifier best-of-n decision"),
		langfuse.WithEventMetadata(langfuse.Metadata{
			"agent":            string(optAgentType),
			"candidates_total": len(candidates),
			"candidates_uniq":  len(unique),
			"duplicates":       duplicates,
			"winner_index":     winner,
			"candidate_probs":  probs,
			"latency_ms":       rankLatency,
			"clm_model":        rankModel,
		}),
		langfuse.WithEventInput(unique),
		langfuse.WithEventOutput(rendered[winner]),
		langfuse.WithEventLevel(level),
		langfuse.WithEventStatus(status),
	)

	return candidates[winner], nil
}

// clmTimeout returns the per-decision timeout, mirroring the client's HTTP
// timeout so a slow CLM server can never stall the agent chain.
func (fp *flowProvider) clmTimeout() time.Duration {
	if fp.cfg == nil || fp.cfg.CLMTimeoutSec <= 0 {
		return 5 * time.Second
	}

	return time.Duration(fp.cfg.CLMTimeoutSec) * time.Second
}
