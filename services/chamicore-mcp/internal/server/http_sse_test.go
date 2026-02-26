package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"git.cscs.ch/openchami/chamicore-mcp/internal/config"
	"git.cscs.ch/openchami/chamicore-mcp/internal/policy"
)

func TestHTTPServer_RoutesAndSSE(t *testing.T) {
	registry := mustTestRegistry(t)
	cfg := config.Config{
		ListenAddr:     ":27774",
		Transport:      config.TransportHTTP,
		MetricsEnabled: true,
	}

	srv := NewHTTPServer(
		cfg,
		"v-test",
		"c-test",
		"b-test",
		[]byte("tools: []"),
		registry,
		mustReadOnlyGuard(t),
		mustSessionAuthForHTTP(t),
		nil,
		zerolog.Nop(),
	)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	reqBody := bytes.NewBufferString(`{}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp/v1/initialize", reqBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var initPayload map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&initPayload))
	_ = resp.Body.Close()
	require.Equal(t, defaultProtocolVersion, initPayload["protocolVersion"])

	resp, err = http.Get(ts.URL + "/mcp/v1/tools")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var toolsPayload map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&toolsPayload))
	_ = resp.Body.Close()
	tools, ok := toolsPayload["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)

	sseReqBody := bytes.NewBufferString(`{"name":"cluster.health_summary","arguments":{"foo":"bar"}}`)
	sseReq, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp/v1/tools/call/sse", sseReqBody)
	require.NoError(t, err)
	sseReq.Header.Set("Content-Type", "application/json")
	sseReq.Header.Set("Authorization", "Bearer http-session-token")
	sseResp, err := http.DefaultClient.Do(sseReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, sseResp.StatusCode)
	require.Contains(t, sseResp.Header.Get("Content-Type"), "text/event-stream")
	sseBody, err := io.ReadAll(sseResp.Body)
	require.NoError(t, err)
	_ = sseResp.Body.Close()
	content := string(sseBody)
	require.Contains(t, content, "event: accepted")
	require.Contains(t, content, "event: result")
	require.Contains(t, content, "event: done")
	require.Contains(t, content, "cluster.health_summary")
}

func TestHTTPServer_CallToolUnknown(t *testing.T) {
	registry := mustTestRegistry(t)
	cfg := config.Config{
		ListenAddr: ":27774",
		Transport:  config.TransportHTTP,
	}

	srv := NewHTTPServer(
		cfg,
		"v-test",
		"c-test",
		"b-test",
		[]byte("tools: []"),
		registry,
		mustReadOnlyGuard(t),
		mustSessionAuthForHTTP(t),
		nil,
		zerolog.Nop(),
	)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	reqBody := bytes.NewBufferString(`{"name":"nope"}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp/v1/tools/call", reqBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer http-session-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Contains(t, strings.ToLower(string(body)), "unknown tool")
}

func TestHTTPServer_ReadOnlyModeDeniesWriteTool(t *testing.T) {
	registry, err := NewToolRegistry([]byte(`
version: "1.0"
service: "chamicore-mcp"
apiVersion: "mcp/v1"
tools:
  - name: bss.bootparams.upsert
    capability: write
    inputSchema:
      type: object
`))
	require.NoError(t, err)

	cfg := config.Config{
		ListenAddr: ":27774",
		Transport:  config.TransportHTTP,
	}
	srv := NewHTTPServer(
		cfg,
		"v-test",
		"c-test",
		"b-test",
		[]byte("tools: []"),
		registry,
		mustReadOnlyGuard(t),
		mustSessionAuthForHTTP(t),
		nil,
		zerolog.Nop(),
	)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	reqBody := bytes.NewBufferString(`{"name":"bss.bootparams.upsert","arguments":{"component_id":"x0","kernel":"k","initrd":"i"}}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp/v1/tools/call", reqBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer http-session-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Contains(t, strings.ToLower(string(body)), "requires read-write mode")
}

func TestHTTPServer_DestructiveToolRequiresConfirmation(t *testing.T) {
	registry, err := NewToolRegistry([]byte(`
version: "1.0"
service: "chamicore-mcp"
apiVersion: "mcp/v1"
tools:
  - name: power.transitions.create
    capability: write
    inputSchema:
      type: object
`))
	require.NoError(t, err)

	cfg := config.Config{
		ListenAddr: ":27774",
		Transport:  config.TransportHTTP,
	}
	srv := NewHTTPServer(
		cfg,
		"v-test",
		"c-test",
		"b-test",
		[]byte("tools: []"),
		registry,
		mustReadWriteGuard(t),
		mustSessionAuthForHTTP(t),
		nil,
		zerolog.Nop(),
	)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	reqBody := bytes.NewBufferString(`{"name":"power.transitions.create","arguments":{"operation":"ForceOff","nodes":["x0"]}}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp/v1/tools/call", reqBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer http-session-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Contains(t, string(body), "requires confirm=true")
}

func TestHTTPServer_DeniesWhenTokenMissing(t *testing.T) {
	registry := mustTestRegistry(t)
	cfg := config.Config{
		ListenAddr: ":27774",
		Transport:  config.TransportHTTP,
	}

	srv := NewHTTPServer(
		cfg,
		"v-test",
		"c-test",
		"b-test",
		[]byte("tools: []"),
		registry,
		mustReadOnlyGuard(t),
		mustSessionAuthForHTTP(t),
		nil,
		zerolog.Nop(),
	)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	reqBody := bytes.NewBufferString(`{"name":"cluster.health_summary","arguments":{}}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp/v1/tools/call", reqBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Contains(t, strings.ToLower(string(body)), "authorization")
}

func TestHTTPServer_DeniesWhenScopeMissing(t *testing.T) {
	registry, err := NewToolRegistry([]byte(`
version: "1.0"
service: "chamicore-mcp"
apiVersion: "mcp/v1"
tools:
  - name: cluster.health_summary
    capability: read
    requiredScopes: [admin]
    inputSchema:
      type: object
`))
	require.NoError(t, err)

	cfg := config.Config{
		ListenAddr: ":27774",
		Transport:  config.TransportHTTP,
	}
	token := testJWTToken(t, "http-agent", []string{"read:components"})

	srv := NewHTTPServer(
		cfg,
		"v-test",
		"c-test",
		"b-test",
		[]byte("tools: []"),
		registry,
		mustReadOnlyGuard(t),
		NewTokenSessionAuthenticator(token),
		nil,
		zerolog.Nop(),
	)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	reqBody := bytes.NewBufferString(`{"name":"cluster.health_summary","arguments":{}}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp/v1/tools/call", reqBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Contains(t, string(body), "missing required scope(s): admin")
}

func TestHTTPServer_AuditCompletionLoggedOnceOnHTTPError(t *testing.T) {
	registry := mustTestRegistry(t)
	cfg := config.Config{
		ListenAddr: ":27774",
		Transport:  config.TransportHTTP,
	}
	logs := &bytes.Buffer{}

	srv := NewHTTPServer(
		cfg,
		"v-test",
		"c-test",
		"b-test",
		[]byte("tools: []"),
		registry,
		mustReadOnlyGuard(t),
		mustSessionAuthForHTTP(t),
		nil,
		zerolog.New(logs),
	)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	reqBody := bytes.NewBufferString(`{"name":"nope","arguments":{"token":"super-secret"}}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp/v1/tools/call", reqBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer http-session-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	_ = resp.Body.Close()

	events := httpAuditEventsFromLogs(t, logs.String())
	require.Len(t, events, 1)
	require.Equal(t, "http", events[0]["transport"])
	require.Equal(t, "nope", events[0]["tool"])
	require.Equal(t, "error", events[0]["result"])
	require.Contains(t, events[0]["error_detail"], "unknown tool")
}

func TestHTTPServer_AuditCompletionLoggedOnceOnSSESuccess(t *testing.T) {
	registry := mustTestRegistry(t)
	cfg := config.Config{
		ListenAddr: ":27774",
		Transport:  config.TransportHTTP,
	}
	logs := &bytes.Buffer{}

	srv := NewHTTPServer(
		cfg,
		"v-test",
		"c-test",
		"b-test",
		[]byte("tools: []"),
		registry,
		mustReadOnlyGuard(t),
		mustSessionAuthForHTTP(t),
		nil,
		zerolog.New(logs),
	)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	reqBody := bytes.NewBufferString(`{"name":"cluster.health_summary","arguments":{"nodes":["x0"]}}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp/v1/tools/call/sse", reqBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer http-session-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	events := httpAuditEventsFromLogs(t, logs.String())
	require.Len(t, events, 1)
	require.Equal(t, "http-sse", events[0]["transport"])
	require.Equal(t, "cluster.health_summary", events[0]["tool"])
	require.Equal(t, "success", events[0]["result"])
}

func mustReadOnlyGuard(t *testing.T) *policy.Guard {
	t.Helper()
	guard, err := policy.NewGuard(policy.ModeReadOnly, false)
	require.NoError(t, err)
	return guard
}

func mustReadWriteGuard(t *testing.T) *policy.Guard {
	t.Helper()
	guard, err := policy.NewGuard(policy.ModeReadWrite, true)
	require.NoError(t, err)
	return guard
}

func mustSessionAuthForHTTP(t *testing.T) SessionAuthenticator {
	t.Helper()
	return NewTokenSessionAuthenticator("http-session-token")
}

func httpAuditEventsFromLogs(t *testing.T, payload string) []map[string]string {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(payload), "\n")
	events := make([]map[string]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var decoded map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &decoded))
		if decoded["event"] != "mcp.tool_call.completed" {
			continue
		}
		entry := map[string]string{}
		for key, value := range decoded {
			if asString, ok := value.(string); ok {
				entry[key] = asString
			}
		}
		events = append(events, entry)
	}
	return events
}
