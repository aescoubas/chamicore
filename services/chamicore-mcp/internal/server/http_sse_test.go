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

	srv := NewHTTPServer(cfg, "v-test", "c-test", "b-test", []byte("tools: []"), registry, mustReadOnlyGuard(t), zerolog.Nop())
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

	srv := NewHTTPServer(cfg, "v-test", "c-test", "b-test", []byte("tools: []"), registry, mustReadOnlyGuard(t), zerolog.Nop())
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	reqBody := bytes.NewBufferString(`{"name":"nope"}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp/v1/tools/call", reqBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
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
	srv := NewHTTPServer(cfg, "v-test", "c-test", "b-test", []byte("tools: []"), registry, mustReadOnlyGuard(t), zerolog.Nop())
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	reqBody := bytes.NewBufferString(`{"name":"bss.bootparams.upsert","arguments":{"component_id":"x0","kernel":"k","initrd":"i"}}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp/v1/tools/call", reqBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Contains(t, strings.ToLower(string(body)), "requires read-write mode")
}

func mustReadOnlyGuard(t *testing.T) *policy.Guard {
	t.Helper()
	guard, err := policy.NewGuard(policy.ModeReadOnly, false)
	require.NoError(t, err)
	return guard
}
