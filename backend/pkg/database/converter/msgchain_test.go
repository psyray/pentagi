package converter

import (
	"database/sql"
	"testing"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/graph/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConverter_ConvertMsgchain_MapsTheAgentUsageAndScope(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	got := ConvertMsgchain(database.Msgchain{
		ID:              42,
		Type:            database.MsgchainTypePentester,
		Model:           "kimi-k3",
		ModelProvider:   "openai",
		UsageIn:         18000,
		UsageOut:        900,
		UsageCacheIn:    4096,
		UsageCacheOut:   128,
		DurationSeconds: 4.2,
		FlowID:          1,
		TaskID:          sql.NullInt64{Int64: 7, Valid: true},
		SubtaskID:       sql.NullInt64{Int64: 9, Valid: true},
		CreatedAt:       sql.NullTime{Time: createdAt, Valid: true},
	})

	assert.Equal(t, int64(42), got.ID)
	assert.Equal(t, model.AgentType(database.MsgchainTypePentester), got.Type)
	assert.Equal(t, "kimi-k3", got.Model)
	assert.Equal(t, "openai", got.ModelProvider)
	assert.Equal(t, 18000, got.UsageIn)
	assert.Equal(t, 900, got.UsageOut)
	assert.Equal(t, 4096, got.UsageCacheIn)
	assert.Equal(t, 128, got.UsageCacheOut)
	assert.Equal(t, 4.2, got.DurationSeconds)
	assert.Equal(t, int64(1), got.FlowID)
	assert.Equal(t, createdAt, got.CreatedAt)
	require.NotNil(t, got.TaskID)
	require.NotNil(t, got.SubtaskID)
	assert.Equal(t, int64(7), *got.TaskID)
	assert.Equal(t, int64(9), *got.SubtaskID)
}

func TestConverter_ConvertMsgchain_LeavesTaskAndSubtaskUnsetWhenAbsent(t *testing.T) {
	t.Parallel()

	got := ConvertMsgchain(database.Msgchain{ID: 1, Type: database.MsgchainTypeAssistant})

	assert.Nil(t, got.TaskID)
	assert.Nil(t, got.SubtaskID)
}

func TestConverter_ConvertMsgchains_KeepsTheOrder(t *testing.T) {
	t.Parallel()

	got := ConvertMsgchains([]database.Msgchain{
		{ID: 2, Type: database.MsgchainTypePentester},
		{ID: 1, Type: database.MsgchainTypePrimaryAgent},
	})

	require.Len(t, got, 2)
	assert.Equal(t, int64(2), got[0].ID)
	assert.Equal(t, int64(1), got[1].ID)
}

func TestConverter_ConvertMessageLog_CarriesTheAgentWhenSet(t *testing.T) {
	t.Parallel()

	got := ConvertMessageLog(database.Msglog{
		ID:        1,
		Type:      database.MsglogTypeThoughts,
		AgentType: database.NullMsgchainType{MsgchainType: database.MsgchainTypePrimaryAgent, Valid: true},
	})

	require.NotNil(t, got.Agent)
	assert.Equal(t, model.AgentType(database.MsgchainTypePrimaryAgent), *got.Agent)
}

func TestConverter_ConvertMessageLog_LeavesTheAgentNilWhenUnattributed(t *testing.T) {
	t.Parallel()

	got := ConvertMessageLog(database.Msglog{ID: 1, Type: database.MsglogTypeThoughts})

	assert.Nil(t, got.Agent)
}
