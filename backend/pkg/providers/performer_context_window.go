package providers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"pentagi/pkg/providers/pconfig"

	"github.com/sirupsen/logrus"
	"github.com/vxcontrol/langchaingo/llms"
)

// A fixed max_tokens budget combined with a growing prompt eventually exceeds the
// model context window: the request is rejected with a 400 before any generation,
// the generic retry loop burns its attempts, the reflector fails the same way and
// the flow worker suspends the chain in `waiting` forever (observed on local vLLM
// models with a reduced context, flows 99/100: prompt 31617 + max_tokens 16384 =
// 48001 > ctx 48000). Instead of dying, clamp the generation budget to the space
// the prompt leaves free and keep the chain alive with slightly shorter generations.

const (
	// contextWindowMarginTokens guards against under-estimated prompt sizes: backends
	// report the prompt token count as "at least" and request framing (tool schemas,
	// message envelopes) adds tokens on top of the reported history size.
	contextWindowMarginTokens = 1024

	// minGenerationBudgetTokens is the smallest max_tokens worth retrying with —
	// below this the generation budget is useless (any tool call gets truncated) and
	// the chain itself must be shortened instead.
	minGenerationBudgetTokens = 512
)

// contextWindowAction tells the retry loop what to do after a context-window error.
type contextWindowAction int

const (
	contextWindowNone      contextWindowAction = iota // not a context-window error
	contextWindowClamped                              // max tokens override set — retry immediately
	contextWindowNeedsTrim                            // no usable budget — shorten the chain
)

// contextWindowKnowledge is what a context-window rejection teaches us about the
// backend: the model's total context window and the generation budget the agent's
// config actually requested (parsed from "requested N output tokens" — this is the
// agent's configured max_tokens as seen by the backend).
type contextWindowKnowledge struct {
	contextTokens int
	maxOutTokens  int
}

// contextWindowLearned reports whether a backend rejection has taught us the
// model's context window for this agent type yet.
func (k contextWindowKnowledge) learned() bool { return k.contextTokens > 0 }

// Lowercase markers covering OpenAI / vLLM / LiteLLM / Anthropic wording.
var contextWindowErrorMarkers = []string{
	"context window exceeded",
	"maximum context length",
	"context_length_exceeded",
	"contextwindowexceeded",
	"prompt is too long",
}

var (
	contextLengthRE = regexp.MustCompile(`(?i)maximum context length is (\d+) tokens`)
	// OpenAI/LiteLLM: "your prompt contains at least 31617 input tokens"
	promptTokensRE = regexp.MustCompile(`(?i)prompt contains (?:at least )?(\d+) input tokens`)
	// LiteLLM structured form: "(parameter=input_tokens, value=31617)"
	promptTokensAltRE = regexp.MustCompile(`(?i)input_tokens[^0-9]*value[=:]\s*(\d+)`)
	// OpenAI/LiteLLM: "you requested 16384 output tokens" — the agent's configured
	// maxTokens as seen by the backend; used as the ceiling of the dynamic budget
	requestedOutRE = regexp.MustCompile(`(?i)requested (\d+) output tokens`)
	// Anthropic: "prompt is too long: 200010 tokens > 197463 maximum tokens"
	anthropicPromptTooLongRE = regexp.MustCompile(`(?i)prompt is too long: (\d+) tokens > (\d+)`)
)

func isContextWindowExceededError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range contextWindowErrorMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// parseContextWindowLimit extracts the model context size and the reported prompt
// token count from the error message. ok is false when either number is missing —
// the caller then falls back to shortening the chain.
func parseContextWindowLimit(msg string) (contextTokens, promptTokens int, ok bool) {
	if m := contextLengthRE.FindStringSubmatch(msg); m != nil {
		contextTokens, _ = strconv.Atoi(m[1])
	}
	if m := promptTokensRE.FindStringSubmatch(msg); m != nil {
		promptTokens, _ = strconv.Atoi(m[1])
	}
	if promptTokens == 0 {
		if m := promptTokensAltRE.FindStringSubmatch(msg); m != nil {
			promptTokens, _ = strconv.Atoi(m[1])
		}
	}
	if m := anthropicPromptTooLongRE.FindStringSubmatch(msg); m != nil {
		promptTokens, _ = strconv.Atoi(m[1])
		if contextTokens == 0 {
			contextTokens, _ = strconv.Atoi(m[2])
		}
	}
	return contextTokens, promptTokens, contextTokens > 0 && promptTokens > 0
}

func clampGenerationBudget(contextTokens, promptTokens int) (int, bool) {
	if contextTokens <= 0 || promptTokens <= 0 {
		return 0, false
	}
	budget := contextTokens - promptTokens - contextWindowMarginTokens
	if budget < minGenerationBudgetTokens {
		return 0, false
	}
	return budget, true
}

