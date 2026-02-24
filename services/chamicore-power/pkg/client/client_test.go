package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
	baseclient "git.cscs.ch/openchami/chamicore-lib/httputil/client"
	"git.cscs.ch/openchami/chamicore-power/pkg/types"
)

func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func problemJSON(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(httputil.ProblemDetail{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	})
}

func newTestClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := New(cfg)
	require.NoError(t, err)
	return c
}

func transitionResource(id, state string) httputil.Resource[types.Transition] {
	return httputil.Resource[types.Transition]{
		Kind:       "Transition",
		APIVersion: "power/v1",
		Metadata:   httputil.Metadata{ID: id},
		Spec: types.Transition{
			Operation: "On",
			State:     state,
			QueuedAt:  time.Now().UTC(),
		},
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("requires base url", func(t *testing.T) {
		t.Parallel()
		c, err := New(Config{})
		require.Error(t, err)
		assert.Nil(t, c)
		assert.Contains(t, err.Error(), "BaseURL is required")
	})

	t.Run("applies defaults", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, Config{BaseURL: " http://example.invalid/ "})
		assert.Equal(t, "http://example.invalid", c.baseURL)
		assert.Equal(t, defaultTimeout, c.cfg.Timeout)
		assert.Equal(t, defaultMaxRetries, c.cfg.MaxRetries)
	})

	t.Run("uses custom values", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, Config{
			BaseURL:    "http://example.invalid",
			Token:      "token",
			Timeout:    5 * time.Second,
			MaxRetries: 9,
		})
		assert.Equal(t, "token", c.cfg.Token)
		assert.Equal(t, 5*time.Second, c.cfg.Timeout)
		assert.Equal(t, 9, c.cfg.MaxRetries)
	})
}

func TestListTransitions(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/power/v1/transitions", r.URL.Path)
		assert.Equal(t, "50", r.URL.Query().Get("limit"))
		assert.Equal(t, "3", r.URL.Query().Get("offset"))
		respondJSON(w, http.StatusOK, httputil.ResourceList[types.Transition]{
			Kind:       "TransitionList",
			APIVersion: "power/v1",
			Metadata:   httputil.ListMetadata{Total: 1, Limit: 50, Offset: 3},
			Items: []httputil.Resource[types.Transition]{
				transitionResource("t-1", types.TransitionStatePending),
			},
		})
	}))
	defer ts.Close()

	c := newTestClient(t, Config{BaseURL: ts.URL})
	resp, err := c.ListTransitions(context.Background(), ListTransitionsOptions{Limit: 50, Offset: 3})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Items, 1)
	assert.Equal(t, "t-1", resp.Items[0].Metadata.ID)
}

func TestCreateTransition_PropagatesHeaders(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/power/v1/transitions", r.URL.Path)
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		assert.Equal(t, "req-123", r.Header.Get("X-Request-ID"))

		var req types.CreateTransitionRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "On", req.Operation)
		assert.Equal(t, []string{"node-1"}, req.Nodes)

		respondJSON(w, http.StatusAccepted, transitionResource("t-1", types.TransitionStatePending))
	}))
	defer ts.Close()

	c := newTestClient(t, Config{BaseURL: ts.URL, Token: "token"})
	ctx := context.WithValue(context.Background(), baseclient.RequestIDKey, "req-123")
	resp, err := c.CreateTransition(ctx, types.CreateTransitionRequest{
		Operation: "On",
		Nodes:     []string{"node-1"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "t-1", resp.Metadata.ID)
	assert.Equal(t, types.TransitionStatePending, resp.Spec.State)
}

func TestStartTransition_Alias(t *testing.T) {
	t.Parallel()

	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		respondJSON(w, http.StatusAccepted, transitionResource("t-1", types.TransitionStatePending))
	}))
	defer ts.Close()

	c := newTestClient(t, Config{BaseURL: ts.URL})
	resp, err := c.StartTransition(context.Background(), types.CreateTransitionRequest{Operation: "On", Nodes: []string{"node-1"}})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits))
}

