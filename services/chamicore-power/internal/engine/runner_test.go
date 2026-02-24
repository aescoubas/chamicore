package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"git.cscs.ch/openchami/chamicore-power/internal/model"
)

type mockExecutor struct {
	executeFn func(ctx context.Context, req ExecutionRequest) error
}

func (m *mockExecutor) ExecutePowerAction(ctx context.Context, req ExecutionRequest) error {
	if m.executeFn != nil {
		return m.executeFn(ctx, req)
	}
	return nil
}

type mockReader struct {
	readFn func(ctx context.Context, req ExecutionRequest) (string, error)
}

func (m *mockReader) ReadPowerState(ctx context.Context, req ExecutionRequest) (string, error) {
	if m.readFn != nil {
		return m.readFn(ctx, req)
	}
	return "On", nil
}

type mockStateUpdater struct {
	updateNodePowerStateFn func(ctx context.Context, nodeID, powerState string) error
}

func (m *mockStateUpdater) UpdateNodePowerState(ctx context.Context, nodeID, powerState string) error {
	if m.updateNodePowerStateFn != nil {
		return m.updateNodePowerStateFn(ctx, nodeID, powerState)
	}
	return nil
}

type memoryStore struct {
	mu sync.Mutex

	mappings map[string]model.NodePowerMapping
	missing  map[string]model.NodeMappingError

	transitionSeq int
	taskSeq       int

	transitions       map[string]Transition
	tasks             map[string]Task
	tasksByTransition map[string][]string
	terminal          map[string]chan struct{}
}

func newMemoryStore(mappings []model.NodePowerMapping, missing []model.NodeMappingError) *memoryStore {
	mappingByNode := make(map[string]model.NodePowerMapping, len(mappings))
	for _, mapping := range mappings {
		mappingByNode[strings.TrimSpace(mapping.NodeID)] = mapping
	}

	missingByNode := make(map[string]model.NodeMappingError, len(missing))
	for _, mappingErr := range missing {
		missingByNode[strings.TrimSpace(mappingErr.NodeID)] = mappingErr
	}

	return &memoryStore{
		mappings:          mappingByNode,
		missing:           missingByNode,
		transitions:       make(map[string]Transition),
		tasks:             make(map[string]Task),
		tasksByTransition: make(map[string][]string),
		terminal:          make(map[string]chan struct{}),
		transitionSeq:     1,
		taskSeq:           1,
	}
}

func (s *memoryStore) ResolveNodeMappings(
	ctx context.Context,
	nodeIDs []string,
) ([]model.NodePowerMapping, []model.NodeMappingError, error) {
	_ = ctx

	s.mu.Lock()
	defer s.mu.Unlock()

	resolved := make([]model.NodePowerMapping, 0, len(nodeIDs))
	missing := make([]model.NodeMappingError, 0)

	for _, nodeID := range nodeIDs {
		normalized := strings.TrimSpace(nodeID)
		if normalized == "" {
			continue
		}
		if mapping, ok := s.mappings[normalized]; ok {
			resolved = append(resolved, mapping)
			continue
		}
		if mappingErr, ok := s.missing[normalized]; ok {
			missing = append(missing, mappingErr)
			continue
		}
		missing = append(missing, model.MissingNodeMappingError(normalized))
	}

	return resolved, missing, nil
}

func (s *memoryStore) CreateTransition(
	ctx context.Context,
	transition Transition,
	tasks []Task,
) (Transition, []Task, error) {
	_ = ctx

	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(transition.ID) == "" {
		transition.ID = s.nextTransitionID()
	}
	if transition.CreatedAt.IsZero() {
		transition.CreatedAt = time.Now().UTC()
	}
	if transition.UpdatedAt.IsZero() {
		transition.UpdatedAt = transition.CreatedAt
	}

	s.transitions[transition.ID] = transition
	if _, exists := s.terminal[transition.ID]; !exists {
		s.terminal[transition.ID] = make(chan struct{})
	}

	createdTasks := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		if strings.TrimSpace(task.ID) == "" {
			task.ID = s.nextTaskID()
		}
		task.TransitionID = transition.ID
		if task.CreatedAt.IsZero() {
			task.CreatedAt = transition.CreatedAt
		}
		if task.UpdatedAt.IsZero() {
			task.UpdatedAt = task.CreatedAt
		}

		s.tasks[task.ID] = task
		s.tasksByTransition[transition.ID] = append(s.tasksByTransition[transition.ID], task.ID)
		createdTasks = append(createdTasks, task)
	}

	if isTerminalTransitionState(transition.State) {
		s.closeTerminalLocked(transition.ID)
	}

	return transition, createdTasks, nil
}

