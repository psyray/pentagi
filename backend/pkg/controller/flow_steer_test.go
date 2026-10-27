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

func TestFlowWorker_ProcessInput_HandsASteerToTheRunningTask(t *testing.T) {
	t.Parallel()

	task := &steerRecordingTask{}
	fw := &flowWorker{
		tc:      &scriptedTasks{tasks: []TaskWorker{task}},
		ctx:     context.Background(),
		flowCtx: &FlowContext{FlowID: 7},
	}

	got, err := fw.processInput(flowInput{
		input: "focus on A15 instead",
		mode:  InputModeSteer,
		reply: newInputReply(),
	})

	require.NoError(t, err)
	assert.Equal(t, task, got)
	assert.Equal(t, []string{"focus on A15 instead"}, task.steers)
}

func TestFlowWorker_ProcessInput_RefusesToSteerWithoutARunningTask(t *testing.T) {
	t.Parallel()

	fw := &flowWorker{
		tc:      &scriptedTasks{tasks: []TaskWorker{&steerRecordingTask{waiting: true}}},
		ctx:     context.Background(),
		flowCtx: &FlowContext{FlowID: 7},
	}

	_, err := fw.processInput(flowInput{
		input: "too late",
		mode:  InputModeSteer,
		reply: newInputReply(),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no running task to steer")
}
