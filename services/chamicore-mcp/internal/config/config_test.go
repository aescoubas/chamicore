package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("CHAMICORE_MCP_LISTEN_ADDR", "")
	t.Setenv("CHAMICORE_MCP_LOG_LEVEL", "")
	t.Setenv("CHAMICORE_MCP_TRANSPORT", "")
	t.Setenv("CHAMICORE_MCP_MODE", "")
	t.Setenv("CHAMICORE_MCP_ENABLE_WRITE", "")
	t.Setenv("CHAMICORE_MCP_ALLOW_CLI_CONFIG_TOKEN", "")
	t.Setenv("CHAMICORE_MCP_CLI_CONFIG_PATH", "")
	t.Setenv("CHAMICORE_MCP_AUTH_URL", "")
	t.Setenv("CHAMICORE_MCP_SMD_URL", "")
	t.Setenv("CHAMICORE_MCP_BSS_URL", "")
	t.Setenv("CHAMICORE_MCP_CLOUD_INIT_URL", "")
	t.Setenv("CHAMICORE_MCP_DISCOVERY_URL", "")
	t.Setenv("CHAMICORE_MCP_POWER_URL", "")
	t.Setenv("CHAMICORE_MCP_METRICS_ENABLED", "")
	t.Setenv("CHAMICORE_MCP_TRACES_ENABLED", "")
	t.Setenv("CHAMICORE_MCP_DEV_MODE", "")

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, defaultListenAddr, cfg.ListenAddr)
	require.Equal(t, "info", cfg.LogLevel)
	require.Equal(t, TransportStdio, cfg.Transport)
	require.Equal(t, ModeReadOnly, cfg.Mode)
	require.False(t, cfg.EnableWrite)
	require.False(t, cfg.AllowCLIConfigToken)
	require.Equal(t, "~/.chamicore/config.yaml", cfg.CLIConfigPath)
	require.Equal(t, defaultAuthURL, cfg.AuthURL)
	require.Equal(t, defaultSMDURL, cfg.SMDURL)
	require.Equal(t, defaultBSSURL, cfg.BSSURL)
	require.Equal(t, defaultCloudURL, cfg.CloudInitURL)
	require.Equal(t, defaultDiscURL, cfg.DiscoveryURL)
	require.Equal(t, defaultPowerURL, cfg.PowerURL)
	require.True(t, cfg.MetricsEnabled)
	require.False(t, cfg.TracesEnabled)
	require.False(t, cfg.DevMode)
}

func TestLoad_InvalidTransport(t *testing.T) {
	t.Setenv("CHAMICORE_MCP_TRANSPORT", "udp")

	_, err := Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid CHAMICORE_MCP_TRANSPORT")
}

func TestLoad_InvalidMode(t *testing.T) {
	t.Setenv("CHAMICORE_MCP_MODE", "full-access")

	_, err := Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid CHAMICORE_MCP_MODE")
}
