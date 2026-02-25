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

	// ModeReadOnly allows only read-capability tools.
	ModeReadOnly = "read-only"
	// ModeReadWrite allows both read and write tools.
	ModeReadWrite = "read-write"

	defaultListenAddr = ":27774"
	defaultAuthURL    = "http://localhost:3333"
	defaultSMDURL     = "http://localhost:27779"
	defaultBSSURL     = "http://localhost:27778"
	defaultCloudURL   = "http://localhost:27777"
	defaultDiscURL    = "http://localhost:27776"
	defaultPowerURL   = "http://localhost:27775"
)

// Config holds service runtime configuration.
type Config struct {
	ListenAddr string
	LogLevel   string

	Transport string
	Mode      string

	EnableWrite         bool
	AllowCLIConfigToken bool
	CLIConfigPath       string

	AuthURL      string
	SMDURL       string
	BSSURL       string
	CloudInitURL string
	DiscoveryURL string
	PowerURL     string

	MetricsEnabled bool
	TracesEnabled  bool
	DevMode        bool
}

// Load returns configuration parsed from environment variables.
func Load() (Config, error) {
	cfg := Config{
		ListenAddr:          envOrDefault("CHAMICORE_MCP_LISTEN_ADDR", defaultListenAddr),
		LogLevel:            strings.ToLower(strings.TrimSpace(envOrDefault("CHAMICORE_MCP_LOG_LEVEL", "info"))),
		Transport:           strings.ToLower(strings.TrimSpace(envOrDefault("CHAMICORE_MCP_TRANSPORT", TransportStdio))),
		Mode:                strings.ToLower(strings.TrimSpace(envOrDefault("CHAMICORE_MCP_MODE", ModeReadOnly))),
		EnableWrite:         envBool("CHAMICORE_MCP_ENABLE_WRITE", false),
		MetricsEnabled:      envBool("CHAMICORE_MCP_METRICS_ENABLED", true),
		TracesEnabled:       envBool("CHAMICORE_MCP_TRACES_ENABLED", false),
		DevMode:             envBool("CHAMICORE_MCP_DEV_MODE", false),
		AllowCLIConfigToken: envBool("CHAMICORE_MCP_ALLOW_CLI_CONFIG_TOKEN", false),
		CLIConfigPath:       strings.TrimSpace(envOrDefault("CHAMICORE_MCP_CLI_CONFIG_PATH", "~/.chamicore/config.yaml")),
		AuthURL:             strings.TrimSpace(envOrDefault("CHAMICORE_MCP_AUTH_URL", defaultAuthURL)),
		SMDURL:              strings.TrimSpace(envOrDefault("CHAMICORE_MCP_SMD_URL", defaultSMDURL)),
		BSSURL:              strings.TrimSpace(envOrDefault("CHAMICORE_MCP_BSS_URL", defaultBSSURL)),
		CloudInitURL:        strings.TrimSpace(envOrDefault("CHAMICORE_MCP_CLOUD_INIT_URL", defaultCloudURL)),
		DiscoveryURL:        strings.TrimSpace(envOrDefault("CHAMICORE_MCP_DISCOVERY_URL", defaultDiscURL)),
		PowerURL:            strings.TrimSpace(envOrDefault("CHAMICORE_MCP_POWER_URL", defaultPowerURL)),
	}

	switch cfg.Transport {
	case TransportStdio, TransportHTTP:
	default:
		return Config{}, fmt.Errorf("invalid CHAMICORE_MCP_TRANSPORT %q (allowed: %s|%s)", cfg.Transport, TransportStdio, TransportHTTP)
	}
	switch cfg.Mode {
	case ModeReadOnly, ModeReadWrite:
	default:
		return Config{}, fmt.Errorf("invalid CHAMICORE_MCP_MODE %q (allowed: %s|%s)", cfg.Mode, ModeReadOnly, ModeReadWrite)
	}

	if strings.TrimSpace(cfg.ListenAddr) == "" {
		cfg.ListenAddr = defaultListenAddr
	}
	if strings.TrimSpace(cfg.LogLevel) == "" {
		cfg.LogLevel = "info"
	}
	if strings.TrimSpace(cfg.CLIConfigPath) == "" {
		cfg.CLIConfigPath = "~/.chamicore/config.yaml"
	}
	if strings.TrimSpace(cfg.AuthURL) == "" {
		cfg.AuthURL = defaultAuthURL
	}
	if strings.TrimSpace(cfg.SMDURL) == "" {
		cfg.SMDURL = defaultSMDURL
	}
	if strings.TrimSpace(cfg.BSSURL) == "" {
		cfg.BSSURL = defaultBSSURL
	}
	if strings.TrimSpace(cfg.CloudInitURL) == "" {
		cfg.CloudInitURL = defaultCloudURL
	}
	if strings.TrimSpace(cfg.DiscoveryURL) == "" {
		cfg.DiscoveryURL = defaultDiscURL
	}
	if strings.TrimSpace(cfg.PowerURL) == "" {
		cfg.PowerURL = defaultPowerURL
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
