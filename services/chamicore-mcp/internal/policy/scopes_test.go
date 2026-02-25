package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequireScopes_AllowsWhenNoRequiredScopes(t *testing.T) {
	require.NoError(t, RequireScopes("tool.read", nil, nil))
}

func TestRequireScopes_AllowsAdmin(t *testing.T) {
	err := RequireScopes("tool.write", []string{"write:power"}, []string{"admin"})
	require.NoError(t, err)
}

func TestRequireScopes_AllowsWhenAllScopesPresent(t *testing.T) {
	err := RequireScopes("tool.write", []string{"write:power", "write:groups"}, []string{"write:groups", "write:power"})
	require.NoError(t, err)
}

func TestRequireScopes_DeniesWhenMissingScope(t *testing.T) {
	err := RequireScopes("tool.write", []string{"write:power"}, []string{"read:power"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing required scope(s): write:power")
	require.Contains(t, err.Error(), "granted: read:power")
}

func TestRequireScopes_DeduplicatesAndTrimsScopes(t *testing.T) {
	err := RequireScopes("tool.write", []string{" write:power ", "write:power"}, []string{"  write:power"})
	require.NoError(t, err)
}
