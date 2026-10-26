package controller

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emptySubtaskController models a task whose plan is exhausted: the task is
// waiting, but no subtask is waiting to receive a new input.
type emptySubtaskController struct{}

func (emptySubtaskController) LoadSubtasks(context.Context, int64, TaskUpdater) error {
	return nil
}

func (emptySubtaskController) GenerateSubtasks(context.Context) error { return nil }

func (emptySubtaskController) RefineSubtasks(context.Context) error { return nil }

func (emptySubtaskController) PopSubtask(context.Context, TaskUpdater) (SubtaskWorker, error) {
	return nil, nil
}

func (emptySubtaskController) ListSubtasks(context.Context) []SubtaskWorker { return nil }

func (emptySubtaskController) GetSubtask(context.Context, int64) (SubtaskWorker, error) {
	return nil, nil
}

func (emptySubtaskController) InvalidateSubtasks([]int64) {}

// A waiting task with no waiting subtask used to accept the input, return nil,
// run with an empty queue and finish: the operator's instruction was silently
// lost (flow 130, B04 was never executed). The input must be refused explicitly.
func TestTask_PutInput_ReportsAnExhaustedPlanInsteadOfDroppingTheInput(t *testing.T) {
	t.Parallel()

	tw := &taskWorker{
		mx:      &sync.RWMutex{},
		waiting: true,
		stc:     emptySubtaskController{},
		taskCtx: &TaskContext{TaskID: 42},
	}

	err := tw.PutInput(context.Background(), "execute B04 now")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no waiting subtask")
	assert.Contains(t, err.Error(), "task 42")
}
