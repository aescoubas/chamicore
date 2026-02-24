package engine

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"git.cscs.ch/openchami/chamicore-lib/redfish"
	"git.cscs.ch/openchami/chamicore-power/internal/model"
)

const (
	defaultGlobalConcurrency = 20
	defaultPerBMCConcurrency = 1
	defaultRetryAttempts     = 3
	defaultRetryBackoffBase  = 250 * time.Millisecond
	defaultRetryBackoffMax   = 5 * time.Second
	defaultTransitionTimeout = 90 * time.Second
)

var (
	// ErrRunnerNotStarted indicates Start must be called before creating transitions.
	ErrRunnerNotStarted = errors.New("runner not started")
	// ErrNoTargetNodes indicates no target node IDs were provided.
	ErrNoTargetNodes = errors.New("at least one node is required")
)

// RetryableError wraps an error that should be retried by policy.
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string {
	if e == nil || e.Err == nil {
		return "retryable error"
	}
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// MarkRetryable marks an error as retryable.
func MarkRetryable(err error) error {
	if err == nil {
		return nil
	}
	return &RetryableError{Err: err}
}

// IsRetryable reports whether an error should be retried.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var retryable *RetryableError
	if errors.As(err, &retryable) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}

	return false
}

// TransitionState values.
const (
	TransitionStatePending   = "pending"
	TransitionStateRunning   = "running"
	TransitionStateCompleted = "completed"
	TransitionStateFailed    = "failed"
	TransitionStatePartial   = "partial"
	TransitionStateCanceled  = "canceled"
	TransitionStatePlanned   = "planned"
)

// TaskState values.
const (
	TaskStatePending   = "pending"
	TaskStateRunning   = "running"
	TaskStateSucceeded = "succeeded"
	TaskStateFailed    = "failed"
	TaskStateCanceled  = "canceled"
	TaskStatePlanned   = "planned"
)

