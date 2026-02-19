// TEMPLATE: Handler tests for __SERVICE_FULL__
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
//
// This file demonstrates the standard handler test patterns:
//   - Table-driven tests with descriptive names.
//   - Mock store with function fields (only set what the test needs).
//   - httptest for recording responses.
//   - Assertions on status code, response body structure, and headers.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// TEMPLATE: Update these imports to match your service module path.
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/config"
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/model"
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/store"
	"git.cscs.ch/openchami/__SERVICE_FULL__/pkg/types"

	// Shared library packages.
	"git.cscs.ch/openchami/chamicore-lib/httputil"
)

// testConfig returns a minimal config for test servers.
func testConfig() config.Config {
	return config.Config{
		ListenAddr: ":0",
		DevMode:    true, // Skip auth in unit tests.
	}
}

// testServer creates a Server with the given mock store for testing.
func testServer(st store.Store) *Server {
	return New(st, testConfig(), "test", "abc123", "2025-01-01")
}

// ---------------------------------------------------------------------------
// GET __API_PREFIX__/__RESOURCE_PLURAL__/{id}
// ---------------------------------------------------------------------------

func TestHandleGet__RESOURCE___Success(t *testing.T) {
	now := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	mock := &store.MockStore{
		Get__RESOURCE__Fn: func(ctx context.Context, id string) (model.__RESOURCE__, error) {
			return model.__RESOURCE__{
				ID:          id,
				Name:        "test-name",
				Description: "test-desc",
				CreatedAt:   now,
				UpdatedAt:   now,
			}, nil
		},
	}

	srv := testServer(mock)
	req := httptest.NewRequest(http.MethodGet, "__API_PREFIX__/__RESOURCE_PLURAL__/test-id", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("ETag"))

	var resp httputil.Resource[types.__RESOURCE__]
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, resourceKind, resp.Kind)
	assert.Equal(t, resourceAPIVersion, resp.APIVersion)
	assert.Equal(t, "test-id", resp.Metadata.ID)
	assert.Equal(t, "test-name", resp.Spec.Name)
}

func TestHandleGet__RESOURCE___NotFound(t *testing.T) {
	mock := &store.MockStore{
		Get__RESOURCE__Fn: func(ctx context.Context, id string) (model.__RESOURCE__, error) {
			return model.__RESOURCE__{}, store.ErrNotFound
		},
	}

	srv := testServer(mock)
	req := httptest.NewRequest(http.MethodGet, "__API_PREFIX__/__RESOURCE_PLURAL__/nonexistent", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var problem httputil.ProblemDetail
	err := json.NewDecoder(rec.Body).Decode(&problem)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, problem.Status)
	assert.Contains(t, problem.Detail, "nonexistent")
}

// ---------------------------------------------------------------------------
// GET __API_PREFIX__/__RESOURCE_PLURAL__
// ---------------------------------------------------------------------------

func TestHandleList__RESOURCE__s_Success(t *testing.T) {
	now := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	mock := &store.MockStore{
		List__RESOURCE__sFn: func(ctx context.Context, opts store.ListOptions) ([]model.__RESOURCE__, int, error) {
			return []model.__RESOURCE__{
				{ID: "id-1", Name: "first", CreatedAt: now, UpdatedAt: now},
				{ID: "id-2", Name: "second", CreatedAt: now, UpdatedAt: now},
			}, 2, nil
		},
	}

	srv := testServer(mock)
	req := httptest.NewRequest(http.MethodGet, "__API_PREFIX__/__RESOURCE_PLURAL__", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp httputil.ResourceList[types.__RESOURCE__]
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, resourceListKind, resp.Kind)
	assert.Equal(t, 2, resp.Metadata.Total)
	assert.Len(t, resp.Items, 2)
}

func TestHandleList__RESOURCE__s_Empty(t *testing.T) {
	mock := &store.MockStore{
		List__RESOURCE__sFn: func(ctx context.Context, opts store.ListOptions) ([]model.__RESOURCE__, int, error) {
			return []model.__RESOURCE__{}, 0, nil
		},
	}

	srv := testServer(mock)
	req := httptest.NewRequest(http.MethodGet, "__API_PREFIX__/__RESOURCE_PLURAL__", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp httputil.ResourceList[types.__RESOURCE__]
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Metadata.Total)
	assert.Empty(t, resp.Items)
}

