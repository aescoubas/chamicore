// Package config loads power-service configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddr        = ":27775"
	defaultDSN               = "postgres://chamicore:chamicore@localhost:5432/chamicore?sslmode=disable&search_path=power"
	defaultPrometheusAddr    = ":9090"
	defaultBulkMaxNodes      = 20
	defaultRetryAttempts     = 3
	defaultRetryBackoffBase  = 250 * time.Millisecond
	defaultRetryBackoffMax   = 5 * time.Second
	defaultTransitionTimeout = 90 * time.Second
	defaultVerifyWindow      = 90 * time.Second
	defaultVerifyPoll        = 2 * time.Second
	defaultGlobalWorkers     = 20
	defaultPerBMCWorkers     = 1
)

// Config holds service configuration values.
type Config struct {
	ListenAddr     string
	DBDSN          string
	LogLevel       string
	JWKSURL        string
	InternalToken  string
	PrometheusAddr string

	DevMode        bool
	MetricsEnabled bool
	TracesEnabled  bool

	BulkMaxNodes       int
	RetryAttempts      int
	RetryBackoffBase   time.Duration
	RetryBackoffMax    time.Duration
	TransitionDeadline time.Duration
	VerificationWindow time.Duration
	VerificationPoll   time.Duration
	GlobalConcurrency  int
	PerBMCConcurrency  int
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	cfg := Config{
		ListenAddr:         envOrDefault("CHAMICORE_POWER_LISTEN_ADDR", defaultListenAddr),
		DBDSN:              envOrDefault("CHAMICORE_POWER_DB_DSN", defaultDSN),
		LogLevel:           strings.ToLower(envOrDefault("CHAMICORE_POWER_LOG_LEVEL", "info")),
		DevMode:            envBool("CHAMICORE_POWER_DEV_MODE", false),
		JWKSURL:            envOrDefault("CHAMICORE_POWER_JWKS_URL", ""),
		InternalToken:      envOrDefault("CHAMICORE_INTERNAL_TOKEN", ""),
		MetricsEnabled:     envBool("CHAMICORE_POWER_METRICS_ENABLED", true),
		TracesEnabled:      envBool("CHAMICORE_POWER_TRACES_ENABLED", false),
		PrometheusAddr:     envOrDefault("CHAMICORE_POWER_PROMETHEUS_ADDR", defaultPrometheusAddr),
		BulkMaxNodes:       envPositiveInt("CHAMICORE_POWER_BULK_MAX_NODES", defaultBulkMaxNodes),
		RetryAttempts:      envPositiveInt("CHAMICORE_POWER_RETRY_ATTEMPTS", defaultRetryAttempts),
		RetryBackoffBase:   envPositiveDuration("CHAMICORE_POWER_RETRY_BACKOFF_BASE", defaultRetryBackoffBase),
		RetryBackoffMax:    envPositiveDuration("CHAMICORE_POWER_RETRY_BACKOFF_MAX", defaultRetryBackoffMax),
		TransitionDeadline: envPositiveDuration("CHAMICORE_POWER_TRANSITION_DEADLINE", defaultTransitionTimeout),
		VerificationWindow: envPositiveDuration("CHAMICORE_POWER_VERIFICATION_WINDOW", defaultVerifyWindow),
		VerificationPoll:   envPositiveDuration("CHAMICORE_POWER_VERIFICATION_POLL_INTERVAL", defaultVerifyPoll),
		GlobalConcurrency:  envPositiveInt("CHAMICORE_POWER_GLOBAL_CONCURRENCY", defaultGlobalWorkers),
		PerBMCConcurrency:  envPositiveInt("CHAMICORE_POWER_PER_BMC_CONCURRENCY", defaultPerBMCWorkers),
	}

	if strings.TrimSpace(cfg.DBDSN) == "" {
		return Config{}, fmt.Errorf("CHAMICORE_POWER_DB_DSN is required")
	}
	if cfg.RetryBackoffMax < cfg.RetryBackoffBase {
		cfg.RetryBackoffMax = cfg.RetryBackoffBase
	}
	if cfg.VerificationPoll > cfg.VerificationWindow {
		cfg.VerificationPoll = cfg.VerificationWindow
	}

	return cfg, nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envBool(key string, defaultVal bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		switch strings.ToLower(v) {
		case "yes", "on":
			return true
		case "no", "off":
			return false
		default:
			return defaultVal
		}
	}
	return b
}

func envPositiveInt(key string, defaultVal int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultVal
	}
	parsed, err := strconv.Atoi(v)
	if err != nil || parsed <= 0 {
		return defaultVal
	}
	return parsed
}

func envPositiveDuration(key string, defaultVal time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultVal
	}
	parsed, err := time.ParseDuration(v)
	if err != nil || parsed <= 0 {
		return defaultVal
	}
	return parsed
}