func (s *memoryStore) UpdateTransition(ctx context.Context, transition Transition) (Transition, error) {
	_ = ctx

	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(transition.ID) == "" {
		return Transition{}, errors.New("transition id is required")
	}

	s.transitions[transition.ID] = transition
	if isTerminalTransitionState(transition.State) {
		s.closeTerminalLocked(transition.ID)
	}
	return transition, nil
}

func (s *memoryStore) UpdateTransitionTask(ctx context.Context, task Task) (Task, error) {
	_ = ctx

	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(task.ID) == "" {
		return Task{}, errors.New("task id is required")
	}

	s.tasks[task.ID] = task
	return task, nil
}

func (s *memoryStore) waitForTerminal(transitionID string, timeout time.Duration) bool {
	s.mu.Lock()
	ch, ok := s.terminal[transitionID]
	s.mu.Unlock()
	if !ok {
		return false
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ch:
		return true
	case <-timer.C:
		return false
	}
}

func (s *memoryStore) transition(transitionID string) Transition {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transitions[transitionID]
}

func (s *memoryStore) tasksForTransition(transitionID string) []Task {
	s.mu.Lock()
	defer s.mu.Unlock()

	taskIDs := append([]string(nil), s.tasksByTransition[transitionID]...)
	tasks := make([]Task, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		tasks = append(tasks, s.tasks[taskID])
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].NodeID < tasks[j].NodeID
	})
	return tasks
}

func (s *memoryStore) closeTerminalLocked(transitionID string) {
	ch, ok := s.terminal[transitionID]
	if !ok {
		ch = make(chan struct{})
		s.terminal[transitionID] = ch
	}

	select {
	case <-ch:
		return
	default:
		close(ch)
	}
}

func (s *memoryStore) nextTransitionID() string {
	id := s.transitionSeq
	s.transitionSeq++
	return "transition-" + strconvItoa(id)
}

func (s *memoryStore) nextTaskID() string {
	id := s.taskSeq
	s.taskSeq++
	return "task-" + strconvItoa(id)
}

func strconvItoa(v int) string {
	return fmt.Sprintf("%d", v)
}

func isTerminalTransitionState(state string) bool {
	switch state {
	case TransitionStateCompleted, TransitionStateFailed, TransitionStatePartial, TransitionStateCanceled, TransitionStatePlanned:
		return true
	default:
		return false
	}
}

type concurrencyExecutor struct {
	mu sync.Mutex

	holdDuration time.Duration
	currentTotal int
	maxTotal     int
	currentByBMC map[string]int
	maxByBMC     map[string]int
}

func newConcurrencyExecutor(holdDuration time.Duration) *concurrencyExecutor {
	return &concurrencyExecutor{
		holdDuration: holdDuration,
		currentByBMC: map[string]int{},
		maxByBMC:     map[string]int{},
	}
}

func (e *concurrencyExecutor) ExecutePowerAction(ctx context.Context, req ExecutionRequest) error {
	e.mu.Lock()
	e.currentTotal++
	if e.currentTotal > e.maxTotal {
		e.maxTotal = e.currentTotal
	}
	e.currentByBMC[req.BMCID]++
	if e.currentByBMC[req.BMCID] > e.maxByBMC[req.BMCID] {
		e.maxByBMC[req.BMCID] = e.currentByBMC[req.BMCID]
	}
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.currentTotal--
		e.currentByBMC[req.BMCID]--
		e.mu.Unlock()
	}()

	timer := time.NewTimer(e.holdDuration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (e *concurrencyExecutor) maxGlobal() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.maxTotal
}

func (e *concurrencyExecutor) maxForBMC(bmcID string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.maxByBMC[bmcID]
}