// Transition is the execution record persisted by the runner.
type Transition struct {
	ID           string
	RequestID    string
	Operation    string
	State        string
	RequestedBy  string
	DryRun       bool
	TargetCount  int
	SuccessCount int
	FailureCount int
	QueuedAt     time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Task is the per-node execution record persisted by the runner.
type Task struct {
	ID                 string
	TransitionID       string
	NodeID             string
	BMCID              string
	BMCEndpoint        string
	CredentialID       string
	Operation          string
	State              string
	DryRun             bool
	InsecureSkipVerify bool
	AttemptCount       int
	FinalPowerState    string
	ErrorDetail        string
	QueuedAt           time.Time
	StartedAt          *time.Time
	CompletedAt        *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// StartRequest describes one transition request.
type StartRequest struct {
	RequestID   string
	RequestedBy string
	Operation   string
	NodeIDs     []string
	DryRun      bool
}

// ExecutionRequest is passed to executor/verification backends.
type ExecutionRequest struct {
	TransitionID       string
	TaskID             string
	NodeID             string
	BMCID              string
	Endpoint           string
	CredentialID       string
	InsecureSkipVerify bool
	Operation          redfish.ResetOperation
}

// Store defines persistence methods required by the runner.
type Store interface {
	ResolveNodeMappings(ctx context.Context, nodeIDs []string) ([]model.NodePowerMapping, []model.NodeMappingError, error)
	CreateTransition(ctx context.Context, transition Transition, tasks []Task) (Transition, []Task, error)
	UpdateTransition(ctx context.Context, transition Transition) (Transition, error)
	UpdateTransitionTask(ctx context.Context, task Task) (Task, error)
}

// Executor executes one node power action.
type Executor interface {
	ExecutePowerAction(ctx context.Context, req ExecutionRequest) error
}

// Config controls execution policy.
type Config struct {
	GlobalConcurrency  int
	PerBMCConcurrency  int
	RetryAttempts      int
	RetryBackoffBase   time.Duration
	RetryBackoffMax    time.Duration
	TransitionDeadline time.Duration
	VerificationWindow time.Duration
	VerificationPoll   time.Duration
	QueueSize          int
}

type runtimeConfig struct {
	globalConcurrency  int
	perBMCConcurrency  int
	retryAttempts      int
	retryBackoffBase   time.Duration
	retryBackoffMax    time.Duration
	transitionDeadline time.Duration
	queueSize          int
	now                func() time.Time
	sleep              func(context.Context, time.Duration) error
	jitter             func(time.Duration) time.Duration
}

type transitionProgress struct {
	transition      Transition
	remaining       int
	executableTotal int
	canceledCount   int
	started         bool
}

type queuedTask struct {
	operation    redfish.ResetOperation
	transitionID string
	task         Task
}

// Runner executes transition tasks asynchronously with configured limits.
type Runner struct {
	store    Store
	executor Executor
	verifier *Verifier
	cfg      runtimeConfig
	queue    *Queue

	startOnce sync.Once

	runMu      sync.RWMutex
	runningCtx context.Context

	progressMu sync.Mutex
	progress   map[string]*transitionProgress

	bmcMu       sync.Mutex
	bmcLimiters map[string]chan struct{}
}

// New creates a new transition runner.
func New(st Store, executor Executor, reader PowerStateReader, cfg Config) *Runner {
	normalized := normalizeConfig(cfg)

	return &Runner{
		store:       st,
		executor:    executor,
		verifier:    NewVerifier(reader, VerifyConfig{Window: cfg.VerificationWindow, PollInterval: cfg.VerificationPoll}),
		cfg:         normalized,
		queue:       newQueue(normalized.queueSize),
		progress:    make(map[string]*transitionProgress),
		bmcLimiters: make(map[string]chan struct{}),
	}
}

// Start launches worker goroutines. It is safe to call multiple times.
func (r *Runner) Start(ctx context.Context) {
	r.startOnce.Do(func() {
		r.setRunningContext(ctx)
		for i := 0; i < r.cfg.globalConcurrency; i++ {
			go r.worker(ctx)
		}
		go func() {
			<-ctx.Done()
			r.queue.close()
		}()
	})
}

// StartTransition persists and enqueues a transition request.
func (r *Runner) StartTransition(ctx context.Context, req StartRequest) (Transition, error) {
	if !r.isRunning() {
		return Transition{}, ErrRunnerNotStarted
	}

	operation, err := redfish.ParseResetOperation(req.Operation)
	if err != nil {
		return Transition{}, fmt.Errorf("parsing operation: %w", err)
	}

	nodeIDs := normalizeNodeIDs(req.NodeIDs)
	if len(nodeIDs) == 0 {
		return Transition{}, ErrNoTargetNodes
	}

	mappings, missing, err := r.store.ResolveNodeMappings(ctx, nodeIDs)
	if err != nil {
		return Transition{}, fmt.Errorf("resolving node mappings: %w", err)
	}

	mappingByNode := make(map[string]model.NodePowerMapping, len(mappings))
	for _, mapping := range mappings {
		mappingByNode[strings.TrimSpace(mapping.NodeID)] = mapping
	}

	missingByNode := make(map[string]model.NodeMappingError, len(missing))
	for _, mappingErr := range missing {
		missingByNode[strings.TrimSpace(mappingErr.NodeID)] = mappingErr
	}

	now := r.cfg.now().UTC()
	transition := Transition{
		RequestID:   strings.TrimSpace(req.RequestID),
		Operation:   string(operation),
		State:       TransitionStatePending,
		RequestedBy: strings.TrimSpace(req.RequestedBy),
		DryRun:      req.DryRun,
		TargetCount: len(nodeIDs),
		QueuedAt:    now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	tasks := make([]Task, 0, len(nodeIDs))
	pendingCount := 0
	for _, nodeID := range nodeIDs {
		task := Task{
			TransitionID: "",
			NodeID:       nodeID,
			Operation:    string(operation),
			DryRun:       req.DryRun,
			QueuedAt:     now,
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		if missingErr, missing := missingByNode[nodeID]; missing {
			completedAt := now
			task.State = TaskStateFailed
			task.ErrorDetail = strings.TrimSpace(missingErr.Detail)
			task.CompletedAt = &completedAt
			transition.FailureCount++
			tasks = append(tasks, task)
			continue
		}

		mapping, ok := mappingByNode[nodeID]
		if !ok {
			completedAt := now
			task.State = TaskStateFailed
			task.ErrorDetail = model.MissingNodeMappingError(nodeID).Detail
			task.CompletedAt = &completedAt
			transition.FailureCount++
			tasks = append(tasks, task)
			continue
		}

		task.BMCID = strings.TrimSpace(mapping.BMCID)
		task.BMCEndpoint = strings.TrimSpace(mapping.Endpoint)
		task.CredentialID = strings.TrimSpace(mapping.CredentialID)
		task.InsecureSkipVerify = mapping.InsecureSkipVerify

		if req.DryRun {
			completedAt := now
			task.State = TaskStatePlanned
			task.CompletedAt = &completedAt
		} else {
			task.State = TaskStatePending
			pendingCount++
		}
		tasks = append(tasks, task)
	}

	if req.DryRun {
		completedAt := now
		transition.State = TransitionStatePlanned
		transition.CompletedAt = &completedAt
	} else if pendingCount == 0 {
		completedAt := now
		transition.State = TransitionStateFailed
		transition.CompletedAt = &completedAt
	}

	createdTransition, createdTasks, err := r.store.CreateTransition(ctx, transition, tasks)
	if err != nil {
		return Transition{}, fmt.Errorf("creating transition record: %w", err)
	}

	if req.DryRun || pendingCount == 0 {
		createdTransition.UpdatedAt = r.cfg.now().UTC()
		updatedTransition, updateErr := r.store.UpdateTransition(ctx, createdTransition)
		if updateErr != nil {
			return Transition{}, fmt.Errorf("updating terminal transition record: %w", updateErr)
		}
		return updatedTransition, nil
	}

	pendingTasks := make([]Task, 0, pendingCount)
	for _, task := range createdTasks {
		if task.State == TaskStatePending {
			pendingTasks = append(pendingTasks, task)
		}
	}

	r.progressMu.Lock()
	r.progress[createdTransition.ID] = &transitionProgress{
		transition:      createdTransition,
		executableTotal: len(pendingTasks),
		remaining:       len(pendingTasks),
	}
	r.progressMu.Unlock()

	for _, task := range pendingTasks {
		if enqueueErr := r.queue.enqueue(ctx, queuedTask{
			operation:    operation,
			transitionID: createdTransition.ID,
			task:         task,
		}); enqueueErr != nil {
			return Transition{}, fmt.Errorf("enqueueing transition task: %w", enqueueErr)
		}
	}

	return createdTransition, nil
}

func (r *Runner) worker(ctx context.Context) {
	for {
		item, err := r.queue.dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, errQueueClosed) {
				return
			}
			continue
		}
		r.executeTask(ctx, item)
	}
}

func (r *Runner) executeTask(ctx context.Context, item queuedTask) {
	if err := r.markTransitionRunning(ctx, item.transitionID); err != nil {
		return
	}

	task := item.task
	startedAt := r.cfg.now().UTC()
	task.State = TaskStateRunning
	task.StartedAt = &startedAt
	task.UpdatedAt = startedAt

	updatedTask, err := r.store.UpdateTransitionTask(ctx, task)
	if err == nil {
		task = updatedTask
	}

	releaseBMC, err := r.acquireBMCLimiter(ctx, task.BMCID)
	if err != nil {
		r.completeTask(ctx, item.transitionID, task, 0, "", err)
		return
	}
	defer releaseBMC()

	execCtx := ctx
	cancelExec := func() {}
	if r.cfg.transitionDeadline > 0 {
		execCtx, cancelExec = context.WithTimeout(ctx, r.cfg.transitionDeadline)
	}
	defer cancelExec()

	executionRequest := ExecutionRequest{
		TransitionID:       item.transitionID,
		TaskID:             task.ID,
		NodeID:             task.NodeID,
		BMCID:              task.BMCID,
		Endpoint:           task.BMCEndpoint,
		CredentialID:       task.CredentialID,
		InsecureSkipVerify: task.InsecureSkipVerify,
		Operation:          item.operation,
	}

	attempts, execErr := r.executeWithRetry(execCtx, executionRequest)
	if execErr != nil {
		r.completeTask(ctx, item.transitionID, task, attempts, "", execErr)
		return
	}

	finalPowerState, verifyErr := r.verifier.Verify(execCtx, executionRequest)
	if verifyErr != nil {
		r.completeTask(ctx, item.transitionID, task, attempts, finalPowerState, verifyErr)
		return
	}

	r.completeTask(ctx, item.transitionID, task, attempts, finalPowerState, nil)
}

func (r *Runner) executeWithRetry(ctx context.Context, req ExecutionRequest) (int, error) {
	attempts := 0
	for attempt := 1; attempt <= r.cfg.retryAttempts; attempt++ {
		attempts = attempt
		err := r.executor.ExecutePowerAction(ctx, req)
		if err == nil {
			return attempts, nil
		}

		if attempt == r.cfg.retryAttempts || !IsRetryable(err) {
			return attempts, err
		}

		wait := r.retryDelay(attempt)
		if sleepErr := r.cfg.sleep(ctx, wait); sleepErr != nil {
			return attempts, sleepErr
		}
	}

	return attempts, fmt.Errorf("exhausted retries without execution")
}

func (r *Runner) retryDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return r.cfg.retryBackoffBase
	}

	scaled := float64(r.cfg.retryBackoffBase) * math.Pow(2, float64(attempt-1))
	delay := time.Duration(scaled)
	if delay > r.cfg.retryBackoffMax {
		delay = r.cfg.retryBackoffMax
	}
	if delay < 0 {
		delay = r.cfg.retryBackoffMax
	}

	jitter := r.cfg.jitter(delay / 2)
	if jitter < 0 {
		jitter = 0
	}
	return delay + jitter
}

