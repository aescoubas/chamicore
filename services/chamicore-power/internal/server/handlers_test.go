package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"git.cscs.ch/openchami/chamicore-lib/auth"
	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-power/internal/config"
	"git.cscs.ch/openchami/chamicore-power/internal/engine"
	"git.cscs.ch/openchami/chamicore-power/internal/model"
	"git.cscs.ch/openchami/chamicore-power/internal/store"
)

type mockTransitionRunner struct {
	startTransitionFn func(ctx context.Context, req engine.StartRequest) (engine.Transition, error)
	abortTransitionFn func(ctx context.Context, transitionID string) error
}

func (m *mockTransitionRunner) StartTransition(ctx context.Context, req engine.StartRequest) (engine.Transition, error) {
	if m.startTransitionFn != nil {
		return m.startTransitionFn(ctx, req)
	}
	return engine.Transition{}, nil
}

func (m *mockTransitionRunner) AbortTransition(ctx context.Context, transitionID string) error {
	if m.abortTransitionFn != nil {
		return m.abortTransitionFn(ctx, transitionID)
	}
	return nil
}

type mockPowerStore struct {
	pingFn                func(ctx context.Context) error
	resolveNodeMappingsFn func(ctx context.Context, nodeIDs []string) ([]model.NodePowerMapping, []model.NodeMappingError, error)
	listTransitionsFn     func(ctx context.Context, limit, offset int) ([]engine.Transition, int, error)
	getTransitionFn       func(ctx context.Context, id string) (engine.Transition, error)
	listTransitionTasksFn func(ctx context.Context, transitionID string) ([]engine.Task, error)
	listLatestTasksByNode func(ctx context.Context, nodeIDs []string) ([]engine.Task, error)
	replaceMappingsFn     func(ctx context.Context, endpoints []model.BMCEndpoint, links []model.NodeBMCLink, syncedAt time.Time) (model.MappingApplyCounts, error)
	listBMCEndpointsFn    func(ctx context.Context) ([]model.BMCEndpoint, error)
	listNodeBMCLinksFn    func(ctx context.Context) ([]model.NodeBMCLink, error)
}

func (m *mockPowerStore) Ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}

func (m *mockPowerStore) ReplaceTopologyMappings(
	ctx context.Context,
	endpoints []model.BMCEndpoint,
	links []model.NodeBMCLink,
	syncedAt time.Time,
) (model.MappingApplyCounts, error) {
	if m.replaceMappingsFn != nil {
		return m.replaceMappingsFn(ctx, endpoints, links, syncedAt)
	}
	return model.MappingApplyCounts{}, nil
}

func (m *mockPowerStore) ResolveNodeMappings(
	ctx context.Context,
	nodeIDs []string,
) ([]model.NodePowerMapping, []model.NodeMappingError, error) {
	if m.resolveNodeMappingsFn != nil {
		return m.resolveNodeMappingsFn(ctx, nodeIDs)
	}
	return []model.NodePowerMapping{}, []model.NodeMappingError{}, nil
}

func (m *mockPowerStore) ListBMCEndpoints(ctx context.Context) ([]model.BMCEndpoint, error) {
	if m.listBMCEndpointsFn != nil {
		return m.listBMCEndpointsFn(ctx)
	}
	return []model.BMCEndpoint{}, nil
}

func (m *mockPowerStore) ListNodeBMCLinks(ctx context.Context) ([]model.NodeBMCLink, error) {
	if m.listNodeBMCLinksFn != nil {
		return m.listNodeBMCLinksFn(ctx)
	}
	return []model.NodeBMCLink{}, nil
}

func (m *mockPowerStore) ListTransitions(ctx context.Context, limit, offset int) ([]engine.Transition, int, error) {
	if m.listTransitionsFn != nil {
		return m.listTransitionsFn(ctx, limit, offset)
	}
	return []engine.Transition{}, 0, nil
}

