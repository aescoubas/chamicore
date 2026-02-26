package server

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"git.cscs.ch/openchami/chamicore-mcp/internal/policy"
)

func TestRunStdio_InitializeListAndCall(t *testing.T) {
	registry := mustTestRegistry(t)

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"cluster.health_summary","arguments":{"limit":5}}}`,
		"",
	}, "\n")
	in := bytes.NewBufferString(input)
	out := &bytes.Buffer{}

	err := RunStdio(context.Background(), in, out, registry, mustReadOnlyGuardForStdio(t), mustSessionAuthForStdio(t), nil, "test-version", zerolog.Nop())
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3)

	var initResp rpcResponse
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &initResp))
	require.Nil(t, initResp.Error)
	initMap, ok := initResp.Result.(map[string]any)
	require.True(t, ok)
	require.Equal(t, defaultProtocolVersion, initMap["protocolVersion"])

	var listResp rpcResponse
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &listResp))
	require.Nil(t, listResp.Error)
	listMap, ok := listResp.Result.(map[string]any)
	require.True(t, ok)
	tools, ok := listMap["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)

	var callResp rpcResponse
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &callResp))
	require.Nil(t, callResp.Error)
	callMap, ok := callResp.Result.(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, callMap["isError"])
}

func TestRunStdio_UnknownMethod(t *testing.T) {
	registry := mustTestRegistry(t)
	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"nope","params":{}}` + "\n")
	out := &bytes.Buffer{}

	err := RunStdio(context.Background(), in, out, registry, mustReadOnlyGuardForStdio(t), mustSessionAuthForStdio(t), nil, "test-version", zerolog.Nop())
	require.NoError(t, err)

	var resp rpcResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, rpcCodeMethodNotFound, resp.Error.Code)
}

func TestRunStdio_ReadOnlyModeDeniesWriteTool(t *testing.T) {
	registry, err := NewToolRegistry([]byte(`
version: "1.0"
service: "chamicore-mcp"
apiVersion: "mcp/v1"
tools:
  - name: smd.groups.members.add
    capability: write
    inputSchema:
      type: object
`))
	require.NoError(t, err)

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smd.groups.members.add","arguments":{"label":"g","members":["x0"]}}}` + "\n")
	out := &bytes.Buffer{}

	runErr := RunStdio(context.Background(), in, out, registry, mustReadOnlyGuardForStdio(t), mustSessionAuthForStdio(t), nil, "test-version", zerolog.Nop())
	require.NoError(t, runErr)

	var resp rpcResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, rpcCodeInvalidParams, resp.Error.Code)
	require.Contains(t, resp.Error.Message, "requires read-write mode")
}

func TestRunStdio_DestructiveToolRequiresConfirmation(t *testing.T) {
	registry, err := NewToolRegistry([]byte(`
version: "1.0"
service: "chamicore-mcp"
apiVersion: "mcp/v1"
tools:
  - name: bss.bootparams.delete
    capability: write
    confirmationRequired: true
    inputSchema:
      type: object
`))
	require.NoError(t, err)

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bss.bootparams.delete","arguments":{"component_id":"x0"}}}` + "\n")
	out := &bytes.Buffer{}

	runErr := RunStdio(context.Background(), in, out, registry, mustReadWriteGuardForStdio(t), mustSessionAuthForStdio(t), nil, "test-version", zerolog.Nop())
	require.NoError(t, runErr)

	var resp rpcResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, rpcCodeInvalidParams, resp.Error.Code)
	require.Contains(t, resp.Error.Message, "requires confirm=true")
}

func TestRunStdio_DestructiveToolWithConfirmationAllowed(t *testing.T) {
	registry, err := NewToolRegistry([]byte(`
version: "1.0"
service: "chamicore-mcp"
apiVersion: "mcp/v1"
tools:
  - name: bss.bootparams.delete
    capability: write
    confirmationRequired: true
    inputSchema:
      type: object
`))
	require.NoError(t, err)

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bss.bootparams.delete","arguments":{"component_id":"x0","confirm":true}}}` + "\n")
	out := &bytes.Buffer{}

	runErr := RunStdio(context.Background(), in, out, registry, mustReadWriteGuardForStdio(t), mustSessionAuthForStdio(t), nil, "test-version", zerolog.Nop())
	require.NoError(t, runErr)

	var resp rpcResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	require.Nil(t, resp.Error)
}

