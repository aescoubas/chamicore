//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"git.cscs.ch/openchami/chamicore-power/internal/engine"
)

func TestPostgresStore_TransitionCRUD(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	createdTransition, createdTasks, err := st.CreateTransition(ctx, engine.Transition{
		RequestID:   "req-1",
		Operation:   "On",
		State:       engine.TransitionStatePending,
		RequestedBy: "tester",
		TargetCount: 2,
		QueuedAt:    now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []engine.Task{
		{
			NodeID:    "node-1",
			BMCID:     "bmc-1",
			Operation: "On",
			State:     engine.TaskStatePending,
			QueuedAt:  now,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			NodeID:    "node-2",
			BMCID:     "bmc-2",
			Operation: "On",
			State:     engine.TaskStatePending,
			QueuedAt:  now,
			CreatedAt: now,
			UpdatedAt: now,
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, createdTransition.ID)
	require.Len(t, createdTasks, 2)

	listedTransitions, total, err := st.ListTransitions(ctx, 100, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, listedTransitions, 1)
	assert.Equal(t, createdTransition.ID, listedTransitions[0].ID)

	fetchedTransition, err := st.GetTransition(ctx, createdTransition.ID)
	require.NoError(t, err)
	assert.Equal(t, engine.TransitionStatePending, fetchedTransition.State)
	assert.Equal(t, 2, fetchedTransition.TargetCount)

	tasks, err := st.ListTransitionTasks(ctx, createdTransition.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	assert.Equal(t, "node-1", tasks[0].NodeID)
	assert.Equal(t, "node-2", tasks[1].NodeID)

	fetchedTransition.State = engine.TransitionStatePartial
	fetchedTransition.SuccessCount = 1
	fetchedTransition.FailureCount = 1
	fetchedTransition.CompletedAt = ptrTime(now.Add(2 * time.Second))
	fetchedTransition.UpdatedAt = now.Add(2 * time.Second)

	updatedTransition, err := st.UpdateTransition(ctx, fetchedTransition)
	require.NoError(t, err)
	assert.Equal(t, engine.TransitionStatePartial, updatedTransition.State)
	assert.Equal(t, 1, updatedTransition.SuccessCount)
	assert.Equal(t, 1, updatedTransition.FailureCount)

	task := tasks[0]
	task.State = engine.TaskStateSucceeded
	task.AttemptCount = 1
	task.FinalPowerState = "On"
	task.CompletedAt = ptrTime(now.Add(time.Second))
	task.UpdatedAt = now.Add(time.Second)

	updatedTask, err := st.UpdateTransitionTask(ctx, task)
	require.NoError(t, err)
	assert.Equal(t, engine.TaskStateSucceeded, updatedTask.State)
	assert.Equal(t, "On", updatedTask.FinalPowerState)

	tasksAfterUpdate, err := st.ListTransitionTasks(ctx, createdTransition.ID)
	require.NoError(t, err)
	require.Len(t, tasksAfterUpdate, 2)
	assert.Equal(t, engine.TaskStateSucceeded, tasksAfterUpdate[0].State)
}

func TestPostgresStore_ListLatestTransitionTasksByNode(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	firstTransition, firstTasks, err := st.CreateTransition(ctx, engine.Transition{
		RequestID:   "req-a",
		Operation:   "On",
		State:       engine.TransitionStateCompleted,
		RequestedBy: "tester",
		TargetCount: 1,
		QueuedAt:    now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []engine.Task{{
		NodeID:    "node-1",
		BMCID:     "bmc-1",
		Operation: "On",
		State:     engine.TaskStateSucceeded,
		QueuedAt:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}})
	require.NoError(t, err)
	require.NotEmpty(t, firstTransition.ID)
	require.Len(t, firstTasks, 1)

	secondTime := now.Add(5 * time.Second)
	secondTransition, secondTasks, err := st.CreateTransition(ctx, engine.Transition{
		RequestID:   "req-b",
		Operation:   "ForceOff",
		State:       engine.TransitionStateCompleted,
		RequestedBy: "tester",
		TargetCount: 1,
		QueuedAt:    secondTime,
		CreatedAt:   secondTime,
		UpdatedAt:   secondTime,
	}, []engine.Task{{
		NodeID:          "node-1",
		BMCID:           "bmc-1",
		Operation:       "ForceOff",
		State:           engine.TaskStateSucceeded,
		FinalPowerState: "Off",
		QueuedAt:        secondTime,
		CreatedAt:       secondTime,
		UpdatedAt:       secondTime,
	}})
	require.NoError(t, err)
	require.NotEmpty(t, secondTransition.ID)
	require.Len(t, secondTasks, 1)

	latest, err := st.ListLatestTransitionTasksByNode(ctx, []string{"node-1", "node-2"})
	require.NoError(t, err)
	require.Len(t, latest, 1)
	assert.Equal(t, secondTransition.ID, latest[0].TransitionID)
	assert.Equal(t, "node-1", latest[0].NodeID)
	assert.Equal(t, "ForceOff", latest[0].Operation)
}

func ptrTime(v time.Time) *time.Time {
	return &v
}
