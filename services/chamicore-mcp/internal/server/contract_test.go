package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewToolRegistry_Success(t *testing.T) {
	contract := []byte(`
version: "1.0"
service: "chamicore-mcp"
apiVersion: "mcp/v1"
tools:
  - name: a.read
    capability: read
    inputSchema:
      type: object
`)
	registry, err := NewToolRegistry(contract)
	require.NoError(t, err)
	require.Len(t, registry.List(), 1)

	tool, ok := registry.Lookup("a.read")
	require.True(t, ok)
	require.Equal(t, "read", tool.Capability)
}

func TestNewToolRegistry_DuplicateName(t *testing.T) {
	contract := []byte(`
version: "1.0"
service: "chamicore-mcp"
apiVersion: "mcp/v1"
tools:
  - name: same
    capability: read
  - name: same
    capability: write
`)
	_, err := NewToolRegistry(contract)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate tool")
}

func TestNewToolRegistry_Empty(t *testing.T) {
	contract := []byte(`
version: "1.0"
service: "chamicore-mcp"
apiVersion: "mcp/v1"
tools: []
`)
	_, err := NewToolRegistry(contract)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no tools")
}