func TestRunStdio_DeniesWhenSessionTokenMissing(t *testing.T) {
	registry := mustTestRegistry(t)

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"cluster.health_summary","arguments":{}}}` + "\n")
	out := &bytes.Buffer{}

	runErr := RunStdio(context.Background(), in, out, registry, mustReadOnlyGuardForStdio(t), NewTokenSessionAuthenticator(""), nil, "test-version", zerolog.Nop())
	require.NoError(t, runErr)

	var resp rpcResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, rpcCodeInvalidParams, resp.Error.Code)
	require.Contains(t, resp.Error.Message, "mcp session token is not configured")
}

func TestRunStdio_DeniesWhenScopeMissing(t *testing.T) {
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

	token := testJWTToken(t, "cli-agent", []string{"read:components"})
	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"cluster.health_summary","arguments":{}}}` + "\n")
	out := &bytes.Buffer{}

	runErr := RunStdio(context.Background(), in, out, registry, mustReadOnlyGuardForStdio(t), NewTokenSessionAuthenticator(token), nil, "test-version", zerolog.Nop())
	require.NoError(t, runErr)

	var resp rpcResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, rpcCodeInvalidParams, resp.Error.Code)
	require.Contains(t, resp.Error.Message, "missing required scope(s): admin")
}

func TestRunStdio_AuditCompletionLoggedOnceOnSuccess(t *testing.T) {
	registry := mustTestRegistry(t)

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"cluster.health_summary","arguments":{"nodes":["x0"]}}}` + "\n")
	out := &bytes.Buffer{}
	logs := &bytes.Buffer{}

	runErr := RunStdio(
		context.Background(),
		in,
		out,
		registry,
		mustReadOnlyGuardForStdio(t),
		mustSessionAuthForStdio(t),
		nil,
		"test-version",
		zerolog.New(logs),
	)
	require.NoError(t, runErr)

	events := stdioAuditEventsFromLogs(t, logs.String())
	require.Len(t, events, 1)
	require.Equal(t, "cluster.health_summary", events[0]["tool"])
	require.Equal(t, "success", events[0]["result"])
	require.Equal(t, "42", events[0]["request_id"])
	require.Equal(t, "stdio", events[0]["session_id"])
}

func TestRunStdio_AuditCompletionLoggedOnceOnError(t *testing.T) {
	registry := mustTestRegistry(t)

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":"req-unknown","method":"tools/call","params":{"name":"nope","arguments":{"token":"secret"}}}` + "\n")
	out := &bytes.Buffer{}
	logs := &bytes.Buffer{}

	runErr := RunStdio(
		context.Background(),
		in,
		out,
		registry,
		mustReadOnlyGuardForStdio(t),
		mustSessionAuthForStdio(t),
		nil,
		"test-version",
		zerolog.New(logs),
	)
	require.NoError(t, runErr)

	events := stdioAuditEventsFromLogs(t, logs.String())
	require.Len(t, events, 1)
	require.Equal(t, "nope", events[0]["tool"])
	require.Equal(t, "error", events[0]["result"])
	require.Contains(t, events[0]["error_detail"], "unknown tool")
}

func mustTestRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	registry, err := NewToolRegistry([]byte(`
version: "1.0"
service: "chamicore-mcp"
apiVersion: "mcp/v1"
tools:
  - name: cluster.health_summary
    capability: read
    inputSchema:
      type: object
`))
	require.NoError(t, err)
	return registry
}

func mustReadOnlyGuardForStdio(t *testing.T) *policy.Guard {
	t.Helper()
	guard, err := policy.NewGuard(policy.ModeReadOnly, false)
	require.NoError(t, err)
	return guard
}

func mustReadWriteGuardForStdio(t *testing.T) *policy.Guard {
	t.Helper()
	guard, err := policy.NewGuard(policy.ModeReadWrite, true)
	require.NoError(t, err)
	return guard
}

func mustSessionAuthForStdio(t *testing.T) SessionAuthenticator {
	t.Helper()
	return NewTokenSessionAuthenticator("stdio-session-token")
}

func stdioAuditEventsFromLogs(t *testing.T, payload string) []map[string]string {
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
