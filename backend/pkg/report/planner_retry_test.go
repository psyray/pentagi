package report

import (
	"context"
	"fmt"
	"testing"

	"github.com/vxcontrol/langchaingo/llms"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plannerResponse builds the single-choice tool-call response the planner reads.
func plannerResponse(args string) *llms.ContentResponse {
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
		ToolCalls: []llms.ToolCall{{
			FunctionCall: &llms.FunctionCall{Name: planToolName, Arguments: args},
		}},
	}}}
}

// validPlanPayload is the smallest plan that decodes and validates.
const validPlanPayload = `{"title":"T","sections_estimate":1,"sections":[{"key":"cover","action":"include"}]}`

// malformedSectionsPayload is the prod failure shape (2026-10-20): the model
// stringified the sections array and wrote an extra colon inside it.
const malformedSectionsPayload = `{"title":"T","sections_estimate":1,"sections":"[{\"key\":\"cover\":\"x\",\"action\":\"include\"}]"}`

func TestPlanWithRetry_RecoversFromAMalformedPayload(t *testing.T) {
	responses := []*llms.ContentResponse{
		plannerResponse(malformedSectionsPayload),
		plannerResponse(validPlanPayload),
	}
	chains := make([][]llms.MessageContent, 0, len(responses))
	call := func(_ context.Context, chain []llms.MessageContent) (*llms.ContentResponse, error) {
		chains = append(chains, chain)
		return responses[len(chains)-1], nil
	}

	got, attempts, err := planWithRetry(context.Background(), call, []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "plan now"),
	})

	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
	assert.Equal(t, "T", got.Title)
	require.Len(t, chains, 2)
	// The retry carries the decode error back to the model.
	assert.Greater(t, len(chains[1]), len(chains[0]))
}

func TestPlanWithRetry_GivesUpAfterTheAttemptBudget(t *testing.T) {
	call := func(_ context.Context, _ []llms.MessageContent) (*llms.ContentResponse, error) {
		return plannerResponse(malformedSectionsPayload), nil
	}

	_, attempts, err := planWithRetry(context.Background(), call, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "plan sections payload")
	assert.Equal(t, planDecodeAttempts, attempts)
}

func TestPlanWithRetry_StopsOnAProviderError(t *testing.T) {
	calls := 0
	call := func(_ context.Context, _ []llms.MessageContent) (*llms.ContentResponse, error) {
		calls++
		return nil, fmt.Errorf("provider unavailable")
	}

	_, attempts, err := planWithRetry(context.Background(), call, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "planner call failed: provider unavailable")
	assert.Equal(t, 1, attempts)
	assert.Equal(t, 1, calls)
}

func TestPlanWithRetry_RetriesOnAValidationRejection(t *testing.T) {
	invalid := `{"title":"T","sections_estimate":1,"sections":[{"key":"cover","action":"archive"}]}`
	responses := []*llms.ContentResponse{
		plannerResponse(invalid),
		plannerResponse(validPlanPayload),
	}
	calls := 0
	call := func(_ context.Context, _ []llms.MessageContent) (*llms.ContentResponse, error) {
		calls++
		return responses[calls-1], nil
	}

	got, attempts, err := planWithRetry(context.Background(), call, nil)

	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
	assert.Equal(t, "T", got.Title)
}
