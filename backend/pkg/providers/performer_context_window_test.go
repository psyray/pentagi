package providers

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"pentagi/pkg/providers/pconfig"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/vxcontrol/langchaingo/llms"
)

// The exact error observed on flow 100 (2026-09-04 00:32 UTC, LiteLLM -> vLLM qwen):
// prompt 31617 + max_tokens 16384 = 48001 > ctx 48000 -> systematic 400 -> waiting.
const flow100ContextWindowError = `API returned unexpected status code: 400: litellm.ContextWindowExceededError: litellm.BadRequestError: ContextWindowExceededError: OpenAIException - This model's maximum context length is 48000 tokens. However, you requested 16384 output tokens and your prompt contains at least 31617 input tokens, for a total of at least 48001 tokens. Please reduce the length of the input prompt or the number of requested output tokens. (parameter=input_tokens, value=31617)`

func testLoggerEntry() *logrus.Entry {
	return logrus.WithContext(context.Background()).WithField("test", "context_window")
}

func TestIsContextWindowExceededError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"litellm flow 100", errors.New(flow100ContextWindowError), true},
		{"openai classic", errors.New(`openai error 400, context_length_exceeded: This model's maximum context length is 8192 tokens`), true},
		{"anthropic", errors.New(`400: prompt is too long: 200010 tokens > 197463 maximum tokens`), true},
		{"plain words", errors.New("context window exceeded for model"), true},
		{"unrelated 400", errors.New(`API returned unexpected status code: 400: model does not support tool calls`), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isContextWindowExceededError(tc.err))
		})
	}
}

func TestParseContextWindowLimit(t *testing.T) {
	ctx, prompt, ok := parseContextWindowLimit(flow100ContextWindowError)
	assert.True(t, ok)
	assert.Equal(t, 48000, ctx)
	assert.Equal(t, 31617, prompt)

	ctx, prompt, ok = parseContextWindowLimit(`400: prompt is too long: 200010 tokens > 197463 maximum tokens`)
	assert.True(t, ok)
	assert.Equal(t, 197463, ctx)
	assert.Equal(t, 200010, prompt)

	// alt LiteLLM structured form without the sentence
	_, prompt, ok = parseContextWindowLimit(`maximum context length is 32768 tokens (parameter=input_tokens, value=29001)`)
	assert.True(t, ok)
	assert.Equal(t, 29001, prompt)

	_, _, ok = parseContextWindowLimit("some other 400 error")
	assert.False(t, ok)
}

func TestClampGenerationBudget(t *testing.T) {
	// flow 100: 48000 - 31617 - 1024 = 15359 — a usable budget instead of a dead flow
	budget, ok := clampGenerationBudget(48000, 31617)
	assert.True(t, ok)
	assert.Equal(t, 15359, budget)

	// flow 99 would have been: 48000 - 23425 - 1024 = 23551
	budget, ok = clampGenerationBudget(48000, 23425)
	assert.True(t, ok)
	assert.Equal(t, 23551, budget)

	// prompt alone (nearly) fills the window — no usable budget, trim the chain instead
	_, ok = clampGenerationBudget(48000, 47200)
	assert.False(t, ok)

	_, ok = clampGenerationBudget(0, 100)
	assert.False(t, ok)
}

func buildTestChain(sections int) []llms.MessageContent {
	chain := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, "system prompt"),
		llms.TextParts(llms.ChatMessageTypeHuman, "task definition"),
	}
	for i := 0; i < sections; i++ {
		chain = append(chain, llms.MessageContent{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{ID: "tc1", FunctionCall: &llms.FunctionCall{Name: "terminal"}},
			},
		})
		chain = append(chain, llms.MessageContent{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{ToolCallID: "tc1", Name: "terminal", Content: "output"},
			},
		})
	}
	chain = append(chain, llms.TextParts(llms.ChatMessageTypeHuman, "latest instruction"))
	return chain
}

func TestTrimChainForContextWindow(t *testing.T) {
	chain := buildTestChain(20)

	trimmed := trimChainForContextWindow(chain)
	assert.Less(t, len(trimmed), len(chain))
	// system prompt kept at the head
	assert.Equal(t, llms.ChatMessageTypeSystem, trimmed[0].Role)
	// the truncation notice, then the cut message — must be a Human/System boundary
	assert.Equal(t, llms.ChatMessageTypeHuman, trimmed[1].Role)
	assert.True(t, strings.Contains(trimmed[1].Parts[0].(llms.TextContent).Text, "context window overflow"))
	assert.Equal(t, llms.ChatMessageTypeHuman, trimmed[2].Role)
	// no orphaned tool responses at the head of the kept section
	assert.NotEqual(t, llms.ChatMessageTypeTool, trimmed[2].Role)

	// small chains are returned unchanged
	small := buildTestChain(1)
	assert.Equal(t, small, trimChainForContextWindow(small))

	// backward fallback: no Human/System in the second half -> cuts at the first human
	noBoundary := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, "system prompt"),
		llms.TextParts(llms.ChatMessageTypeHuman, "task definition"),
	}
	for i := 0; i < 20; i++ {
		noBoundary = append(noBoundary,
			llms.MessageContent{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{llms.TextPart("thinking")}},
			llms.MessageContent{Role: llms.ChatMessageTypeTool, Parts: []llms.ContentPart{llms.TextPart("result")}},
		)
	}
	trimmed = trimChainForContextWindow(noBoundary)
	// degenerate chain: no Human/System boundary after index 0 — unchanged (cutting
	// at the task definition itself would drop the task and gain nothing)
	assert.Equal(t, noBoundary, trimmed)
}

