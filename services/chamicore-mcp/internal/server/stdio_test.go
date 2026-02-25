package server

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
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

	err := RunStdio(context.Background(), in, out, registry, "test-version", zerolog.Nop())
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

	err := RunStdio(context.Background(), in, out, registry, "test-version", zerolog.Nop())
	require.NoError(t, err)

	var resp rpcResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, rpcCodeMethodNotFound, resp.Error.Code)
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
