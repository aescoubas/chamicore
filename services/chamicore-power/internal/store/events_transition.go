package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"git.cscs.ch/openchami/chamicore-lib/events"
	"git.cscs.ch/openchami/chamicore-power/internal/engine"
)

const (
	transitionLifecycleEventType  = "chamicore.power.transitions.lifecycle"
	transitionTaskResultEventType = "chamicore.power.transitions.task-result"
	transitionEventSource         = "chamicore-power"
)

var (
	readTransitionEventRandom = rand.Read
	marshalTransitionEvent    = json.Marshal
)

type transitionLifecycleEventData struct {
	TransitionID string                  `json:"transitionId"`
	Snapshot     transitionEventSnapshot `json:"snapshot"`
}

type transitionTaskResultEventData struct {
	TransitionID string                      `json:"transitionId"`
	NodeID       string                      `json:"nodeId"`
	TaskID       string                      `json:"taskId"`
	Snapshot     transitionTaskEventSnapshot `json:"snapshot"`
}

type transitionEventSnapshot struct {
	ID           string     `json:"id"`
	RequestID    string     `json:"requestId,omitempty"`
	Operation    string     `json:"operation"`
	State        string     `json:"state"`
	RequestedBy  string     `json:"requestedBy,omitempty"`
	DryRun       bool       `json:"dryRun"`
	TargetCount  int        `json:"targetCount"`
	SuccessCount int        `json:"successCount"`
	FailureCount int        `json:"failureCount"`
	QueuedAt     time.Time  `json:"queuedAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type transitionTaskEventSnapshot struct {
	ID                 string     `json:"id"`
	TransitionID       string     `json:"transitionId"`
	NodeID             string     `json:"nodeId"`
	BMCID              string     `json:"bmcId,omitempty"`
	BMCEndpoint        string     `json:"bmcEndpoint,omitempty"`
	CredentialID       string     `json:"credentialId,omitempty"`
	Operation          string     `json:"operation"`
	State              string     `json:"state"`
	DryRun             bool       `json:"dryRun"`
	AttemptCount       int        `json:"attemptCount"`
	FinalPowerState    string     `json:"finalPowerState,omitempty"`
	ErrorDetail        string     `json:"errorDetail,omitempty"`
	QueuedAt           time.Time  `json:"queuedAt"`
	StartedAt          *time.Time `json:"startedAt,omitempty"`
	CompletedAt        *time.Time `json:"completedAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	InsecureSkipVerify bool       `json:"insecureSkipVerify,omitempty"`
}

func newTransitionLifecycleEvent(transition engine.Transition) (events.Event, error) {
	transitionID := strings.TrimSpace(transition.ID)
	if transitionID == "" {
		return events.Event{}, fmt.Errorf("transition id is required")
	}

	eventID, err := newTransitionEventID()
	if err != nil {
		return events.Event{}, err
	}

	payload := transitionLifecycleEventData{
		TransitionID: transitionID,
		Snapshot: transitionEventSnapshot{
			ID:           transitionID,
			RequestID:    strings.TrimSpace(transition.RequestID),
			Operation:    strings.TrimSpace(transition.Operation),
			State:        strings.TrimSpace(transition.State),
			RequestedBy:  strings.TrimSpace(transition.RequestedBy),
			DryRun:       transition.DryRun,
			TargetCount:  transition.TargetCount,
			SuccessCount: transition.SuccessCount,
			FailureCount: transition.FailureCount,
			QueuedAt:     transition.QueuedAt.UTC(),
			StartedAt:    utcTimePtr(transition.StartedAt),
			CompletedAt:  utcTimePtr(transition.CompletedAt),
			CreatedAt:    transition.CreatedAt.UTC(),
			UpdatedAt:    transition.UpdatedAt.UTC(),
		},
	}

	data, err := marshalTransitionEvent(payload)
	if err != nil {
		return events.Event{}, fmt.Errorf("marshaling transition lifecycle payload: %w", err)
	}

	return events.Event{
		ID:              eventID,
		Source:          transitionEventSource,
		Type:            transitionLifecycleEventType,
		Subject:         transitionID,
		DataContentType: events.JSONDataContentType,
		Data:            data,
	}, nil
}

func newTransitionTaskResultEvent(task engine.Task) (events.Event, error) {
	taskID := strings.TrimSpace(task.ID)
	if taskID == "" {
		return events.Event{}, fmt.Errorf("task id is required")
	}

	transitionID := strings.TrimSpace(task.TransitionID)
	if transitionID == "" {
		return events.Event{}, fmt.Errorf("transition id is required")
	}

	nodeID := strings.TrimSpace(task.NodeID)
	if nodeID == "" {
		return events.Event{}, fmt.Errorf("node id is required")
	}

	eventID, err := newTransitionEventID()
	if err != nil {
		return events.Event{}, err
	}

	payload := transitionTaskResultEventData{
		TransitionID: transitionID,
		NodeID:       nodeID,
		TaskID:       taskID,
		Snapshot: transitionTaskEventSnapshot{
			ID:                 taskID,
			TransitionID:       transitionID,
			NodeID:             nodeID,
			BMCID:              strings.TrimSpace(task.BMCID),
			BMCEndpoint:        strings.TrimSpace(task.BMCEndpoint),
			CredentialID:       strings.TrimSpace(task.CredentialID),
			Operation:          strings.TrimSpace(task.Operation),
			State:              strings.TrimSpace(task.State),
			DryRun:             task.DryRun,
			AttemptCount:       task.AttemptCount,
			FinalPowerState:    strings.TrimSpace(task.FinalPowerState),
			ErrorDetail:        strings.TrimSpace(task.ErrorDetail),
			QueuedAt:           task.QueuedAt.UTC(),
			StartedAt:          utcTimePtr(task.StartedAt),
			CompletedAt:        utcTimePtr(task.CompletedAt),
			CreatedAt:          task.CreatedAt.UTC(),
			UpdatedAt:          task.UpdatedAt.UTC(),
			InsecureSkipVerify: task.InsecureSkipVerify,
		},
	}

	data, err := marshalTransitionEvent(payload)
	if err != nil {
		return events.Event{}, fmt.Errorf("marshaling transition task payload: %w", err)
	}

	return events.Event{
		ID:              eventID,
		Source:          transitionEventSource,
		Type:            transitionTaskResultEventType,
		Subject:         nodeID,
		DataContentType: events.JSONDataContentType,
		Data:            data,
	}, nil
}

func newTransitionEventID() (string, error) {
	var id [16]byte
	if _, err := readTransitionEventRandom(id[:]); err != nil {
		return "", fmt.Errorf("generating event id: %w", err)
	}
	return "evt-" + hex.EncodeToString(id[:]), nil
}

func utcTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	t := value.UTC()
	return &t
}
