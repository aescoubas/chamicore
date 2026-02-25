package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveToken_PrefersMCPToken(t *testing.T) {
	t.Setenv("CHAMICORE_MCP_TOKEN", "mcp-token")
	t.Setenv("CHAMICORE_TOKEN", "shared-token")

	resolved, err := ResolveToken(TokenSourceOptions{AllowCLIConfigToken: false})
	require.NoError(t, err)
	require.Equal(t, "mcp-token", resolved.Token)
	require.Equal(t, TokenSourceMCPEnv, resolved.Source)
}

func TestResolveToken_FallsBackToSharedToken(t *testing.T) {
	t.Setenv("CHAMICORE_MCP_TOKEN", "")
	t.Setenv("CHAMICORE_TOKEN", "shared-token")

	resolved, err := ResolveToken(TokenSourceOptions{AllowCLIConfigToken: false})
	require.NoError(t, err)
	require.Equal(t, "shared-token", resolved.Token)
	require.Equal(t, TokenSourceSharedEnv, resolved.Source)
}

func TestResolveToken_UsesCLIConfigWhenAllowed(t *testing.T) {
	t.Setenv("CHAMICORE_MCP_TOKEN", "")
	t.Setenv("CHAMICORE_TOKEN", "")

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := []byte("auth:\n  token: cli-token\n")
	require.NoError(t, writeFile(configPath, content))

	resolved, err := ResolveToken(TokenSourceOptions{
		AllowCLIConfigToken: true,
		CLIConfigPath:       configPath,
	})
	require.NoError(t, err)
	require.Equal(t, "cli-token", resolved.Token)
	require.Equal(t, TokenSourceCLIConfig, resolved.Source)
}

func TestResolveToken_IgnoresCLIConfigWhenNotAllowed(t *testing.T) {
	t.Setenv("CHAMICORE_MCP_TOKEN", "")
	t.Setenv("CHAMICORE_TOKEN", "")

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := []byte("auth:\n  token: cli-token\n")
	require.NoError(t, writeFile(configPath, content))

	resolved, err := ResolveToken(TokenSourceOptions{
		AllowCLIConfigToken: false,
		CLIConfigPath:       configPath,
	})
	require.NoError(t, err)
	require.Equal(t, "", resolved.Token)
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
