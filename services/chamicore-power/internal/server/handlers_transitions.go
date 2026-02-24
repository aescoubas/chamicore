package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"git.cscs.ch/openchami/chamicore-lib/auth"
	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-lib/redfish"
	"git.cscs.ch/openchami/chamicore-power/internal/engine"
	"git.cscs.ch/openchami/chamicore-power/internal/store"
)

const (
	defaultTransitionListLimit = 100
	maxTransitionListLimit     = 1000
)

var (
	// ErrGroupNotFound indicates an SMD group does not exist.
	ErrGroupNotFound = errors.New("group not found")

	errTransitionSubsystemUnavailable = errors.New("transition subsystem is not configured")
	errGroupResolverUnavailable       = errors.New("group resolver is not configured")
)

type transitionCreateRequest struct {
	RequestID string   `json:"requestID,omitempty"`
	Operation string   `json:"operation"`
	Nodes     []string `json:"nodes,omitempty"`
	Groups    []string `json:"groups,omitempty"`
	DryRun    bool     `json:"dryRun,omitempty"`
}

type transitionRequest struct {
	RequestID string
	Operation string
	Nodes     []string
	Groups    []string
	DryRun    bool
}

type transitionSpec struct {
	RequestID    string               `json:"requestID,omitempty"`
	Operation    string               `json:"operation"`
	State        string               `json:"state"`
	RequestedBy  string               `json:"requestedBy,omitempty"`
	DryRun       bool                 `json:"dryRun"`
	TargetCount  int                  `json:"targetCount"`
	SuccessCount int                  `json:"successCount"`
	FailureCount int                  `json:"failureCount"`
	QueuedAt     timeRFC3339          `json:"queuedAt"`
	StartedAt    *timeRFC3339         `json:"startedAt,omitempty"`
	CompletedAt  *timeRFC3339         `json:"completedAt,omitempty"`
	Tasks        []transitionTaskSpec `json:"tasks,omitempty"`
}

type transitionTaskSpec struct {
	NodeID          string       `json:"nodeID"`
	BMCID           string       `json:"bmcID,omitempty"`
	Endpoint        string       `json:"endpoint,omitempty"`
	Operation       string       `json:"operation"`
	State           string       `json:"state"`
	DryRun          bool         `json:"dryRun"`
	AttemptCount    int          `json:"attemptCount"`
	FinalPowerState string       `json:"finalPowerState,omitempty"`
	ErrorDetail     string       `json:"errorDetail,omitempty"`
	QueuedAt        timeRFC3339  `json:"queuedAt"`
	StartedAt       *timeRFC3339 `json:"startedAt,omitempty"`
	CompletedAt     *timeRFC3339 `json:"completedAt,omitempty"`
}

func (s *Server) handleListTransitions(w http.ResponseWriter, r *http.Request) {
	if s.transitionStore == nil {
		httputil.RespondProblem(w, r, http.StatusServiceUnavailable, errTransitionSubsystemUnavailable.Error())
		return
	}

	limit, offset, err := parseListPagination(r)
	if err != nil {
		httputil.RespondProblem(w, r, http.StatusBadRequest, err.Error())
		return
	}

	items, total, err := s.transitionStore.ListTransitions(r.Context(), limit, offset)
	if err != nil {
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "failed to list transitions")
		return
	}

	resources := make([]httputil.Resource[transitionSpec], 0, len(items))
	for _, item := range items {
		resources = append(resources, toTransitionResource(item, nil))
	}

	httputil.RespondJSON(w, http.StatusOK, httputil.ResourceList[transitionSpec]{
		Kind:       "TransitionList",
		APIVersion: "power/v1",
		Metadata: httputil.ListMetadata{
			Total:  total,
			Limit:  limit,
			Offset: offset,
		},
		Items: resources,
	})
}

func (s *Server) handleCreateTransition(w http.ResponseWriter, r *http.Request) {
	var req transitionCreateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondProblemf(w, r, http.StatusBadRequest, "invalid request body: %v", err)
		return
	}

	s.startTransition(w, r, transitionRequest{
		RequestID: strings.TrimSpace(req.RequestID),
		Operation: strings.TrimSpace(req.Operation),
		Nodes:     req.Nodes,
		Groups:    req.Groups,
		DryRun:    req.DryRun,
	})
}

func (s *Server) handleGetTransition(w http.ResponseWriter, r *http.Request) {
	if s.transitionStore == nil {
		httputil.RespondProblem(w, r, http.StatusServiceUnavailable, errTransitionSubsystemUnavailable.Error())
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		httputil.RespondProblem(w, r, http.StatusBadRequest, "transition id is required")
		return
	}

	transition, tasks, err := s.loadTransition(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httputil.RespondProblemf(w, r, http.StatusNotFound, "transition %q not found", id)
			return
		}
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "failed to load transition")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, toTransitionResource(transition, tasks))
}

