package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewGuard_DefaultReadOnly(t *testing.T) {
	guard, err := NewGuard("", false)
	require.NoError(t, err)
	require.Equal(t, ModeReadOnly, guard.Mode())
}

func TestNewGuard_ReadWriteRequiresEnableFlag(t *testing.T) {
	_, err := NewGuard(ModeReadWrite, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "CHAMICORE_MCP_ENABLE_WRITE=true")
}

func TestNewGuard_ReadWriteEnabled(t *testing.T) {
	guard, err := NewGuard(ModeReadWrite, true)
	require.NoError(t, err)
	require.Equal(t, ModeReadWrite, guard.Mode())
}

func TestAuthorizeTool_ReadOnlyDeniesWrite(t *testing.T) {
	guard, err := NewGuard(ModeReadOnly, false)
	require.NoError(t, err)

	require.NoError(t, guard.AuthorizeTool("x.read", "read"))
	err = guard.AuthorizeTool("x.write", "write")
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires read-write mode")
}

func TestAuthorizeTool_UnknownCapability(t *testing.T) {
	guard, err := NewGuard(ModeReadOnly, false)
	require.NoError(t, err)

	err = guard.AuthorizeTool("x.unknown", "admin")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown capability")
}
