package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("CHAMICORE_POWER_LISTEN_ADDR", "")
	t.Setenv("CHAMICORE_POWER_DB_DSN", "")
	t.Setenv("CHAMICORE_POWER_LOG_LEVEL", "")
	t.Setenv("CHAMICORE_POWER_DEV_MODE", "")
	t.Setenv("CHAMICORE_POWER_JWKS_URL", "")
	t.Setenv("CHAMICORE_INTERNAL_TOKEN", "")
	t.Setenv("CHAMICORE_POWER_METRICS_ENABLED", "")
	t.Setenv("CHAMICORE_POWER_TRACES_ENABLED", "")
	t.Setenv("CHAMICORE_POWER_PROMETHEUS_ADDR", "")
	t.Setenv("CHAMICORE_POWER_BULK_MAX_NODES", "")
	t.Setenv("CHAMICORE_POWER_RETRY_ATTEMPTS", "")
	t.Setenv("CHAMICORE_POWER_RETRY_BACKOFF_BASE", "")
	t.Setenv("CHAMICORE_POWER_RETRY_BACKOFF_MAX", "")
	t.Setenv("CHAMICORE_POWER_TRANSITION_DEADLINE", "")
	t.Setenv("CHAMICORE_POWER_VERIFICATION_WINDOW", "")
	t.Setenv("CHAMICORE_POWER_VERIFICATION_POLL_INTERVAL", "")
	t.Setenv("CHAMICORE_POWER_GLOBAL_CONCURRENCY", "")
	t.Setenv("CHAMICORE_POWER_PER_BMC_CONCURRENCY", "")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, defaultListenAddr, cfg.ListenAddr)
	assert.Equal(t, defaultDSN, cfg.DBDSN)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, defaultPrometheusAddr, cfg.PrometheusAddr)
	assert.Equal(t, defaultBulkMaxNodes, cfg.BulkMaxNodes)
	assert.Equal(t, defaultRetryAttempts, cfg.RetryAttempts)
	assert.Equal(t, defaultRetryBackoffBase, cfg.RetryBackoffBase)
	assert.Equal(t, defaultRetryBackoffMax, cfg.RetryBackoffMax)
	assert.Equal(t, defaultTransitionTimeout, cfg.TransitionDeadline)
	assert.Equal(t, defaultVerifyWindow, cfg.VerificationWindow)
	assert.Equal(t, defaultVerifyPoll, cfg.VerificationPoll)
	assert.Equal(t, defaultGlobalWorkers, cfg.GlobalConcurrency)
	assert.Equal(t, defaultPerBMCWorkers, cfg.PerBMCConcurrency)
}

func TestLoad_Normalization(t *testing.T) {
	t.Setenv("CHAMICORE_POWER_DB_DSN", "postgres://example")
	t.Setenv("CHAMICORE_POWER_RETRY_BACKOFF_BASE", "2s")
	t.Setenv("CHAMICORE_POWER_RETRY_BACKOFF_MAX", "500ms")
	t.Setenv("CHAMICORE_POWER_VERIFICATION_WINDOW", "20s")
	t.Setenv("CHAMICORE_POWER_VERIFICATION_POLL_INTERVAL", "30s")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 2*time.Second, cfg.RetryBackoffBase)
	assert.Equal(t, 2*time.Second, cfg.RetryBackoffMax)
	assert.Equal(t, 20*time.Second, cfg.VerificationWindow)
	assert.Equal(t, 20*time.Second, cfg.VerificationPoll)
}

func TestLoad_InvalidOrZeroUsesDefaults(t *testing.T) {
	t.Setenv("CHAMICORE_POWER_DB_DSN", "postgres://example")
	t.Setenv("CHAMICORE_POWER_BULK_MAX_NODES", "0")
	t.Setenv("CHAMICORE_POWER_RETRY_ATTEMPTS", "-1")
	t.Setenv("CHAMICORE_POWER_TRANSITION_DEADLINE", "not-a-duration")
	t.Setenv("CHAMICORE_POWER_GLOBAL_CONCURRENCY", "invalid")
	t.Setenv("CHAMICORE_POWER_PER_BMC_CONCURRENCY", "0")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, defaultBulkMaxNodes, cfg.BulkMaxNodes)
	assert.Equal(t, defaultRetryAttempts, cfg.RetryAttempts)
	assert.Equal(t, defaultTransitionTimeout, cfg.TransitionDeadline)
	assert.Equal(t, defaultGlobalWorkers, cfg.GlobalConcurrency)
	assert.Equal(t, defaultPerBMCWorkers, cfg.PerBMCConcurrency)
}