func TestRunner_EnforcesGlobalAndPerBMCConcurrency(t *testing.T) {
	store := newMemoryStore([]model.NodePowerMapping{
		{NodeID: "node-1", BMCID: "bmc-a", Endpoint: "https://bmc-a", CredentialID: "cred-a"},
		{NodeID: "node-2", BMCID: "bmc-a", Endpoint: "https://bmc-a", CredentialID: "cred-a"},
		{NodeID: "node-3", BMCID: "bmc-b", Endpoint: "https://bmc-b", CredentialID: "cred-b"},
		{NodeID: "node-4", BMCID: "bmc-b", Endpoint: "https://bmc-b", CredentialID: "cred-b"},
	}, nil)

	exec := newConcurrencyExecutor(35 * time.Millisecond)
	reader := &mockReader{readFn: func(ctx context.Context, req ExecutionRequest) (string, error) {
		return "On", nil
	}}

	runner := New(store, exec, reader, Config{
		GlobalConcurrency:  2,
		PerBMCConcurrency:  1,
		RetryAttempts:      1,
		VerificationWindow: 200 * time.Millisecond,
		VerificationPoll:   5 * time.Millisecond,
		TransitionDeadline: 500 * time.Millisecond,
	})
	runner.cfg.jitter = func(time.Duration) time.Duration { return 0 }

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(runCtx)

	transition, err := runner.StartTransition(context.Background(), StartRequest{
		Operation: "On",
		NodeIDs:   []string{"node-1", "node-2", "node-3", "node-4"},
	})
	require.NoError(t, err)

	require.True(t, store.waitForTerminal(transition.ID, 3*time.Second))
	finalTransition := store.transition(transition.ID)
	assert.Equal(t, TransitionStateCompleted, finalTransition.State)
	assert.Equal(t, 4, finalTransition.SuccessCount)
	assert.Equal(t, 0, finalTransition.FailureCount)

	assert.LessOrEqual(t, exec.maxGlobal(), 2)
	assert.LessOrEqual(t, exec.maxForBMC("bmc-a"), 1)
	assert.LessOrEqual(t, exec.maxForBMC("bmc-b"), 1)
}

func TestRunner_RetryPolicyOnlyRetriesRetryableErrors(t *testing.T) {
	t.Run("non-retryable failure", func(t *testing.T) {
		store := newMemoryStore([]model.NodePowerMapping{{
			NodeID: "node-1", BMCID: "bmc-1", Endpoint: "https://bmc-1", CredentialID: "cred-1",
		}}, nil)

		var calls atomic.Int32
		exec := &mockExecutor{executeFn: func(ctx context.Context, req ExecutionRequest) error {
			calls.Add(1)
			return errors.New("permanent failure")
		}}
		reader := &mockReader{readFn: func(ctx context.Context, req ExecutionRequest) (string, error) {
			return "On", nil
		}}

		runner := New(store, exec, reader, Config{
			RetryAttempts:      4,
			RetryBackoffBase:   time.Millisecond,
			RetryBackoffMax:    time.Millisecond,
			TransitionDeadline: 200 * time.Millisecond,
		})
		runner.cfg.jitter = func(time.Duration) time.Duration { return 0 }
		runner.cfg.sleep = func(context.Context, time.Duration) error { return nil }

		runCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		runner.Start(runCtx)

		transition, err := runner.StartTransition(context.Background(), StartRequest{
			Operation: "On",
			NodeIDs:   []string{"node-1"},
		})
		require.NoError(t, err)
		require.True(t, store.waitForTerminal(transition.ID, time.Second))

		tasks := store.tasksForTransition(transition.ID)
		require.Len(t, tasks, 1)
		assert.Equal(t, int32(1), calls.Load())
		assert.Equal(t, 1, tasks[0].AttemptCount)
		assert.Equal(t, TaskStateFailed, tasks[0].State)
	})

	t.Run("retryable failure then success", func(t *testing.T) {
		store := newMemoryStore([]model.NodePowerMapping{{
			NodeID: "node-1", BMCID: "bmc-1", Endpoint: "https://bmc-1", CredentialID: "cred-1",
		}}, nil)

		var calls atomic.Int32
		exec := &mockExecutor{executeFn: func(ctx context.Context, req ExecutionRequest) error {
			attempt := calls.Add(1)
			if attempt < 3 {
				return MarkRetryable(errors.New("temporary transport error"))
			}
			return nil
		}}
		reader := &mockReader{readFn: func(ctx context.Context, req ExecutionRequest) (string, error) {
			return "On", nil
		}}

		runner := New(store, exec, reader, Config{
			RetryAttempts:      4,
			RetryBackoffBase:   time.Millisecond,
			RetryBackoffMax:    time.Millisecond,
			TransitionDeadline: 200 * time.Millisecond,
		})
		runner.cfg.jitter = func(time.Duration) time.Duration { return 0 }
		runner.cfg.sleep = func(context.Context, time.Duration) error { return nil }

		runCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		runner.Start(runCtx)

		transition, err := runner.StartTransition(context.Background(), StartRequest{
			Operation: "On",
			NodeIDs:   []string{"node-1"},
		})
		require.NoError(t, err)
		require.True(t, store.waitForTerminal(transition.ID, time.Second))

		tasks := store.tasksForTransition(transition.ID)
		require.Len(t, tasks, 1)
		assert.Equal(t, int32(3), calls.Load())
		assert.Equal(t, 3, tasks[0].AttemptCount)
		assert.Equal(t, TaskStateSucceeded, tasks[0].State)
	})
}

