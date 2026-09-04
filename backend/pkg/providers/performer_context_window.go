package providers

import (
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
// error is a context-window overflow, either stores a clamped max_tokens override
// for this agent type (re-parsed from fresh numbers on every failure, so repeated
// overflows converge downward) or reports that the chain itself must be shortened.
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
	fp.setMaxTokensOverride(optAgentType, budget)
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

func (fp *flowProvider) getMaxTokensOverride(opt pconfig.ProviderOptionsType) (int, bool) {
	fp.mx.RLock()
	defer fp.mx.RUnlock()
	if fp.maxTokensOverride == nil {
		return 0, false
	}
	value, ok := fp.maxTokensOverride[opt]
	return value, ok
}

func (fp *flowProvider) setMaxTokensOverride(opt pconfig.ProviderOptionsType, maxTokens int) {
	fp.mx.Lock()
	defer fp.mx.Unlock()
	if fp.maxTokensOverride == nil {
		fp.maxTokensOverride = make(map[pconfig.ProviderOptionsType]int)
	}
	// always take the fresh numbers: they may raise the budget back up after the
	// summarizer compressed the history, or clamp further down as the prompt grows
	fp.maxTokensOverride[opt] = maxTokens
}