func TestContextWindowKnowledge(t *testing.T) {
	fp := &flowProvider{mx: new(sync.RWMutex)}

	_, ok := fp.getContextWindowKnowledge(pconfig.OptionsTypePentester)
	assert.False(t, ok)

	fp.setContextWindowKnowledge(pconfig.OptionsTypePentester, contextWindowKnowledge{contextTokens: 48000, maxOutTokens: 16384})
	k, ok := fp.getContextWindowKnowledge(pconfig.OptionsTypePentester)
	assert.True(t, ok)
	assert.Equal(t, 48000, k.contextTokens)
	assert.Equal(t, 16384, k.maxOutTokens)

	// fresh numbers re-learned on a later rejection replace the old knowledge
	fp.setContextWindowKnowledge(pconfig.OptionsTypePentester, contextWindowKnowledge{contextTokens: 48000, maxOutTokens: 12288})
	k, _ = fp.getContextWindowKnowledge(pconfig.OptionsTypePentester)
	assert.Equal(t, 12288, k.maxOutTokens)

	// other agent types are unaffected
	_, ok = fp.getContextWindowKnowledge(pconfig.OptionsTypeReflector)
	assert.False(t, ok)
}

func TestDynamicGenerationBudget(t *testing.T) {
	k := contextWindowKnowledge{contextTokens: 48000, maxOutTokens: 12288}

	// prompt fell back to ~5.5k tokens after summarizer compression (flow 101 state):
	// the budget goes back UP to the configured 12288 instead of staying stale-low
	budget, ok := dynamicGenerationBudget(k, 5500)
	assert.True(t, ok)
	assert.Equal(t, 12288, budget)

	// prompt ~30k: the window binds, not the config
	budget, ok = dynamicGenerationBudget(k, 30000)
	assert.True(t, ok)
	assert.Equal(t, 12288, budget) // 48000 - 30000 - 1024 = 16976 > 12288

	// prompt 37k: estimate leaves only 10264 -> below the configured ceiling
	budget, ok = dynamicGenerationBudget(k, 36712)
	assert.True(t, ok)
	assert.Equal(t, 10264, budget)

	// no usable budget (prompt alone nearly fills the window) — no override, the
	// rejection/trim path takes over
	_, ok = dynamicGenerationBudget(k, 47200)
	assert.False(t, ok)

	// unlearned knowledge — no dynamic budget
	_, ok = dynamicGenerationBudget(contextWindowKnowledge{}, 1000)
	assert.False(t, ok)

	// anti-explosion ceiling: gemma4-style 4096 cap is respected even with a tiny prompt
	g := contextWindowKnowledge{contextTokens: 131072, maxOutTokens: 4096}
	budget, ok = dynamicGenerationBudget(g, 3000)
	assert.True(t, ok)
	assert.Equal(t, 4096, budget)
}

func TestEstimateChainTokens(t *testing.T) {
	chain := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, strings.Repeat("a", 4000)),
		llms.TextParts(llms.ChatMessageTypeHuman, strings.Repeat("b", 4000)),
		llms.MessageContent{Role: llms.ChatMessageTypeTool, Parts: []llms.ContentPart{
			llms.ToolCallResponse{Name: "terminal", Content: strings.Repeat("c", 8000)},
		}},
	}
	// 16008 bytes (8000+8000 text + "terminal" name) / 4 = 4002 estimated tokens
	assert.Equal(t, 4002, estimateChainTokens(chain))
	assert.Equal(t, 0, estimateChainTokens(nil))
}

func TestHandleContextWindowExceeded(t *testing.T) {
	fp := &flowProvider{mx: new(sync.RWMutex)}
	logger := testLoggerEntry()

	// unrelated error -> no action
	assert.Equal(t, contextWindowNone, fp.handleContextWindowExceeded(pconfig.OptionsTypePentester, errors.New("timeout"), logger))
	_, ok := fp.getContextWindowKnowledge(pconfig.OptionsTypePentester)
	assert.False(t, ok)

	// flow 100 error -> knowledge learned (ctx + configured budget) and clamp returned
	action := fp.handleContextWindowExceeded(pconfig.OptionsTypePentester, errors.New(flow100ContextWindowError), logger)
	assert.Equal(t, contextWindowClamped, action)
	k, ok := fp.getContextWindowKnowledge(pconfig.OptionsTypePentester)
	assert.True(t, ok)
	assert.Equal(t, 48000, k.contextTokens)
	assert.Equal(t, 16384, k.maxOutTokens) // parsed from "requested 16384 output tokens"

	// prompt alone fills the window -> trim requested
	assert.Equal(t, contextWindowNeedsTrim, fp.handleContextWindowExceeded(pconfig.OptionsTypePentester,
		errors.New("This model's maximum context length is 48000 tokens. However, your prompt contains at least 47800 input tokens"), logger))

	// unparsable message -> trim requested
	assert.Equal(t, contextWindowNeedsTrim, fp.handleContextWindowExceeded(pconfig.OptionsTypePentester,
		errors.New("context window exceeded for unknown reason"), logger))
}