// ---------------------------------------------------------------------------
// POST __API_PREFIX__/__RESOURCE_PLURAL__
// ---------------------------------------------------------------------------

func TestHandleCreate__RESOURCE___Success(t *testing.T) {
	now := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	mock := &store.MockStore{
		Create__RESOURCE__Fn: func(ctx context.Context, m model.__RESOURCE__) (model.__RESOURCE__, error) {
			m.ID = "new-id"
			m.CreatedAt = now
			m.UpdatedAt = now
			return m, nil
		},
	}

	srv := testServer(mock)
	body := `{"name":"new-resource","description":"a description"}`
	req := httptest.NewRequest(http.MethodPost, "__API_PREFIX__/__RESOURCE_PLURAL__", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "new-id")

	var resp httputil.Resource[types.__RESOURCE__]
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "new-id", resp.Metadata.ID)
	assert.Equal(t, "new-resource", resp.Spec.Name)
}

func TestHandleCreate__RESOURCE___ValidationError(t *testing.T) {
	mock := &store.MockStore{} // Create__RESOURCE__Fn not set — should not be called.

	srv := testServer(mock)
	body := `{"name":"","description":"missing name"}`
	req := httptest.NewRequest(http.MethodPost, "__API_PREFIX__/__RESOURCE_PLURAL__", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	var problem httputil.ProblemDetail
	err := json.NewDecoder(rec.Body).Decode(&problem)
	require.NoError(t, err)
	assert.NotEmpty(t, problem.Errors)
	assert.Equal(t, "name", problem.Errors[0].Field)
}

func TestHandleCreate__RESOURCE___Conflict(t *testing.T) {
	mock := &store.MockStore{
		Create__RESOURCE__Fn: func(ctx context.Context, m model.__RESOURCE__) (model.__RESOURCE__, error) {
			return model.__RESOURCE__{}, store.ErrConflict
		},
	}

	srv := testServer(mock)
	body := `{"name":"duplicate"}`
	req := httptest.NewRequest(http.MethodPost, "__API_PREFIX__/__RESOURCE_PLURAL__", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

// ---------------------------------------------------------------------------
// DELETE __API_PREFIX__/__RESOURCE_PLURAL__/{id}
// ---------------------------------------------------------------------------

func TestHandleDelete__RESOURCE___Success(t *testing.T) {
	mock := &store.MockStore{
		Delete__RESOURCE__Fn: func(ctx context.Context, id string) error {
			return nil
		},
	}

	srv := testServer(mock)
	req := httptest.NewRequest(http.MethodDelete, "__API_PREFIX__/__RESOURCE_PLURAL__/delete-me", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestHandleDelete__RESOURCE___NotFound(t *testing.T) {
	mock := &store.MockStore{
		Delete__RESOURCE__Fn: func(ctx context.Context, id string) error {
			return store.ErrNotFound
		},
	}

	srv := testServer(mock)
	req := httptest.NewRequest(http.MethodDelete, "__API_PREFIX__/__RESOURCE_PLURAL__/gone", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ---------------------------------------------------------------------------
// Pagination helper
// ---------------------------------------------------------------------------

func TestParsePagination(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		wantLimit     int
		wantOffset    int
		wantErr       bool
	}{
		{"defaults", "", defaultPageLimit, 0, false},
		{"custom limit", "?limit=10", 10, 0, false},
		{"custom offset", "?offset=50", defaultPageLimit, 50, false},
		{"both", "?limit=25&offset=100", 25, 100, false},
		{"limit clamped", fmt.Sprintf("?limit=%d", maxPageLimit+1), maxPageLimit, 0, false},
		{"invalid limit", "?limit=abc", 0, 0, true},
		{"negative limit", "?limit=-1", 0, 0, true},
		{"invalid offset", "?offset=abc", 0, 0, true},
		{"negative offset", "?offset=-1", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test"+tt.query, nil)
			limit, offset, err := parsePagination(req)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantLimit, limit)
			assert.Equal(t, tt.wantOffset, offset)
		})
	}
}
