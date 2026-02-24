// Package types defines public request/response payloads for the power API.
package types

import "time"

const (
	// TransitionStatePending indicates queued transition work.
	TransitionStatePending = "pending"
	// TransitionStateRunning indicates transition tasks are in progress.
	TransitionStateRunning = "running"
	// TransitionStateCompleted indicates all transition tasks succeeded.
	TransitionStateCompleted = "completed"
	// TransitionStateFailed indicates all transition tasks failed.
	TransitionStateFailed = "failed"
	// TransitionStatePartial indicates a transition completed with mixed results.
	TransitionStatePartial = "partial"
	// TransitionStateCanceled indicates a transition was canceled.
	TransitionStateCanceled = "canceled"
	// TransitionStatePlanned indicates a dry-run transition plan.
	TransitionStatePlanned = "planned"
)

const (
	// TaskStatePending indicates queued node task work.
	TaskStatePending = "pending"
	// TaskStateRunning indicates a node task is in progress.
	TaskStateRunning = "running"
	// TaskStateSucceeded indicates a node task completed successfully.
	TaskStateSucceeded = "succeeded"
	// TaskStateFailed indicates a node task failed.
	TaskStateFailed = "failed"
	// TaskStateCanceled indicates a node task was canceled.
	TaskStateCanceled = "canceled"
	// TaskStatePlanned indicates a dry-run node task.
	TaskStatePlanned = "planned"
)

// CreateTransitionRequest is the body for POST /power/v1/transitions.
type CreateTransitionRequest struct {
	Operation string   `json:"operation"`
	RequestID string   `json:"requestID,omitempty"`
	Nodes     []string `json:"nodes,omitempty"`
	Groups    []string `json:"groups,omitempty"`
	DryRun    bool     `json:"dryRun,omitempty"`
}

// ActionRequest is the body for POST action convenience endpoints.
type ActionRequest struct {
	RequestID string   `json:"requestID,omitempty"`
	Nodes     []string `json:"nodes,omitempty"`
	Groups    []string `json:"groups,omitempty"`
	DryRun    bool     `json:"dryRun,omitempty"`
}

// ResetActionRequest is the body for POST /power/v1/actions/reset.
type ResetActionRequest struct {
	Operation string   `json:"operation"`
	RequestID string   `json:"requestID,omitempty"`
	Nodes     []string `json:"nodes,omitempty"`
	Groups    []string `json:"groups,omitempty"`
	DryRun    bool     `json:"dryRun,omitempty"`
}

// Transition is the public transition resource payload.
type Transition struct {
	RequestID    string           `json:"requestID,omitempty"`
	Operation    string           `json:"operation"`
	State        string           `json:"state"`
	RequestedBy  string           `json:"requestedBy,omitempty"`
	QueuedAt     time.Time        `json:"queuedAt"`
	StartedAt    *time.Time       `json:"startedAt,omitempty"`
	CompletedAt  *time.Time       `json:"completedAt,omitempty"`
	Tasks        []TransitionTask `json:"tasks,omitempty"`
	TargetCount  int              `json:"targetCount"`
	SuccessCount int              `json:"successCount"`
	FailureCount int              `json:"failureCount"`
	DryRun       bool             `json:"dryRun"`
}

// TransitionTask is the public per-node task payload.
type TransitionTask struct {
	NodeID          string     `json:"nodeID"`
	BMCID           string     `json:"bmcID,omitempty"`
	Endpoint        string     `json:"endpoint,omitempty"`
	Operation       string     `json:"operation"`
	State           string     `json:"state"`
	FinalPowerState string     `json:"finalPowerState,omitempty"`
	ErrorDetail     string     `json:"errorDetail,omitempty"`
	QueuedAt        time.Time  `json:"queuedAt"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
	AttemptCount    int        `json:"attemptCount"`
	DryRun          bool       `json:"dryRun"`
}

// PowerStatus is the payload returned by GET /power/v1/power-status.
type PowerStatus struct {
	NodeStatuses []PowerNodeStatus `json:"nodeStatuses"`
	Total        int               `json:"total"`
}

// PowerNodeStatus contains current/latest power status for one node.
type PowerNodeStatus struct {
	NodeID          string     `json:"nodeID"`
	BMCID           string     `json:"bmcID,omitempty"`
	TransitionID    string     `json:"transitionID,omitempty"`
	Operation       string     `json:"operation,omitempty"`
	State           string     `json:"state"`
	PowerState      string     `json:"powerState,omitempty"`
	ErrorDetail     string     `json:"errorDetail,omitempty"`
	LastUpdatedAt   *time.Time `json:"lastUpdatedAt,omitempty"`
	LastCompletedAt *time.Time `json:"lastCompletedAt,omitempty"`
}

// MappingSyncTrigger is the payload for POST /power/v1/admin/mappings/sync.
type MappingSyncTrigger struct {
	Status string `json:"status"`
}
