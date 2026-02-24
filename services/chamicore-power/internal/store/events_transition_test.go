package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"git.cscs.ch/openchami/chamicore-power/internal/engine"
)

func TestNewTransitionLifecycleEvent(t *testing.T) {
	now := time.Now().UTC()
	startedAt := now.Add(2 * time.Second)
	completedAt := now.Add(4 * time.Second)

	event, err := newTransitionLifecycleEvent(engine.Transition{
		ID:           "transition-1",
		RequestID:    "req-1",
		Operation:    "On",
		State:        engine.TransitionStateCompleted,
		RequestedBy:  "user-1",
		DryRun:       false,
		TargetCount:  2,
		SuccessCount: 2,
		FailureCount: 0,
		QueuedAt:     now,
		StartedAt:    &startedAt,
		CompletedAt:  &completedAt,
		CreatedAt:    now,
		UpdatedAt:    completedAt,
	})
	require.NoError(t, err)

	assert.Equal(t, transitionLifecycleEventType, event.Type)
	assert.Equal(t, transitionEventSource, event.Source)
	assert.Equal(t, "transition-1", event.Subject)

	var payload transitionLifecycleEventData
	require.NoError(t, json.Unmarshal(event.Data, &payload))
	assert.Equal(t, "transition-1", payload.TransitionID)
	assert.Equal(t, engine.TransitionStateCompleted, payload.Snapshot.State)
	assert.Equal(t, "On", payload.Snapshot.Operation)
	assert.Equal(t, 2, payload.Snapshot.SuccessCount)
}

func TestNewTransitionTaskResultEvent(t *testing.T) {
	now := time.Now().UTC()
	startedAt := now.Add(time.Second)
	completedAt := now.Add(2 * time.Second)

	event, err := newTransitionTaskResultEvent(engine.Task{
		ID:                 "task-1",
		TransitionID:       "transition-1",
		NodeID:             "node-1",
		BMCID:              "bmc-1",
		BMCEndpoint:        "https://bmc-1",
		CredentialID:       "cred-1",
		Operation:          "On",
		State:              engine.TaskStateSucceeded,
		DryRun:             false,
		AttemptCount:       1,
		FinalPowerState:    "On",
		QueuedAt:           now,
		StartedAt:          &startedAt,
		CompletedAt:        &completedAt,
		CreatedAt:          now,
		UpdatedAt:          completedAt,
		InsecureSkipVerify: true,
	})
	require.NoError(t, err)

	assert.Equal(t, transitionTaskResultEventType, event.Type)
	assert.Equal(t, "node-1", event.Subject)
	assert.Equal(t, transitionEventSource, event.Source)

	var payload transitionTaskResultEventData
	require.NoError(t, json.Unmarshal(event.Data, &payload))
	assert.Equal(t, "transition-1", payload.TransitionID)
	assert.Equal(t, "node-1", payload.NodeID)
	assert.Equal(t, "task-1", payload.TaskID)
	assert.Equal(t, "On", payload.Snapshot.FinalPowerState)
	assert.Equal(t, engine.TaskStateSucceeded, payload.Snapshot.State)
	assert.True(t, payload.Snapshot.InsecureSkipVerify)
}

func TestNewTransitionTaskResultEvent_Validation(t *testing.T) {
	_, err := newTransitionTaskResultEvent(engine.Task{
		TransitionID: "transition-1",
		NodeID:       "node-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task id is required")
}

func TestNewTransitionEventID_ReadRandomFailure(t *testing.T) {
	originalReadRandom := readTransitionEventRandom
	readTransitionEventRandom = func([]byte) (int, error) {
		return 0, errors.New("boom")
	}
	t.Cleanup(func() {
		readTransitionEventRandom = originalReadRandom
	})

	_, err := newTransitionEventID()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "generating event id")
}