func TestRunner_VerificationMarksPerNodeSuccessAndFailure(t *testing.T) {
	store := newMemoryStore([]model.NodePowerMapping{
		{NodeID: "node-ok", BMCID: "bmc-a", Endpoint: "https://bmc-a", CredentialID: "cred-a"},
		{NodeID: "node-bad", BMCID: "bmc-b", Endpoint: "https://bmc-b", CredentialID: "cred-b"},
	}, nil)

	exec := &mockExecutor{executeFn: func(ctx context.Context, req ExecutionRequest) error {
		return nil
	}}
	reader := &mockReader{readFn: func(ctx context.Context, req ExecutionRequest) (string, error) {
		if req.NodeID == "node-ok" {
			return "On", nil
		}
		return "Off", nil
	}}

	runner := New(store, exec, reader, Config{
		GlobalConcurrency:  2,
		PerBMCConcurrency:  1,
		RetryAttempts:      1,
		VerificationWindow: 60 * time.Millisecond,
		VerificationPoll:   10 * time.Millisecond,
		TransitionDeadline: 250 * time.Millisecond,
	})
	runner.cfg.jitter = func(time.Duration) time.Duration { return 0 }

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(runCtx)

	transition, err := runner.StartTransition(context.Background(), StartRequest{
		Operation: "On",
		NodeIDs:   []string{"node-ok", "node-bad"},
	})
	require.NoError(t, err)
	require.True(t, store.waitForTerminal(transition.ID, 2*time.Second))

	finalTransition := store.transition(transition.ID)
	assert.Equal(t, TransitionStatePartial, finalTransition.State)
	assert.Equal(t, 1, finalTransition.SuccessCount)
	assert.Equal(t, 1, finalTransition.FailureCount)

	tasks := store.tasksForTransition(transition.ID)
	require.Len(t, tasks, 2)

	byNode := make(map[string]Task, len(tasks))
	for _, task := range tasks {
		byNode[task.NodeID] = task
	}

	assert.Equal(t, TaskStateSucceeded, byNode["node-ok"].State)
	assert.Equal(t, "On", byNode["node-ok"].FinalPowerState)

	assert.Equal(t, TaskStateFailed, byNode["node-bad"].State)
	assert.Contains(t, byNode["node-bad"].ErrorDetail, ErrVerificationTimeout.Error())
}

func TestRunner_DryRunCreatesPlannedRecords(t *testing.T) {
	store := newMemoryStore([]model.NodePowerMapping{
		{NodeID: "node-1", BMCID: "bmc-1", Endpoint: "https://bmc-1", CredentialID: "cred-1"},
		{NodeID: "node-2", BMCID: "bmc-2", Endpoint: "https://bmc-2", CredentialID: "cred-2"},
	}, nil)

	var calls atomic.Int32
	exec := &mockExecutor{executeFn: func(ctx context.Context, req ExecutionRequest) error {
		calls.Add(1)
		return nil
	}}
	reader := &mockReader{readFn: func(ctx context.Context, req ExecutionRequest) (string, error) {
		return "On", nil
	}}

	runner := New(store, exec, reader, Config{})
	runner.cfg.jitter = func(time.Duration) time.Duration { return 0 }

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(runCtx)

	transition, err := runner.StartTransition(context.Background(), StartRequest{
		Operation: "On",
		NodeIDs:   []string{"node-1", "node-2"},
		DryRun:    true,
	})
	require.NoError(t, err)
	require.True(t, store.waitForTerminal(transition.ID, time.Second))

	finalTransition := store.transition(transition.ID)
	assert.Equal(t, TransitionStatePlanned, finalTransition.State)
	assert.Equal(t, 0, finalTransition.SuccessCount)
	assert.Equal(t, 0, finalTransition.FailureCount)

	tasks := store.tasksForTransition(transition.ID)
	require.Len(t, tasks, 2)
	for _, task := range tasks {
		assert.Equal(t, TaskStatePlanned, task.State)
		assert.Equal(t, 0, task.AttemptCount)
		require.NotNil(t, task.CompletedAt)
	}

	assert.Equal(t, int32(0), calls.Load())
}

