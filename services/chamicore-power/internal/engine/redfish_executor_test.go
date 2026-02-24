package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedredfish "git.cscs.ch/openchami/chamicore-lib/redfish"
)

func TestSystemPathResolver_ResolvesByNodeIDAndCaches(t *testing.T) {
	t.Parallel()

	var systemsCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			atomic.AddInt32(&systemsCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/node-b"},
					{"@odata.id": "/redfish/v1/Systems/node-a"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	resolver := NewSystemPathResolver()
	client := sharedredfish.New(sharedredfish.Config{MaxAttempts: 1})

	first, err := resolver.Resolve(context.Background(), client, server.URL, "node-a", sharedredfish.Credential{})
	require.NoError(t, err)
	assert.Equal(t, "/redfish/v1/Systems/node-a", first)

	second, err := resolver.Resolve(context.Background(), client, server.URL, "node-a", sharedredfish.Credential{})
	require.NoError(t, err)
	assert.Equal(t, "/redfish/v1/Systems/node-a", second)
	assert.Equal(t, int32(1), atomic.LoadInt32(&systemsCalls))
}

func TestRedfishExecutor_ExecutePowerAction_Success(t *testing.T) {
	t.Parallel()

	var resetCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Systems":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/node-a"},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/redfish/v1/Systems/node-a/Actions/ComputerSystem.Reset":
			atomic.AddInt32(&resetCalls, 1)
			var payload map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			assert.Equal(t, "GracefulRestart", payload["ResetType"])
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	executor := NewRedfishExecutor(
		sharedredfish.Config{MaxAttempts: 1},
		EmptyCredentialResolver{},
		NewSystemPathResolver(),
	)

	err := executor.ExecutePowerAction(context.Background(), ExecutionRequest{
		Endpoint:  server.URL,
		NodeID:    "node-a",
		Operation: sharedredfish.ResetOperationGracefulRestart,
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&resetCalls))
}

func TestRedfishStateReader_ReadPowerState_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/node-a"}},
			})
		case "/redfish/v1/Systems/node-a":
			_ = json.NewEncoder(w).Encode(map[string]any{"PowerState": "On"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	reader := NewRedfishStateReader(
		sharedredfish.Config{MaxAttempts: 1},
		EmptyCredentialResolver{},
		NewSystemPathResolver(),
	)

	powerState, err := reader.ReadPowerState(context.Background(), ExecutionRequest{
		Endpoint: server.URL,
		NodeID:   "node-a",
	})
	require.NoError(t, err)
	assert.Equal(t, "On", powerState)
}

func TestClassifyExecutionError_RetryableAndNonRetryable(t *testing.T) {
	t.Parallel()

	retryableErr := classifyExecutionError(fmt.Errorf("unexpected status 503: service unavailable"))
	assert.True(t, IsRetryable(retryableErr))

	nonRetryableErr := classifyExecutionError(fmt.Errorf("unexpected status 401: unauthorized"))
	assert.False(t, IsRetryable(nonRetryableErr))
}
