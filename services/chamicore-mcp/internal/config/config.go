// Package config loads chamicore-mcp configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	// TransportStdio runs MCP over stdin/stdout.
	TransportStdio = "stdio"
	// TransportHTTP runs MCP over HTTP with SSE tool streaming.
	TransportHTTP = "http"

	defaultListenAddr = ":27774"
)

// Config holds service runtime configuration.
type Config struct {
	ListenAddr string
	LogLevel   string

	Transport string

	MetricsEnabled bool
	TracesEnabled  bool
	DevMode        bool
}

// Load returns configuration parsed from environment variables.
func Load() (Config, error) {
	cfg := Config{
		ListenAddr:     envOrDefault("CHAMICORE_MCP_LISTEN_ADDR", defaultListenAddr),
		LogLevel:       strings.ToLower(strings.TrimSpace(envOrDefault("CHAMICORE_MCP_LOG_LEVEL", "info"))),
		Transport:      strings.ToLower(strings.TrimSpace(envOrDefault("CHAMICORE_MCP_TRANSPORT", TransportStdio))),
		MetricsEnabled: envBool("CHAMICORE_MCP_METRICS_ENABLED", true),
		TracesEnabled:  envBool("CHAMICORE_MCP_TRACES_ENABLED", false),
		DevMode:        envBool("CHAMICORE_MCP_DEV_MODE", false),
	}

	switch cfg.Transport {
	case TransportStdio, TransportHTTP:
	default:
		return Config{}, fmt.Errorf("invalid CHAMICORE_MCP_TRANSPORT %q (allowed: %s|%s)", cfg.Transport, TransportStdio, TransportHTTP)
	}

	if strings.TrimSpace(cfg.ListenAddr) == "" {
		cfg.ListenAddr = defaultListenAddr
	}
	if strings.TrimSpace(cfg.LogLevel) == "" {
		cfg.LogLevel = "info"
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
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultVal
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		switch strings.ToLower(value) {
		case "yes", "on":
			return true
		case "no", "off":
			return false
		default:
			return defaultVal
		}
	}
	return parsed
}