func TestRunner_SuccessfulTaskUpdatesSMDState(t *testing.T) {
	store := newMemoryStore([]model.NodePowerMapping{{
		NodeID: "node-1", BMCID: "bmc-1", Endpoint: "https://bmc-1", CredentialID: "cred-1",
	}}, nil)

	exec := &mockExecutor{executeFn: func(ctx context.Context, req ExecutionRequest) error {
		return nil
	}}
	reader := &mockReader{readFn: func(ctx context.Context, req ExecutionRequest) (string, error) {
		return "On", nil
	}}

	var updatedNodeID string
	var updatedPowerState string
	updater := &mockStateUpdater{
		updateNodePowerStateFn: func(ctx context.Context, nodeID, powerState string) error {
			updatedNodeID = nodeID
			updatedPowerState = powerState
			return nil
		},
	}

	runner := New(store, exec, reader, Config{}, WithNodeStateUpdater(updater))
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(runCtx)

	transition, err := runner.StartTransition(context.Background(), StartRequest{
		Operation: "On",
		NodeIDs:   []string{"node-1"},
	})
	require.NoError(t, err)
	require.True(t, store.waitForTerminal(transition.ID, time.Second))

	assert.Equal(t, "node-1", updatedNodeID)
	assert.Equal(t, "On", updatedPowerState)

	tasks := store.tasksForTransition(transition.ID)
	require.Len(t, tasks, 1)
	assert.Equal(t, TaskStateSucceeded, tasks[0].State)
}

func TestRunner_SMDUpdateFailureMarksTaskFailed(t *testing.T) {
	store := newMemoryStore([]model.NodePowerMapping{{
		NodeID: "node-1", BMCID: "bmc-1", Endpoint: "https://bmc-1", CredentialID: "cred-1",
	}}, nil)

	exec := &mockExecutor{executeFn: func(ctx context.Context, req ExecutionRequest) error {
		return nil
	}}
	reader := &mockReader{readFn: func(ctx context.Context, req ExecutionRequest) (string, error) {
		return "On", nil
	}}
	updater := &mockStateUpdater{
		updateNodePowerStateFn: func(ctx context.Context, nodeID, powerState string) error {
			return errors.New("smd patch failed")
		},
	}

	runner := New(store, exec, reader, Config{}, WithNodeStateUpdater(updater))
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(runCtx)

	transition, err := runner.StartTransition(context.Background(), StartRequest{
		Operation: "On",
		NodeIDs:   []string{"node-1"},
	})
	require.NoError(t, err)
	require.True(t, store.waitForTerminal(transition.ID, time.Second))

	finalTransition := store.transition(transition.ID)
	assert.Equal(t, TransitionStateFailed, finalTransition.State)
	assert.Equal(t, 0, finalTransition.SuccessCount)
	assert.Equal(t, 1, finalTransition.FailureCount)

	tasks := store.tasksForTransition(transition.ID)
	require.Len(t, tasks, 1)
	assert.Equal(t, TaskStateFailed, tasks[0].State)
	assert.Contains(t, tasks[0].ErrorDetail, "updating SMD state")
}

func TestRunner_CancellationMarksTasksCanceled(t *testing.T) {
	store := newMemoryStore([]model.NodePowerMapping{{
		NodeID: "node-1", BMCID: "bmc-1", Endpoint: "https://bmc-1", CredentialID: "cred-1",
	}}, nil)

	started := make(chan struct{}, 1)
	exec := &mockExecutor{executeFn: func(ctx context.Context, req ExecutionRequest) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	reader := &mockReader{}

	runner := New(store, exec, reader, Config{RetryAttempts: 1})
	runner.cfg.jitter = func(time.Duration) time.Duration { return 0 }

	runCtx, cancel := context.WithCancel(context.Background())
	runner.Start(runCtx)

	transition, err := runner.StartTransition(context.Background(), StartRequest{
		Operation: "On",
		NodeIDs:   []string{"node-1"},
	})
	require.NoError(t, err)

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("task execution did not start")
	}
	cancel()

	require.True(t, store.waitForTerminal(transition.ID, 2*time.Second))
	finalTransition := store.transition(transition.ID)
	assert.Equal(t, TransitionStateCanceled, finalTransition.State)

	tasks := store.tasksForTransition(transition.ID)
	require.Len(t, tasks, 1)
	assert.Equal(t, TaskStateCanceled, tasks[0].State)
}