func TestGetTransition(t *testing.T) {
	t.Parallel()

	t.Run("requires id", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, Config{BaseURL: "http://example.invalid"})
		resp, err := c.GetTransition(context.Background(), "  ")
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "transition id is required")
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/power/v1/transitions/t-1", r.URL.Path)
			respondJSON(w, http.StatusOK, transitionResource("t-1", types.TransitionStateCompleted))
		}))
		defer ts.Close()

		c := newTestClient(t, Config{BaseURL: ts.URL})
		resp, err := c.GetTransition(context.Background(), "t-1")
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, types.TransitionStateCompleted, resp.Spec.State)
	})
}

func TestAbortTransition(t *testing.T) {
	t.Parallel()

	t.Run("requires id", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, Config{BaseURL: "http://example.invalid"})
		resp, err := c.AbortTransition(context.Background(), " ")
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "transition id is required")
	})

	t.Run("delete then get", func(t *testing.T) {
		t.Parallel()
		var calls int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch atomic.AddInt32(&calls, 1) {
			case 1:
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/power/v1/transitions/t-1", r.URL.Path)
				respondJSON(w, http.StatusAccepted, transitionResource("t-1", types.TransitionStateCanceled))
			case 2:
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/power/v1/transitions/t-1", r.URL.Path)
				respondJSON(w, http.StatusOK, transitionResource("t-1", types.TransitionStateCanceled))
			default:
				t.Fatalf("unexpected request count: %d", calls)
			}
		}))
		defer ts.Close()

		c := newTestClient(t, Config{BaseURL: ts.URL})
		resp, err := c.AbortTransition(context.Background(), "t-1")
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, types.TransitionStateCanceled, resp.Spec.State)
		assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
	})

	t.Run("delete error", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			problemJSON(w, http.StatusForbidden, "insufficient scope")
		}))
		defer ts.Close()

		c := newTestClient(t, Config{BaseURL: ts.URL})
		resp, err := c.AbortTransition(context.Background(), "t-1")
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "aborting transition")
	})

	t.Run("get after delete error", func(t *testing.T) {
		t.Parallel()
		var calls int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if atomic.AddInt32(&calls, 1) == 1 {
				respondJSON(w, http.StatusAccepted, transitionResource("t-1", types.TransitionStateCanceled))
				return
			}
			problemJSON(w, http.StatusInternalServerError, "failed to load transition")
		}))
		defer ts.Close()

		c := newTestClient(t, Config{BaseURL: ts.URL})
		resp, err := c.AbortTransition(context.Background(), "t-1")
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "loading transition")
	})
}

func TestWaitTransition(t *testing.T) {
	t.Parallel()

	t.Run("requires id", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, Config{BaseURL: "http://example.invalid"})
		resp, err := c.WaitTransition(context.Background(), " ", WaitTransitionOptions{})
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "transition id is required")
	})

	t.Run("returns terminal state immediately", func(t *testing.T) {
		t.Parallel()
		var calls int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&calls, 1)
			respondJSON(w, http.StatusOK, transitionResource("t-1", types.TransitionStateCompleted))
		}))
		defer ts.Close()

		c := newTestClient(t, Config{BaseURL: ts.URL})
		resp, err := c.WaitTransition(context.Background(), "t-1", WaitTransitionOptions{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, types.TransitionStateCompleted, resp.Spec.State)
		assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
	})

	t.Run("polls pending to completion", func(t *testing.T) {
		t.Parallel()
		var calls int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			call := atomic.AddInt32(&calls, 1)
			if call < 3 {
				respondJSON(w, http.StatusOK, transitionResource("t-1", types.TransitionStateRunning))
				return
			}
			respondJSON(w, http.StatusOK, transitionResource("t-1", types.TransitionStateCompleted))
		}))
		defer ts.Close()

		c := newTestClient(t, Config{BaseURL: ts.URL})
		resp, err := c.WaitTransition(context.Background(), "t-1", WaitTransitionOptions{Interval: 5 * time.Millisecond})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, types.TransitionStateCompleted, resp.Spec.State)
		assert.GreaterOrEqual(t, atomic.LoadInt32(&calls), int32(3))
	})

	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			respondJSON(w, http.StatusOK, transitionResource("t-1", types.TransitionStateRunning))
		}))
		defer ts.Close()

		c := newTestClient(t, Config{BaseURL: ts.URL})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		resp, err := c.WaitTransition(ctx, "t-1", WaitTransitionOptions{Interval: 200 * time.Millisecond})
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context deadline exceeded")
	})

	t.Run("propagates get errors", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			problemJSON(w, http.StatusNotFound, "transition not found")
		}))
		defer ts.Close()

		c := newTestClient(t, Config{BaseURL: ts.URL})
		resp, err := c.WaitTransition(context.Background(), "t-404", WaitTransitionOptions{Interval: 5 * time.Millisecond})
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "waiting transition")
	})
}

