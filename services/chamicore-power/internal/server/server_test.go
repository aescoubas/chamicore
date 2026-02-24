package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"git.cscs.ch/openchami/chamicore-power/internal/config"
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
	assert.Equal(t, http.StatusNotImplemented, resp.Code)
}
