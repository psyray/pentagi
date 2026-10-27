package controller

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type steerRecordingSubtask struct {
	SubtaskWorker

	completed bool
	waiting   bool
	steers    []string
}

func (s *steerRecordingSubtask) GetSubtaskID() int64 { return 5 }
func (s *steerRecordingSubtask) IsCompleted() bool   { return s.completed }
func (s *steerRecordingSubtask) IsWaiting() bool     { return s.waiting }

func (s *steerRecordingSubtask) Steer(_ context.Context, input string) error {
	s.steers = append(s.steers, input)

	return nil
}

func (s *steerRecordingSubtask) DrainSteers() []string { return nil }

type steerSubtaskController struct {
	subtasks []SubtaskWorker
}

func (c steerSubtaskController) LoadSubtasks(context.Context, int64, TaskUpdater) error {
	return nil
}
func (c steerSubtaskController) GenerateSubtasks(context.Context) error { return nil }
func (c steerSubtaskController) RefineSubtasks(context.Context) error   { return nil }
func (c steerSubtaskController) PopSubtask(context.Context, TaskUpdater) (SubtaskWorker, error) {
	return nil, nil
}
func (c steerSubtaskController) ListSubtasks(context.Context) []SubtaskWorker {
	return c.subtasks
}
func (c steerSubtaskController) GetSubtask(context.Context, int64) (SubtaskWorker, error) {
	return nil, nil
}
func (c steerSubtaskController) InvalidateSubtasks([]int64) {}

func TestTaskWorker_Steer_HandsTheMessageToTheRunningSubtask(t *testing.T) {
	t.Parallel()

	sub := &steerRecordingSubtask{}
	tw := &taskWorker{
		mx:      &sync.RWMutex{},
		stc:     steerSubtaskController{subtasks: []SubtaskWorker{sub}},
		taskCtx: &TaskContext{TaskID: 42},
	}

	require.NoError(t, tw.Steer(context.Background(), "focus on A15"))
	assert.Equal(t, []string{"focus on A15"}, sub.steers)
}

func TestTaskWorker_Steer_ReachesARunningSubtaskEvenWhenTheTaskIsWaiting(t *testing.T) {
	t.Parallel()

	sub := &steerRecordingSubtask{}
	tw := &taskWorker{
		mx:      &sync.RWMutex{},
		stc:     steerSubtaskController{subtasks: []SubtaskWorker{sub}},
		taskCtx: &TaskContext{TaskID: 42},
		waiting: true,
	}

	require.NoError(t, tw.Steer(context.Background(), "keep going on the current subtask"))
	assert.Equal(t, []string{"keep going on the current subtask"}, sub.steers)
}

func TestTaskWorker_Steer_QueuesBetweenSubtasks(t *testing.T) {
	t.Parallel()

	tw := &taskWorker{
		mx:      &sync.RWMutex{},
		stc:     steerSubtaskController{},
		taskCtx: &TaskContext{TaskID: 42},
	}

	require.NoError(t, tw.Steer(context.Background(), "wait for the next block"))
	assert.Equal(t, []string{"wait for the next block"}, tw.drainPendingSteers())
	assert.Nil(t, tw.drainPendingSteers())
}

func TestSubtaskWorker_Steer_QueuesForTheNextIteration(t *testing.T) {
	t.Parallel()

	stw := &subtaskWorker{
		mx:         &sync.RWMutex{},
		subtaskCtx: &SubtaskContext{SubtaskID: 9},
		steerCh:    make(chan string, 1),
	}

	require.NoError(t, stw.Steer(context.Background(), "change the plan"))

	select {
	case got := <-stw.steerCh:
		assert.Equal(t, "change the plan", got)
	default:
		t.Fatal("the steer was not queued for the next iteration")
	}
}

func TestSubtaskWorker_Steer_RefusesACompletedSubtask(t *testing.T) {
	t.Parallel()

	stw := &subtaskWorker{
		mx:         &sync.RWMutex{},
		subtaskCtx: &SubtaskContext{SubtaskID: 9},
		steerCh:    make(chan string, 1),
		completed:  true,
	}

	require.Error(t, stw.Steer(context.Background(), "too late"))
}