func (s *Server) handleDeleteTransition(w http.ResponseWriter, r *http.Request) {
	if s.transitionRunner == nil || s.transitionStore == nil {
		httputil.RespondProblem(w, r, http.StatusServiceUnavailable, errTransitionSubsystemUnavailable.Error())
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		httputil.RespondProblem(w, r, http.StatusBadRequest, "transition id is required")
		return
	}

	abortErr := s.transitionRunner.AbortTransition(r.Context(), id)
	if abortErr != nil && !errors.Is(abortErr, engine.ErrTransitionNotFound) {
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "failed to abort transition")
		return
	}

	transition, tasks, err := s.loadTransition(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httputil.RespondProblemf(w, r, http.StatusNotFound, "transition %q not found", id)
			return
		}
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "failed to load transition")
		return
	}

	httputil.RespondJSON(w, http.StatusAccepted, toTransitionResource(transition, tasks))
}

func (s *Server) startTransition(w http.ResponseWriter, r *http.Request, req transitionRequest) {
	if s.transitionRunner == nil {
		httputil.RespondProblem(w, r, http.StatusServiceUnavailable, errTransitionSubsystemUnavailable.Error())
		return
	}

	operation, err := redfish.ParseResetOperation(req.Operation)
	if err != nil {
		httputil.RespondProblemf(w, r, http.StatusBadRequest, "invalid operation %q", req.Operation)
		return
	}

	nodeIDs, err := s.resolveTargets(r.Context(), req.Nodes, req.Groups)
	if err != nil {
		s.respondTargetResolutionError(w, r, err)
		return
	}
	if len(nodeIDs) > s.cfg.BulkMaxNodes {
		httputil.RespondProblemf(
			w,
			r,
			http.StatusBadRequest,
			"too many target nodes: got %d, max %d",
			len(nodeIDs),
			s.cfg.BulkMaxNodes,
		)
		return
	}

	startReq := engine.StartRequest{
		RequestID:   resolvedRequestID(r, req.RequestID),
		RequestedBy: requestedByFromContext(r),
		Operation:   string(operation),
		NodeIDs:     nodeIDs,
		DryRun:      req.DryRun,
	}

	transition, err := s.transitionRunner.StartTransition(r.Context(), startReq)
	if err != nil {
		s.respondStartTransitionError(w, r, err)
		return
	}

	var tasks []engine.Task
	if s.transitionStore != nil {
		tasks, err = s.transitionStore.ListTransitionTasks(r.Context(), transition.ID)
		if err != nil {
			httputil.RespondProblem(w, r, http.StatusInternalServerError, "failed to load transition tasks")
			return
		}
	}

	w.Header().Set("Location", fmt.Sprintf("/power/v1/transitions/%s", transition.ID))
	httputil.RespondJSON(w, http.StatusAccepted, toTransitionResource(transition, tasks))
}

func (s *Server) loadTransition(ctx context.Context, id string) (engine.Transition, []engine.Task, error) {
	transition, err := s.transitionStore.GetTransition(ctx, id)
	if err != nil {
		return engine.Transition{}, nil, err
	}

	tasks, err := s.transitionStore.ListTransitionTasks(ctx, id)
	if err != nil {
		return engine.Transition{}, nil, err
	}

	return transition, tasks, nil
}

func (s *Server) resolveTargets(ctx context.Context, nodes, groups []string) ([]string, error) {
	unique := make(map[string]struct{})
	resolved := make([]string, 0, len(nodes))

	for _, node := range parseTargetList(nodes) {
		if _, exists := unique[node]; exists {
			continue
		}
		unique[node] = struct{}{}
		resolved = append(resolved, node)
	}

	groupNames := parseTargetList(groups)
	if len(groupNames) > 0 && s.resolveGroupMembers == nil {
		return nil, errGroupResolverUnavailable
	}

	for _, group := range groupNames {
		members, err := s.resolveGroupMembers(ctx, group)
		if err != nil {
			return nil, fmt.Errorf("resolving group %q: %w", group, err)
		}
		for _, member := range parseTargetList(members) {
			if _, exists := unique[member]; exists {
				continue
			}
			unique[member] = struct{}{}
			resolved = append(resolved, member)
		}
	}

	sort.Strings(resolved)
	if len(resolved) == 0 {
		return nil, engine.ErrNoTargetNodes
	}

	return resolved, nil
}

