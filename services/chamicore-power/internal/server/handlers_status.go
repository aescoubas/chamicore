package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-lib/redfish"
	"git.cscs.ch/openchami/chamicore-power/internal/engine"
)

type powerStatusResponse struct {
	NodeStatuses []powerNodeStatus `json:"nodeStatuses"`
	Total        int               `json:"total"`
}

type powerNodeStatus struct {
	NodeID          string       `json:"nodeID"`
	BMCID           string       `json:"bmcID,omitempty"`
	TransitionID    string       `json:"transitionID,omitempty"`
	Operation       string       `json:"operation,omitempty"`
	State           string       `json:"state"`
	PowerState      string       `json:"powerState,omitempty"`
	ErrorDetail     string       `json:"errorDetail,omitempty"`
	LastUpdatedAt   *timeRFC3339 `json:"lastUpdatedAt,omitempty"`
	LastCompletedAt *timeRFC3339 `json:"lastCompletedAt,omitempty"`
}

// timeRFC3339 serializes timestamps as RFC3339 UTC strings.
type timeRFC3339 string

func (s *Server) handleGetPowerStatus(w http.ResponseWriter, r *http.Request) {
	if s.transitionStore == nil {
		httputil.RespondProblem(w, r, http.StatusServiceUnavailable, errTransitionSubsystemUnavailable.Error())
		return
	}

	nodes := parseQueryTargets(r, "nodes", "node")
	groups := parseQueryTargets(r, "groups", "group")
	targetNodes, err := s.resolveTargets(r.Context(), nodes, groups)
	if err != nil {
		s.respondTargetResolutionError(w, r, err)
		return
	}
	if len(targetNodes) > s.cfg.BulkMaxNodes {
		httputil.RespondProblemf(
			w,
			r,
			http.StatusBadRequest,
			"too many target nodes: got %d, max %d",
			len(targetNodes),
			s.cfg.BulkMaxNodes,
		)
		return
	}

	latestTasks, err := s.transitionStore.ListLatestTransitionTasksByNode(r.Context(), targetNodes)
	if err != nil {
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "failed to resolve power status")
		return
	}

	_, missingMappings, err := s.store.ResolveNodeMappings(r.Context(), targetNodes)
	if err != nil {
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "failed to resolve topology mapping")
		return
	}

	missingByNode := make(map[string]string, len(missingMappings))
	for _, item := range missingMappings {
		missingByNode[strings.TrimSpace(item.NodeID)] = strings.TrimSpace(item.Detail)
	}

	taskByNode := make(map[string]engine.Task, len(latestTasks))
	for _, task := range latestTasks {
		nodeID := strings.TrimSpace(task.NodeID)
		if nodeID == "" {
			continue
		}
		taskByNode[nodeID] = task
	}

	statuses := make([]powerNodeStatus, 0, len(targetNodes))
	for _, nodeID := range targetNodes {
		status := powerNodeStatus{NodeID: nodeID}

		if detail, missing := missingByNode[nodeID]; missing {
			status.State = "unresolved"
			status.ErrorDetail = detail
			statuses = append(statuses, status)
			continue
		}

		task, ok := taskByNode[nodeID]
		if !ok {
			status.State = "unknown"
			status.ErrorDetail = "no transition history for node"
			statuses = append(statuses, status)
			continue
		}

		status.BMCID = strings.TrimSpace(task.BMCID)
		status.TransitionID = strings.TrimSpace(task.TransitionID)
		status.Operation = strings.TrimSpace(task.Operation)
		status.State = strings.TrimSpace(task.State)
		status.PowerState = strings.TrimSpace(task.FinalPowerState)
		if status.PowerState == "" {
			status.PowerState = inferredPowerState(task.Operation)
		}
		if completedAt := toTimeRFC3339Ptr(task.CompletedAt); completedAt != nil {
			status.LastCompletedAt = completedAt
		}
		status.LastUpdatedAt = toTimeRFC3339Ptr(&task.UpdatedAt)
		if status.LastUpdatedAt == nil {
			now := newTimeRFC3339(time.Now().UTC())
			status.LastUpdatedAt = &now
		}
		if strings.TrimSpace(task.ErrorDetail) != "" {
			status.ErrorDetail = strings.TrimSpace(task.ErrorDetail)
		}

		statuses = append(statuses, status)
	}

	httputil.RespondJSON(w, http.StatusOK, httputil.Resource[powerStatusResponse]{
		Kind:       "PowerStatus",
		APIVersion: "power/v1",
		Metadata: httputil.Metadata{
			ID: "power-status",
		},
		Spec: powerStatusResponse{
			NodeStatuses: statuses,
			Total:        len(statuses),
		},
	})
}

func parseQueryTargets(r *http.Request, keys ...string) []string {
	values := make([]string, 0)
	for _, key := range keys {
		values = append(values, r.URL.Query()[key]...)
	}
	return parseTargetList(values)
}

func inferredPowerState(operation string) string {
	parsed, err := redfish.ParseResetOperation(operation)
	if err != nil {
		return ""
	}

	switch parsed {
	case redfish.ResetOperationOn,
		redfish.ResetOperationGracefulRestart,
		redfish.ResetOperationForceRestart,
		redfish.ResetOperationNMI:
		return "On"
	case redfish.ResetOperationForceOff,
		redfish.ResetOperationGracefulShutdown:
		return "Off"
	default:
		return ""
	}
}

func newTimeRFC3339(v time.Time) timeRFC3339 {
	return timeRFC3339(v.UTC().Format(time.RFC3339Nano))
}

func toTimeRFC3339Ptr(v *time.Time) *timeRFC3339 {
	if v == nil {
		return nil
	}
	t := newTimeRFC3339(*v)
	return &t
}

func (t timeRFC3339) String() string {
	return string(t)
}

func (t timeRFC3339) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", string(t))), nil
}
