package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"git.cscs.ch/openchami/chamicore-power/internal/config"
	"git.cscs.ch/openchami/chamicore-power/internal/model"
)

type mockStore struct {
	pingFn func(ctx context.Context) error
}

func (m *mockStore) Ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}

func (m *mockStore) ReplaceTopologyMappings(
	ctx context.Context,
	endpoints []model.BMCEndpoint,
	links []model.NodeBMCLink,
	syncedAt time.Time,
) (model.MappingApplyCounts, error) {
	return model.MappingApplyCounts{}, nil
}

func (m *mockStore) ResolveNodeMappings(
	ctx context.Context,
	nodeIDs []string,
) ([]model.NodePowerMapping, []model.NodeMappingError, error) {
	return []model.NodePowerMapping{}, []model.NodeMappingError{}, nil
}

func (m *mockStore) ListBMCEndpoints(ctx context.Context) ([]model.BMCEndpoint, error) {
	return []model.BMCEndpoint{}, nil
}

func (m *mockStore) ListNodeBMCLinks(ctx context.Context) ([]model.NodeBMCLink, error) {
	return []model.NodeBMCLink{}, nil
}

type mockMappingSyncer struct {
	triggerFn func(ctx context.Context) error
	isReadyFn func() bool
}

func (m *mockMappingSyncer) Trigger(ctx context.Context) error {
	if m.triggerFn != nil {
		return m.triggerFn(ctx)
	}
	return nil
}

func (m *mockMappingSyncer) IsReady() bool {
	if m.isReadyFn != nil {
		return m.isReadyFn()
	}
	return true
}

func TestServer_PublicEndpoints(t *testing.T) {
	srv := New(&mockStore{}, config.Config{MetricsEnabled: true, DevMode: true}, "v1", "abc", "now")
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)

	req = httptest.NewRequest(http.MethodGet, "/readiness", nil)
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)

	req = httptest.NewRequest(http.MethodGet, "/version", nil)
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)
}

func TestServer_ReadinessFailure(t *testing.T) {
	srv := New(&mockStore{pingFn: func(context.Context) error {
		return errors.New("db down")
	}}, config.Config{DevMode: true}, "v1", "abc", "now")

	req := httptest.NewRequest(http.MethodGet, "/readiness", nil)
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)
	require.Equal(t, http.StatusServiceUnavailable, resp.Code)
}

func TestServer_ReadinessRequiresMappingSync(t *testing.T) {
	syncMock := &mockMappingSyncer{
		isReadyFn: func() bool { return false },
	}

	srv := New(&mockStore{}, config.Config{DevMode: true}, "v1", "abc", "now", WithMappingSyncer(syncMock))

	req := httptest.NewRequest(http.MethodGet, "/readiness", nil)
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)
	require.Equal(t, http.StatusServiceUnavailable, resp.Code)

	syncMock.isReadyFn = func() bool { return true }
	req = httptest.NewRequest(http.MethodGet, "/readiness", nil)
	resp = httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)
}

func TestServer_AuthRequiredWhenNotDevMode(t *testing.T) {
	srv := New(&mockStore{}, config.Config{DevMode: false}, "v1", "abc", "now")

	req := httptest.NewRequest(http.MethodGet, "/power/v1/transitions", nil)
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)
	require.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestServer_PowerRoutesScopedInDevMode(t *testing.T) {
	srv := New(&mockStore{}, config.Config{DevMode: true}, "v1", "abc", "now")
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/power/v1/transitions", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusNotImplemented, resp.Code)

	req = httptest.NewRequest(http.MethodPost, "/power/v1/actions/on", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusNotImplemented, resp.Code)

	req = httptest.NewRequest(http.MethodPost, "/power/v1/admin/mappings/sync", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
}

func TestServer_AdminSyncMappings_Success(t *testing.T) {
	srv := New(
		&mockStore{},
		config.Config{DevMode: true},
		"v1",
		"abc",
		"now",
		WithMappingSyncer(&mockMappingSyncer{}),
	)

	req := httptest.NewRequest(http.MethodPost, "/power/v1/admin/mappings/sync", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)
	require.Equal(t, http.StatusAccepted, resp.Code)
}

func TestServer_AdminSyncMappings_Error(t *testing.T) {
	srv := New(
		&mockStore{},
		config.Config{DevMode: true},
		"v1",
		"abc",
		"now",
		WithMappingSyncer(&mockMappingSyncer{
			triggerFn: func(ctx context.Context) error {
				return errors.New("boom")
			},
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/power/v1/admin/mappings/sync", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Router().ServeHTTP(resp, req)
	require.Equal(t, http.StatusInternalServerError, resp.Code)
}
