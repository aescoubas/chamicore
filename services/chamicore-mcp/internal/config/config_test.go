package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("CHAMICORE_MCP_LISTEN_ADDR", "")
	t.Setenv("CHAMICORE_MCP_LOG_LEVEL", "")
	t.Setenv("CHAMICORE_MCP_TRANSPORT", "")
	t.Setenv("CHAMICORE_MCP_METRICS_ENABLED", "")
	t.Setenv("CHAMICORE_MCP_TRACES_ENABLED", "")
	t.Setenv("CHAMICORE_MCP_DEV_MODE", "")

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, defaultListenAddr, cfg.ListenAddr)
	require.Equal(t, "info", cfg.LogLevel)
	require.Equal(t, TransportStdio, cfg.Transport)
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
