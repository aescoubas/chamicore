// TEMPLATE: Configuration for __SERVICE_FULL__
// Environment variable prefix: CHAMICORE___SERVICE_UPPER___*
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration values for the __SERVICE__ service.
// Every field is loaded from environment variables with sensible defaults.
type Config struct {
	// ListenAddr is the address the HTTP server binds to.
	// Env: CHAMICORE___SERVICE_UPPER___LISTEN_ADDR
	// Default: ":__PORT__"
	ListenAddr string

	// DBDSN is the PostgreSQL connection string.
	// Env: CHAMICORE___SERVICE_UPPER___DB_DSN
	// Default: "postgres://chamicore:chamicore@localhost:5432/__SERVICE__?sslmode=disable"
	DBDSN string

	// LogLevel controls zerolog verbosity (trace, debug, info, warn, error, fatal, panic).
	// Env: CHAMICORE___SERVICE_UPPER___LOG_LEVEL
	// Default: "info"
	LogLevel string

	// DevMode enables human-friendly console log output.
	// Env: CHAMICORE___SERVICE_UPPER___DEV_MODE
	// Default: false
	DevMode bool

	// JWKSUrl is the URL for the JSON Web Key Set used to validate JWTs.
	// Env: CHAMICORE___SERVICE_UPPER___JWKS_URL
	// Default: ""
	JWKSUrl string

	// InternalToken is the pre-shared secret for service-to-service auth.
	// All Chamicore services in the same deployment share this value.
	// Env: CHAMICORE_INTERNAL_TOKEN
	// Default: ""
	InternalToken string

	// MetricsEnabled controls whether Prometheus metrics are exposed.
	// Env: CHAMICORE___SERVICE_UPPER___METRICS_ENABLED
	// Default: true
	MetricsEnabled bool

	// TracesEnabled controls whether OpenTelemetry traces are exported.
	// Env: CHAMICORE___SERVICE_UPPER___TRACES_ENABLED
	// Default: false
	TracesEnabled bool

	// PrometheusAddr is the address the Prometheus /metrics endpoint binds to.
	// Env: CHAMICORE___SERVICE_UPPER___PROMETHEUS_ADDR
	// Default: ":9090"
	PrometheusAddr string

	// TEMPLATE: Add service-specific configuration fields below.
	// Example:
	// MaxPageSize int  // Maximum number of items per page for list endpoints.
}

// Load reads configuration from environment variables, applying defaults
// where values are not set. It returns an error if a required value is
// missing or an invalid value is provided.
func Load() (Config, error) {
	cfg := Config{
		ListenAddr:     envOrDefault("CHAMICORE___SERVICE_UPPER___LISTEN_ADDR", ":__PORT__"),
		DBDSN:          envOrDefault("CHAMICORE___SERVICE_UPPER___DB_DSN", "postgres://chamicore:chamicore@localhost:5432/__SERVICE__?sslmode=disable"),
		LogLevel:       strings.ToLower(envOrDefault("CHAMICORE___SERVICE_UPPER___LOG_LEVEL", "info")),
		DevMode:        envBool("CHAMICORE___SERVICE_UPPER___DEV_MODE", false),
		JWKSUrl:        envOrDefault("CHAMICORE___SERVICE_UPPER___JWKS_URL", ""),
		InternalToken:  envOrDefault("CHAMICORE_INTERNAL_TOKEN", ""),
		MetricsEnabled: envBool("CHAMICORE___SERVICE_UPPER___METRICS_ENABLED", true),
		TracesEnabled:  envBool("CHAMICORE___SERVICE_UPPER___TRACES_ENABLED", false),
		PrometheusAddr: envOrDefault("CHAMICORE___SERVICE_UPPER___PROMETHEUS_ADDR", ":9090"),
	}

	// Validate required fields.
	if cfg.DBDSN == "" {
		return Config{}, fmt.Errorf("CHAMICORE___SERVICE_UPPER___DB_DSN is required")
	}

	// TEMPLATE: Add additional validation here.
	// Example:
	// if cfg.MaxPageSize <= 0 {
	//     cfg.MaxPageSize = 200
	// }

	return cfg, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// envOrDefault returns the value of the named environment variable, or
// defaultVal if the variable is empty or unset.
func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// envBool returns the boolean value of the named environment variable.
// Accepted truthy values: "1", "true", "yes", "on" (case-insensitive).
// Returns defaultVal if the variable is empty or unset.
func envBool(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		// Also accept "yes" / "on".
		switch strings.ToLower(v) {
		case "yes", "on":
			return true
		case "no", "off":
			return false
		}
		return defaultVal
	}
	return b
}

// envInt returns the integer value of the named environment variable.
// Returns defaultVal if the variable is empty, unset, or not a valid integer.
func envInt(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return i
}