func (m *mockPowerStore) GetTransition(ctx context.Context, id string) (engine.Transition, error) {
	if m.getTransitionFn != nil {
		return m.getTransitionFn(ctx, id)
	}
	return engine.Transition{}, store.ErrNotFound
}

func (m *mockPowerStore) ListTransitionTasks(ctx context.Context, transitionID string) ([]engine.Task, error) {
	if m.listTransitionTasksFn != nil {
		return m.listTransitionTasksFn(ctx, transitionID)
	}
	return []engine.Task{}, nil
}

func (m *mockPowerStore) ListLatestTransitionTasksByNode(ctx context.Context, nodeIDs []string) ([]engine.Task, error) {
	if m.listLatestTasksByNode != nil {
		return m.listLatestTasksByNode(ctx, nodeIDs)
	}
	return []engine.Task{}, nil
}

func newHandlerTestServer(
	t *testing.T,
	st *mockPowerStore,
	runner *mockTransitionRunner,
	resolver func(ctx context.Context, group string) ([]string, error),
) *Server {
	t.Helper()

	opts := []Option{}
	if runner != nil {
		opts = append(opts, WithTransitionRunner(runner))
	}
	if resolver != nil {
		opts = append(opts, WithGroupMemberResolver(resolver))
	}

	cfg := config.Config{DevMode: true, BulkMaxNodes: 20}
	return New(st, cfg, "v1", "abc", "now", opts...)
}

func TestPowerEndpointsExist(t *testing.T) {
	srv := newHandlerTestServer(t, &mockPowerStore{}, &mockTransitionRunner{}, func(ctx context.Context, group string) ([]string, error) {
		return []string{"node-1"}, nil
	})
	router := srv.Router()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{name: "list transitions", method: http.MethodGet, path: "/power/v1/transitions", status: http.StatusOK},
		{name: "create transitions", method: http.MethodPost, path: "/power/v1/transitions", body: `{"operation":"On","nodes":["node-1"]}`, status: http.StatusAccepted},
		{name: "get transition", method: http.MethodGet, path: "/power/v1/transitions/t1", status: http.StatusNotFound},
		{name: "abort transition", method: http.MethodDelete, path: "/power/v1/transitions/t1", status: http.StatusNotFound},
		{name: "power status", method: http.MethodGet, path: "/power/v1/power-status?nodes=node-1", status: http.StatusOK},
		{name: "action on", method: http.MethodPost, path: "/power/v1/actions/on", body: `{"nodes":["node-1"]}`, status: http.StatusAccepted},
		{name: "action off", method: http.MethodPost, path: "/power/v1/actions/off", body: `{"nodes":["node-1"]}`, status: http.StatusAccepted},
		{name: "action reboot", method: http.MethodPost, path: "/power/v1/actions/reboot", body: `{"nodes":["node-1"]}`, status: http.StatusAccepted},
		{name: "action reset", method: http.MethodPost, path: "/power/v1/actions/reset", body: `{"operation":"ForceRestart","nodes":["node-1"]}`, status: http.StatusAccepted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body *bytes.Reader
			if tt.body != "" {
				body = bytes.NewReader([]byte(tt.body))
			} else {
				body = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(tt.method, tt.path, body)
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)
			assert.Equal(t, tt.status, resp.Code)
		})
	}
}

