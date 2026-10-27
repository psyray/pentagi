package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// steerRecordingTask records the operator messages the flow hands to it.
type steerRecordingTask struct {
	TaskWorker

	completed bool
	waiting   bool
	steers    []string
	steerErr  error
}

func (t *steerRecordingTask) GetTaskID() int64  { return 42 }
func (t *steerRecordingTask) IsCompleted() bool { return t.completed }
func (t *steerRecordingTask) IsWaiting() bool   { return t.waiting }

func (t *steerRecordingTask) Steer(_ context.Context, input string) error {
	if t.steerErr != nil {
		return t.steerErr
	}

	t.steers = append(t.steers, input)

	return nil
}

func TestFlowWorker_SteerRunningTask_HandsTheMessageToTheActiveTask(t *testing.T) {
	t.Parallel()

	task := &steerRecordingTask{}
	fw := &flowWorker{
		tc:      &scriptedTasks{tasks: []TaskWorker{task}},
		ctx:     context.Background(),
		flowCtx: &FlowContext{FlowID: 7},
	}

	require.NoError(t, fw.steerRunningTask(context.Background(), "focus on A15 instead"))
	assert.Equal(t, []string{"focus on A15 instead"}, task.steers)
}

func TestFlowWorker_SteerRunningTask_RefusesWithoutAnActiveTask(t *testing.T) {
	t.Parallel()

	fw := &flowWorker{
		tc:      &scriptedTasks{tasks: []TaskWorker{}},
		ctx:     context.Background(),
		flowCtx: &FlowContext{FlowID: 7},
	}

	err := fw.steerRunningTask(context.Background(), "too late")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active task to steer")
}