func TestGetPowerStatus(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, powerStatusPath, r.URL.Path)
		assert.Equal(t, []string{"node-1", "node-2"}, r.URL.Query()["nodes"])
		assert.Equal(t, []string{"compute"}, r.URL.Query()["groups"])
		respondJSON(w, http.StatusOK, httputil.Resource[types.PowerStatus]{
			Kind:       "PowerStatus",
			APIVersion: "power/v1",
			Metadata:   httputil.Metadata{ID: "power-status"},
			Spec: types.PowerStatus{
				Total: 1,
				NodeStatuses: []types.PowerNodeStatus{
					{
						NodeID:     "node-1",
						State:      types.TaskStateSucceeded,
						PowerState: "On",
					},
				},
			},
		})
	}))
	defer ts.Close()

	c := newTestClient(t, Config{BaseURL: ts.URL})
	resp, err := c.GetPowerStatus(context.Background(), PowerStatusOptions{
		Nodes:  []string{"node-1", " ", "node-2"},
		Groups: []string{"compute"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 1, resp.Spec.Total)
	require.Len(t, resp.Spec.NodeStatuses, 1)
	assert.Equal(t, "node-1", resp.Spec.NodeStatuses[0].NodeID)
}

func TestGetPowerStatus_EmptyQueryAndError(t *testing.T) {
	t.Parallel()

	t.Run("empty options keep path without query", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, powerStatusPath, r.URL.Path)
			assert.Equal(t, "", r.URL.RawQuery)
			respondJSON(w, http.StatusOK, httputil.Resource[types.PowerStatus]{
				Kind:       "PowerStatus",
				APIVersion: "power/v1",
				Metadata:   httputil.Metadata{ID: "power-status"},
				Spec:       types.PowerStatus{Total: 0, NodeStatuses: []types.PowerNodeStatus{}},
			})
		}))
		defer ts.Close()

		c := newTestClient(t, Config{BaseURL: ts.URL})
		resp, err := c.GetPowerStatus(context.Background(), PowerStatusOptions{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, 0, resp.Spec.Total)
	})

	t.Run("error from server", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			problemJSON(w, http.StatusBadGateway, "status backend unavailable")
		}))
		defer ts.Close()

		c := newTestClient(t, Config{BaseURL: ts.URL})
		resp, err := c.GetPowerStatus(context.Background(), PowerStatusOptions{})
		assert.Nil(t, resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "getting power status")
	})
}

func TestActionEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		call func(ctx context.Context, c *Client) (*httputil.Resource[types.Transition], error)
	}{
		{
			name: "on",
			path: actionOnPath,
			call: func(ctx context.Context, c *Client) (*httputil.Resource[types.Transition], error) {
				return c.ActionOn(ctx, types.ActionRequest{Nodes: []string{"node-1"}})
			},
		},
		{
			name: "off",
			path: actionOffPath,
			call: func(ctx context.Context, c *Client) (*httputil.Resource[types.Transition], error) {
				return c.ActionOff(ctx, types.ActionRequest{Nodes: []string{"node-1"}})
			},
		},
		{
			name: "reboot",
			path: actionRebootPath,
			call: func(ctx context.Context, c *Client) (*httputil.Resource[types.Transition], error) {
				return c.ActionReboot(ctx, types.ActionRequest{Nodes: []string{"node-1"}})
			},
		},
		{
			name: "reset",
			path: actionResetPath,
			call: func(ctx context.Context, c *Client) (*httputil.Resource[types.Transition], error) {
				return c.ActionReset(ctx, types.ResetActionRequest{
					Operation: "ForceRestart",
					Nodes:     []string{"node-1"},
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, tt.path, r.URL.Path)
				respondJSON(w, http.StatusAccepted, transitionResource("t-1", types.TransitionStatePending))
			}))
			defer ts.Close()

			c := newTestClient(t, Config{BaseURL: ts.URL})
			resp, err := tt.call(context.Background(), c)
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, "t-1", resp.Metadata.ID)
		})
	}
}