// handleContextWindowExceeded classifies a failed agent chain call and, when the
// error is a context-window overflow, records what the rejection teaches (context
// window size, configured generation budget) and stores a clamped max_tokens
// override for an immediate retry. Fresh rejections re-learn with exact numbers.
func (fp *flowProvider) handleContextWindowExceeded(
	optAgentType pconfig.ProviderOptionsType,
	err error,
	logger *logrus.Entry,
) contextWindowAction {
	if !isContextWindowExceededError(err) {
		return contextWindowNone
	}
	contextTokens, promptTokens, ok := parseContextWindowLimit(err.Error())
	if !ok {
		return contextWindowNeedsTrim
	}
	budget, ok := clampGenerationBudget(contextTokens, promptTokens)
	if !ok {
		return contextWindowNeedsTrim
	}
	knowledge := contextWindowKnowledge{contextTokens: contextTokens}
	if m := requestedOutRE.FindStringSubmatch(err.Error()); m != nil {
		knowledge.maxOutTokens, _ = strconv.Atoi(m[1])
	}
	fp.setContextWindowKnowledge(optAgentType, knowledge)
	logger.WithFields(logrus.Fields{
		"context_tokens":     contextTokens,
		"prompt_tokens":      promptTokens,
		"clamped_max_tokens": budget,
	}).Warn("context window exceeded — clamped max tokens for this agent, retrying with the reduced budget")
	return contextWindowClamped
}

// trimChainForContextWindow drops the oldest half of the conversation history as a
// last resort (no usable generation budget left). The cut lands only on a Human or
// System boundary so AI(tool calls) + Tool response groups stay paired — cutting
// between them would produce orphan tool responses that OpenAI-compatible APIs
// reject with another 400.
func trimChainForContextWindow(chain []llms.MessageContent) []llms.MessageContent {
	if len(chain) <= 4 {
		return chain
	}

	head := 0
	for head < len(chain) && chain[head].Role == llms.ChatMessageTypeSystem {
		head++
	}
	rest := chain[head:]
	if len(rest) <= 4 {
		return chain
	}

	isCuttable := func(msg llms.MessageContent) bool {
		return msg.Role == llms.ChatMessageTypeHuman || msg.Role == llms.ChatMessageTypeSystem
	}

	cut := -1
	for i := len(rest) / 2; i < len(rest); i++ {
		if isCuttable(rest[i]) {
			cut = i
			break
		}
	}
	if cut == -1 {
		for i := len(rest)/2 - 1; i > 0; i-- {
			if isCuttable(rest[i]) {
				cut = i
				break
			}
		}
	}
	if cut <= 0 {
		return chain
	}

	trimmed := make([]llms.MessageContent, 0, head+len(rest)-cut+1)
	trimmed = append(trimmed, chain[:head]...)
	trimmed = append(trimmed, llms.TextParts(llms.ChatMessageTypeHuman,
		"[context window overflow] The oldest part of this conversation was truncated to fit the model context window. Work from the most recent messages below."))
	trimmed = append(trimmed, rest[cut:]...)
	return trimmed
}

func (fp *flowProvider) getContextWindowKnowledge(opt pconfig.ProviderOptionsType) (contextWindowKnowledge, bool) {
	fp.mx.RLock()
	defer fp.mx.RUnlock()
	knowledge, ok := fp.contextWindowKnowledge[opt]
	return knowledge, ok
}

func (fp *flowProvider) setContextWindowKnowledge(opt pconfig.ProviderOptionsType, knowledge contextWindowKnowledge) {
	fp.mx.Lock()
	defer fp.mx.Unlock()
	if fp.contextWindowKnowledge == nil {
		fp.contextWindowKnowledge = make(map[pconfig.ProviderOptionsType]contextWindowKnowledge)
	}
	// always take the fresh numbers: the context window is a backend fact, the
	// configured budget reflects the agent config at rejection time
	fp.contextWindowKnowledge[opt] = knowledge
}

// dynamicGenerationBudget computes the max_tokens for ONE call from the learned
// context window and the CURRENT chain estimate. The history grows and shrinks
// between calls (summarizer, new subtasks): a stale static budget wastes space
// when the prompt shrank (truncated thinking loops) or misses the window when it
// grew. The agent's configured budget (parsed from "requested N output tokens")
// stays the ceiling — anti-explosion caps like gemma4's 4096 are preserved.
func dynamicGenerationBudget(knowledge contextWindowKnowledge, estimatedPromptTokens int) (int, bool) {
	if !knowledge.learned() {
		return 0, false
	}
	budget, ok := clampGenerationBudget(knowledge.contextTokens, estimatedPromptTokens)
	if !ok {
		return 0, false
	}
	if knowledge.maxOutTokens > 0 && budget > knowledge.maxOutTokens {
		budget = knowledge.maxOutTokens
	}
	return budget, true
}

// estimateChainTokens approximates the token count of a prompt chain. Byte-based
// (~4 bytes/token for typical English + tool JSON): a slightly optimistic estimate
// is the cheap side — a too-big budget is one fast 400 that the rejection path
// re-learns from with exact numbers, while a too-small budget causes truncated
// generation loops that burn minutes per attempt.
func estimateChainTokens(chain []llms.MessageContent) int {
	bytes := 0
	for _, msg := range chain {
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case llms.TextContent:
				bytes += len(p.Text)
			case llms.ToolCall:
				if p.FunctionCall != nil {
					bytes += len(p.FunctionCall.Name) + len(p.FunctionCall.Arguments)
				}
			case llms.ToolCallResponse:
				bytes += len(p.Name) + len(p.Content)
			default:
				bytes += len(fmt.Sprintf("%v", part))
			}
		}
	}
	return bytes / 4
}