func (r *Runner) completeTask(
	ctx context.Context,
	transitionID string,
	task Task,
	attempts int,
	finalPowerState string,
	resultErr error,
) {
	now := r.cfg.now().UTC()
	task.AttemptCount = attempts
	task.FinalPowerState = strings.TrimSpace(finalPowerState)
	task.CompletedAt = &now
	task.UpdatedAt = now

	outcomeState := TaskStateSucceeded
	if resultErr != nil {
		task.ErrorDetail = strings.TrimSpace(resultErr.Error())
		switch {
		case errors.Is(resultErr, context.Canceled), errors.Is(resultErr, context.DeadlineExceeded):
			outcomeState = TaskStateCanceled
		default:
			outcomeState = TaskStateFailed
		}
	}
	task.State = outcomeState

	updatedTask, err := r.store.UpdateTransitionTask(ctx, task)
	if err == nil {
		task = updatedTask
	}

	r.recordTaskOutcome(ctx, transitionID, task.State)
}

func (r *Runner) markTransitionRunning(ctx context.Context, transitionID string) error {
	var transitionToPersist Transition
	persist := false

	r.progressMu.Lock()
	progress, ok := r.progress[transitionID]
	if !ok {
		r.progressMu.Unlock()
		return fmt.Errorf("transition %q has no active progress", transitionID)
	}
	if !progress.started {
		startedAt := r.cfg.now().UTC()
		progress.started = true
		progress.transition.State = TransitionStateRunning
		progress.transition.StartedAt = &startedAt
		progress.transition.UpdatedAt = startedAt
		transitionToPersist = progress.transition
		persist = true
	}
	r.progressMu.Unlock()

	if !persist {
		return nil
	}

	_, err := r.store.UpdateTransition(ctx, transitionToPersist)
	if err != nil {
		return fmt.Errorf("marking transition running: %w", err)
	}
	return nil
}