func TestCreateTransition_RejectsUnknownFields(t *testing.T) {
	srv := newHandlerTestServer(t, &mockPowerStore{}, &mockTransitionRunner{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/power/v1/transitions", bytes.NewBufferString(`{"operation":"On","nodes":["node-1"],"unexpected":true}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestCreateTransition_RejectsInvalidOperation(t *testing.T) {
	srv := newHandlerTestServer(t, &mockPowerStore{}, &mockTransitionRunner{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/power/v1/transitions", bytes.NewBufferString(`{"operation":"invalid","nodes":["node-1"]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestCreateTransition_EnforcesBulkLimit(t *testing.T) {
	runner := &mockTransitionRunner{}
	srv := New(
		&mockPowerStore{},
		config.Config{DevMode: true, BulkMaxNodes: 2},
		"v1",
		"abc",
		"now",
		WithTransitionRunner(runner),
	)

	req := httptest.NewRequest(http.MethodPost, "/power/v1/transitions", bytes.NewBufferString(`{"operation":"On","nodes":["node-1","node-2","node-3"]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestCreateTransition_ExpandsGroupsAndReturnsTasks(t *testing.T) {
	called := false
	runner := &mockTransitionRunner{
		startTransitionFn: func(ctx context.Context, req engine.StartRequest) (engine.Transition, error) {
			called = true
			assert.Equal(t, []string{"node-1", "node-2"}, req.NodeIDs)
			return engine.Transition{
				ID:          "transition-1",
				Operation:   req.Operation,
				State:       engine.TransitionStatePending,
				TargetCount: len(req.NodeIDs),
				QueuedAt:    time.Now().UTC(),
				CreatedAt:   time.Now().UTC(),
				UpdatedAt:   time.Now().UTC(),
			}, nil
		},
	}
	st := &mockPowerStore{
		listTransitionTasksFn: func(ctx context.Context, transitionID string) ([]engine.Task, error) {
			return []engine.Task{
				{NodeID: "node-1", Operation: "On", State: engine.TaskStatePending, QueuedAt: time.Now().UTC()},
				{NodeID: "node-2", Operation: "On", State: engine.TaskStatePending, QueuedAt: time.Now().UTC()},
			}, nil
		},
	}

	srv := newHandlerTestServer(t, st, runner, func(ctx context.Context, group string) ([]string, error) {
		require.Equal(t, "compute", group)
		return []string{"node-2"}, nil
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/power/v1/transitions",
		bytes.NewBufferString(`{"operation":"On","nodes":["node-1"],"groups":["compute"]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)

	require.True(t, called)
	require.Equal(t, http.StatusAccepted, resp.Code)
	assert.Equal(t, "/power/v1/transitions/transition-1", resp.Header().Get("Location"))

	var out httputil.Resource[transitionSpec]
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Spec.Tasks, 2)
	assert.Equal(t, "node-1", out.Spec.Tasks[0].NodeID)
	assert.Equal(t, "node-2", out.Spec.Tasks[1].NodeID)
}

func TestGetTransition_ReturnsPerNodeResults(t *testing.T) {
	st := &mockPowerStore{
		getTransitionFn: func(ctx context.Context, id string) (engine.Transition, error) {
			return engine.Transition{
				ID:           id,
				Operation:    "On",
				State:        engine.TransitionStatePartial,
				TargetCount:  2,
				SuccessCount: 1,
				FailureCount: 1,
				QueuedAt:     time.Now().UTC(),
				CreatedAt:    time.Now().UTC(),
				UpdatedAt:    time.Now().UTC(),
			}, nil
		},
		listTransitionTasksFn: func(ctx context.Context, transitionID string) ([]engine.Task, error) {
			return []engine.Task{
				{NodeID: "node-1", Operation: "On", State: engine.TaskStateSucceeded, FinalPowerState: "On", QueuedAt: time.Now().UTC()},
				{NodeID: "node-2", Operation: "On", State: engine.TaskStateFailed, ErrorDetail: "timeout", QueuedAt: time.Now().UTC()},
			}, nil
		},
	}

	srv := newHandlerTestServer(t, st, &mockTransitionRunner{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/power/v1/transitions/transition-1", nil)
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var out httputil.Resource[transitionSpec]
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Spec.Tasks, 2)
	assert.Equal(t, engine.TaskStateFailed, out.Spec.Tasks[1].State)
	assert.Equal(t, "timeout", out.Spec.Tasks[1].ErrorDetail)
}

func TestDeleteTransition_AbortsAndReturnsTransition(t *testing.T) {
	runner := &mockTransitionRunner{
		abortTransitionFn: func(ctx context.Context, transitionID string) error {
			assert.Equal(t, "transition-1", transitionID)
			return nil
		},
	}
	st := &mockPowerStore{
		getTransitionFn: func(ctx context.Context, id string) (engine.Transition, error) {
			return engine.Transition{ID: id, Operation: "On", State: engine.TransitionStateCanceled, QueuedAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, nil
		},
	}

	srv := newHandlerTestServer(t, st, runner, nil)
	req := httptest.NewRequest(http.MethodDelete, "/power/v1/transitions/transition-1", nil)
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)

	assert.Equal(t, http.StatusAccepted, resp.Code)
}

func TestPowerStatus_ReturnsPerNodeStatusAndMissingMappings(t *testing.T) {
	st := &mockPowerStore{
		resolveNodeMappingsFn: func(ctx context.Context, nodeIDs []string) ([]model.NodePowerMapping, []model.NodeMappingError, error) {
			return []model.NodePowerMapping{{NodeID: "node-1", BMCID: "bmc-1", Endpoint: "https://bmc-1", CredentialID: "cred-1"}}, []model.NodeMappingError{{
				NodeID: "node-2",
				Code:   model.MappingErrorCodeNotFound,
				Detail: "node mapping missing",
			}}, nil
		},
		listLatestTasksByNode: func(ctx context.Context, nodeIDs []string) ([]engine.Task, error) {
			return []engine.Task{{
				TransitionID:    "transition-1",
				NodeID:          "node-1",
				BMCID:           "bmc-1",
				Operation:       "On",
				State:           engine.TaskStateSucceeded,
				FinalPowerState: "On",
				UpdatedAt:       time.Now().UTC(),
				CompletedAt:     ptrTime(time.Now().UTC()),
			}}, nil
		},
	}

	srv := newHandlerTestServer(t, st, &mockTransitionRunner{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/power/v1/power-status?nodes=node-1,node-2", nil)
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var out httputil.Resource[powerStatusResponse]
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Spec.NodeStatuses, 2)
	assert.Equal(t, "node-1", out.Spec.NodeStatuses[0].NodeID)
	assert.Equal(t, engine.TaskStateSucceeded, out.Spec.NodeStatuses[0].State)
	assert.Equal(t, "node-2", out.Spec.NodeStatuses[1].NodeID)
	assert.Equal(t, "unresolved", out.Spec.NodeStatuses[1].State)
}

func TestActionReset_RejectsInvalidOperation(t *testing.T) {
	srv := newHandlerTestServer(t, &mockPowerStore{}, &mockTransitionRunner{}, nil)
	req := httptest.NewRequest(http.MethodPost, "/power/v1/actions/reset", bytes.NewBufferString(`{"operation":"broken","nodes":["node-1"]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestRequireAnyScope(t *testing.T) {
	hit := false
	mw := requireAnyScope("read:power", "admin")
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("scope allowed", func(t *testing.T) {
		hit = false
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.Claims{Scopes: []string{"read:power"}}))
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusNoContent, resp.Code)
		assert.True(t, hit)
	})

	t.Run("missing scope forbidden", func(t *testing.T) {
		hit = false
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.Claims{Scopes: []string{"read:components"}}))
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusForbidden, resp.Code)
		assert.False(t, hit)
	})
}

func TestCreateTransition_GroupNotFound(t *testing.T) {
	srv := newHandlerTestServer(t, &mockPowerStore{}, &mockTransitionRunner{}, func(ctx context.Context, group string) ([]string, error) {
		return nil, fmt.Errorf("%w: %s", ErrGroupNotFound, group)
	})

	req := httptest.NewRequest(http.MethodPost, "/power/v1/transitions", bytes.NewBufferString(`{"operation":"On","groups":["missing"]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)

	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func ptrTime(v time.Time) *time.Time {
	return &v
}