func (s *Server) respondTargetResolutionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, engine.ErrNoTargetNodes):
		httputil.RespondProblem(w, r, http.StatusBadRequest, engine.ErrNoTargetNodes.Error())
	case errors.Is(err, errGroupResolverUnavailable):
		httputil.RespondProblem(w, r, http.StatusServiceUnavailable, errGroupResolverUnavailable.Error())
	case errors.Is(err, ErrGroupNotFound):
		httputil.RespondProblem(w, r, http.StatusNotFound, err.Error())
	default:
		httputil.RespondProblemf(w, r, http.StatusBadGateway, "failed resolving targets: %v", err)
	}
}

func (s *Server) respondStartTransitionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, engine.ErrRunnerNotStarted):
		httputil.RespondProblem(w, r, http.StatusServiceUnavailable, engine.ErrRunnerNotStarted.Error())
	case errors.Is(err, engine.ErrNoTargetNodes):
		httputil.RespondProblem(w, r, http.StatusBadRequest, engine.ErrNoTargetNodes.Error())
	default:
		httputil.RespondProblemf(w, r, http.StatusInternalServerError, "failed to create transition: %v", err)
	}
}

func toTransitionResource(transition engine.Transition, tasks []engine.Task) httputil.Resource[transitionSpec] {
	taskSpecs := make([]transitionTaskSpec, 0, len(tasks))
	for _, task := range tasks {
		taskSpecs = append(taskSpecs, transitionTaskSpec{
			NodeID:          strings.TrimSpace(task.NodeID),
			BMCID:           strings.TrimSpace(task.BMCID),
			Endpoint:        strings.TrimSpace(task.BMCEndpoint),
			Operation:       strings.TrimSpace(task.Operation),
			State:           strings.TrimSpace(task.State),
			DryRun:          task.DryRun,
			AttemptCount:    task.AttemptCount,
			FinalPowerState: strings.TrimSpace(task.FinalPowerState),
			ErrorDetail:     strings.TrimSpace(task.ErrorDetail),
			QueuedAt:        newTimeRFC3339(task.QueuedAt),
			StartedAt:       toTimeRFC3339Ptr(task.StartedAt),
			CompletedAt:     toTimeRFC3339Ptr(task.CompletedAt),
		})
	}

	return httputil.Resource[transitionSpec]{
		Kind:       "Transition",
		APIVersion: "power/v1",
		Metadata: httputil.Metadata{
			ID:        strings.TrimSpace(transition.ID),
			CreatedAt: transition.CreatedAt,
			UpdatedAt: transition.UpdatedAt,
		},
		Spec: transitionSpec{
			RequestID:    strings.TrimSpace(transition.RequestID),
			Operation:    strings.TrimSpace(transition.Operation),
			State:        strings.TrimSpace(transition.State),
			RequestedBy:  strings.TrimSpace(transition.RequestedBy),
			DryRun:       transition.DryRun,
			TargetCount:  transition.TargetCount,
			SuccessCount: transition.SuccessCount,
			FailureCount: transition.FailureCount,
			QueuedAt:     newTimeRFC3339(transition.QueuedAt),
			StartedAt:    toTimeRFC3339Ptr(transition.StartedAt),
			CompletedAt:  toTimeRFC3339Ptr(transition.CompletedAt),
			Tasks:        taskSpecs,
		},
	}
}

func parseTargetList(items []string) []string {
	result := make([]string, 0, len(items))
	for _, raw := range items {
		for _, piece := range strings.Split(raw, ",") {
			normalized := strings.TrimSpace(piece)
			if normalized == "" {
				continue
			}
			result = append(result, normalized)
		}
	}
	return result
}

func parseListPagination(r *http.Request) (int, int, error) {
	limit := defaultTransitionListLimit
	offset := 0

	if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return 0, 0, fmt.Errorf("invalid limit value %q", value)
		}
		if parsed > maxTransitionListLimit {
			parsed = maxTransitionListLimit
		}
		limit = parsed
	}

	if value := strings.TrimSpace(r.URL.Query().Get("offset")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return 0, 0, fmt.Errorf("invalid offset value %q", value)
		}
		offset = parsed
	}

	return limit, offset, nil
}

func requestedByFromContext(r *http.Request) string {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		return "unknown"
	}
	subject := strings.TrimSpace(claims.Subject)
	if subject == "" {
		return "unknown"
	}
	return subject
}

func resolvedRequestID(r *http.Request, requested string) string {
	if normalized := strings.TrimSpace(requested); normalized != "" {
		return normalized
	}
	if requestID := strings.TrimSpace(r.Header.Get("X-Request-ID")); requestID != "" {
		return requestID
	}
	return ""
}

func requireAnyScope(scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				httputil.RespondProblem(w, r, http.StatusForbidden, "no claims in context")
				return
			}

			for _, scope := range scopes {
				if claims.HasScope(scope) {
					next.ServeHTTP(w, r)
					return
				}
			}
			httputil.RespondProblemf(w, r, http.StatusForbidden, "missing required scope: %s", scopes[0])
		})
	}
}