func (r *Runner) recordTaskOutcome(ctx context.Context, transitionID, taskState string) {
	var transitionToPersist Transition
	persist := false

	r.progressMu.Lock()
	progress, ok := r.progress[transitionID]
	if !ok {
		r.progressMu.Unlock()
		return
	}

	switch taskState {
	case TaskStateSucceeded:
		progress.transition.SuccessCount++
	case TaskStateCanceled:
		progress.transition.FailureCount++
		progress.canceledCount++
	default:
		progress.transition.FailureCount++
	}

	progress.remaining--
	if progress.remaining <= 0 {
		completedAt := r.cfg.now().UTC()
		progress.transition.CompletedAt = &completedAt
		progress.transition.UpdatedAt = completedAt
		progress.transition.State = finalTransitionState(progress)
		transitionToPersist = progress.transition
		delete(r.progress, transitionID)
		persist = true
	}
	r.progressMu.Unlock()

	if !persist {
		return
	}

	_, _ = r.store.UpdateTransition(ctx, transitionToPersist)
}

func finalTransitionState(progress *transitionProgress) string {
	if progress.executableTotal > 0 &&
		progress.canceledCount == progress.executableTotal &&
		progress.transition.SuccessCount == 0 &&
		progress.transition.FailureCount == progress.canceledCount {
		return TransitionStateCanceled
	}

	switch {
	case progress.transition.FailureCount == 0:
		return TransitionStateCompleted
	case progress.transition.SuccessCount == 0:
		return TransitionStateFailed
	default:
		return TransitionStatePartial
	}
}