func TestRunner_RetriesExhaustedMarksFailure(t *testing.T) {
	store := newMemoryStore([]model.NodePowerMapping{{
		NodeID: "node-1", BMCID: "bmc-1", Endpoint: "https://bmc-1", CredentialID: "cred-1",
	}}, nil)

	var calls atomic.Int32
	exec := &mockExecutor{executeFn: func(ctx context.Context, req ExecutionRequest) error {
		calls.Add(1)
		return MarkRetryable(errors.New("temporary network failure"))
	}}
	reader := &mockReader{}

	runner := New(store, exec, reader, Config{
		RetryAttempts:      3,
		RetryBackoffBase:   time.Millisecond,
		RetryBackoffMax:    time.Millisecond,
		TransitionDeadline: 200 * time.Millisecond,
	})
	runner.cfg.jitter = func(time.Duration) time.Duration { return 0 }
	runner.cfg.sleep = func(context.Context, time.Duration) error { return nil }

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(runCtx)

	transition, err := runner.StartTransition(context.Background(), StartRequest{
		Operation: "On",
		NodeIDs:   []string{"node-1"},
	})
	require.NoError(t, err)
	require.True(t, store.waitForTerminal(transition.ID, time.Second))

	assert.Equal(t, int32(3), calls.Load())
	finalTransition := store.transition(transition.ID)
	assert.Equal(t, TransitionStateFailed, finalTransition.State)
	assert.Equal(t, 0, finalTransition.SuccessCount)
	assert.Equal(t, 1, finalTransition.FailureCount)

	tasks := store.tasksForTransition(transition.ID)
	require.Len(t, tasks, 1)
	assert.Equal(t, TaskStateFailed, tasks[0].State)
	assert.Equal(t, 3, tasks[0].AttemptCount)
}

func TestRunner_AbortTransition_CancelsTasks(t *testing.T) {
	store := newMemoryStore([]model.NodePowerMapping{
		{NodeID: "node-1", BMCID: "bmc-1", Endpoint: "https://bmc-1", CredentialID: "cred-1"},
		{NodeID: "node-2", BMCID: "bmc-2", Endpoint: "https://bmc-2", CredentialID: "cred-2"},
	}, nil)

	exec := &mockExecutor{executeFn: func(ctx context.Context, req ExecutionRequest) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	reader := &mockReader{readFn: func(ctx context.Context, req ExecutionRequest) (string, error) {
		return "On", nil
	}}

	runner := New(store, exec, reader, Config{
		GlobalConcurrency:  1,
		PerBMCConcurrency:  1,
		RetryAttempts:      1,
		TransitionDeadline: 2 * time.Second,
	})

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(runCtx)

	transition, err := runner.StartTransition(context.Background(), StartRequest{
		Operation: "On",
		NodeIDs:   []string{"node-1", "node-2"},
	})
	require.NoError(t, err)

	require.NoError(t, runner.AbortTransition(context.Background(), transition.ID))
	require.Eventually(t, func() bool {
		tasks := store.tasksForTransition(transition.ID)
		if len(tasks) != 2 {
			return false
		}
		for _, task := range tasks {
			if task.State == TaskStatePending {
				return false
			}
		}
		finalTransition := store.transition(transition.ID)
		return finalTransition.State == TransitionStateCanceled && finalTransition.FailureCount == 2
	}, 2*time.Second, 10*time.Millisecond)

	finalTransition := store.transition(transition.ID)
	assert.Equal(t, TransitionStateCanceled, finalTransition.State)
	assert.Equal(t, 0, finalTransition.SuccessCount)
	assert.Equal(t, 2, finalTransition.FailureCount)

	tasks := store.tasksForTransition(transition.ID)
	require.Len(t, tasks, 2)
	assert.Equal(t, TaskStateCanceled, tasks[0].State)
	assert.Equal(t, TaskStateCanceled, tasks[1].State)
}