func TestTriggerMappingSync(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, adminMappingSyncPath, r.URL.Path)
		respondJSON(w, http.StatusAccepted, httputil.Resource[types.MappingSyncTrigger]{
			Kind:       "MappingSyncTrigger",
			APIVersion: "power/v1",
			Metadata:   httputil.Metadata{ID: "power-mapping-sync"},
			Spec:       types.MappingSyncTrigger{Status: "accepted"},
		})
	}))
	defer ts.Close()

	c := newTestClient(t, Config{BaseURL: ts.URL})
	resp, err := c.TriggerMappingSync(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "accepted", resp.Spec.Status)
}

func TestTriggerMappingSync_Error(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		problemJSON(w, http.StatusServiceUnavailable, "mapping sync unavailable")
	}))
	defer ts.Close()

	c := newTestClient(t, Config{BaseURL: ts.URL})
	resp, err := c.TriggerMappingSync(context.Background())
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "triggering mapping sync")
}

func TestActionOn_Error(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		problemJSON(w, http.StatusBadRequest, "invalid target list")
	}))
	defer ts.Close()

	c := newTestClient(t, Config{BaseURL: ts.URL})
	resp, err := c.ActionOn(context.Background(), types.ActionRequest{Nodes: []string{"node-1"}})
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "starting on action")
}

func TestRetryAndErrors(t *testing.T) {
	t.Parallel()

	t.Run("retries on 503 and succeeds", func(t *testing.T) {
		t.Parallel()
		var attempts int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if atomic.AddInt32(&attempts, 1) < 3 {
				problemJSON(w, http.StatusServiceUnavailable, "temporarily unavailable")
				return
			}
			respondJSON(w, http.StatusOK, httputil.ResourceList[types.Transition]{
				Kind:       "TransitionList",
				APIVersion: "power/v1",
				Metadata:   httputil.ListMetadata{Total: 0, Limit: 100, Offset: 0},
				Items:      []httputil.Resource[types.Transition]{},
			})
		}))
		defer ts.Close()

		c := newTestClient(t, Config{
			BaseURL:    ts.URL,
			MaxRetries: 2,
			Timeout:    2 * time.Second,
		})
		resp, err := c.ListTransitions(context.Background(), ListTransitionsOptions{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
	})

	t.Run("returns api error after retries exhausted", func(t *testing.T) {
		t.Parallel()
		var attempts int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&attempts, 1)
			problemJSON(w, http.StatusServiceUnavailable, "still unavailable")
		}))
		defer ts.Close()

		c := newTestClient(t, Config{
			BaseURL:    ts.URL,
			MaxRetries: 1,
			Timeout:    2 * time.Second,
		})
		resp, err := c.ListTransitions(context.Background(), ListTransitionsOptions{})
		assert.Nil(t, resp)
		require.Error(t, err)
		var apiErr *baseclient.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
		assert.Equal(t, "still unavailable", apiErr.Problem.Detail)
		assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
	})

	t.Run("returns parsed problem for non-retryable status", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			problemJSON(w, http.StatusForbidden, "insufficient scope")
		}))
		defer ts.Close()

		c := newTestClient(t, Config{BaseURL: ts.URL})
		resp, err := c.CreateTransition(context.Background(), types.CreateTransitionRequest{
			Operation: "On",
			Nodes:     []string{"node-1"},
		})
		assert.Nil(t, resp)
		require.Error(t, err)
		var apiErr *baseclient.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
		assert.Equal(t, "insufficient scope", apiErr.Problem.Detail)
	})

	t.Run("returns fallback problem on plain text body", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("invalid payload"))
		}))
		defer ts.Close()

		c := newTestClient(t, Config{BaseURL: ts.URL})
		resp, err := c.CreateTransition(context.Background(), types.CreateTransitionRequest{
			Operation: "On",
			Nodes:     []string{"node-1"},
		})
		assert.Nil(t, resp)
		require.Error(t, err)
		var apiErr *baseclient.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		assert.Equal(t, "invalid payload", apiErr.Problem.Detail)
	})
}

func TestIsTransitionTerminalState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state string
		want  bool
	}{
		{state: "completed", want: true},
		{state: "failed", want: true},
		{state: "partial", want: true},
		{state: "canceled", want: true},
		{state: "RUNNING", want: false},
		{state: " pending ", want: false},
		{state: "", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.state, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsTransitionTerminalState(tt.state))
		})
	}
}