func (r *Runner) acquireBMCLimiter(ctx context.Context, bmcID string) (func(), error) {
	bmc := strings.TrimSpace(bmcID)
	if bmc == "" {
		return func() {}, nil
	}

	limiter := r.bmcLimiter(bmc)
	select {
	case limiter <- struct{}{}:
		return func() {
			<-limiter
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *Runner) bmcLimiter(bmcID string) chan struct{} {
	r.bmcMu.Lock()
	defer r.bmcMu.Unlock()

	if limiter, exists := r.bmcLimiters[bmcID]; exists {
		return limiter
	}

	limiter := make(chan struct{}, r.cfg.perBMCConcurrency)
	r.bmcLimiters[bmcID] = limiter
	return limiter
}

func (r *Runner) setRunningContext(ctx context.Context) {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	r.runningCtx = ctx
}

func (r *Runner) isRunning() bool {
	r.runMu.RLock()
	defer r.runMu.RUnlock()
	if r.runningCtx == nil {
		return false
	}
	return r.runningCtx.Err() == nil
}

func normalizeNodeIDs(nodeIDs []string) []string {
	unique := make(map[string]struct{}, len(nodeIDs))
	ordered := make([]string, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		normalized := strings.TrimSpace(nodeID)
		if normalized == "" {
			continue
		}
		if _, exists := unique[normalized]; exists {
			continue
		}
		unique[normalized] = struct{}{}
		ordered = append(ordered, normalized)
	}
	sort.Strings(ordered)
	return ordered
}

func normalizeConfig(cfg Config) runtimeConfig {
	globalConcurrency := cfg.GlobalConcurrency
	if globalConcurrency <= 0 {
		globalConcurrency = defaultGlobalConcurrency
	}

	perBMCConcurrency := cfg.PerBMCConcurrency
	if perBMCConcurrency <= 0 {
		perBMCConcurrency = defaultPerBMCConcurrency
	}

	retryAttempts := cfg.RetryAttempts
	if retryAttempts <= 0 {
		retryAttempts = defaultRetryAttempts
	}

	retryBackoffBase := cfg.RetryBackoffBase
	if retryBackoffBase <= 0 {
		retryBackoffBase = defaultRetryBackoffBase
	}

	retryBackoffMax := cfg.RetryBackoffMax
	if retryBackoffMax <= 0 {
		retryBackoffMax = defaultRetryBackoffMax
	}
	if retryBackoffMax < retryBackoffBase {
		retryBackoffMax = retryBackoffBase
	}

	transitionDeadline := cfg.TransitionDeadline
	if transitionDeadline <= 0 {
		transitionDeadline = defaultTransitionTimeout
	}

	queueSize := cfg.QueueSize
	if queueSize <= 0 {
		queueSize = globalConcurrency * 4
	}
	if queueSize < 1 {
		queueSize = 1
	}

	return runtimeConfig{
		globalConcurrency:  globalConcurrency,
		perBMCConcurrency:  perBMCConcurrency,
		retryAttempts:      retryAttempts,
		retryBackoffBase:   retryBackoffBase,
		retryBackoffMax:    retryBackoffMax,
		transitionDeadline: transitionDeadline,
		queueSize:          queueSize,
		now:                time.Now,
		sleep:              sleepWithContext,
		jitter:             cryptoJitter,
	}
}

func sleepWithContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func cryptoJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(max.Nanoseconds()+1))
	if err != nil {
		return 0
	}
	return time.Duration(value.Int64())
}
