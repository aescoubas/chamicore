// Package auth resolves upstream access tokens for MCP service calls.
package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// TokenSource identifies where a token was resolved from.
type TokenSource string

const (
	// TokenSourceMCPEnv is CHAMICORE_MCP_TOKEN.
	TokenSourceMCPEnv TokenSource = "chamicore_mcp_token"
	// TokenSourceSharedEnv is CHAMICORE_TOKEN.
	TokenSourceSharedEnv TokenSource = "chamicore_token"
	// TokenSourceCLIConfig is ~/.chamicore/config.yaml auth.token.
	TokenSourceCLIConfig TokenSource = "cli_config"
)

// TokenResolution contains the resolved token and source.
type TokenResolution struct {
	Token  string
	Source TokenSource
}

// TokenSourceOptions controls token resolution.
type TokenSourceOptions struct {
	AllowCLIConfigToken bool
	CLIConfigPath       string
}

type cliConfigFile struct {
	Auth struct {
		Token string `yaml:"token"`
	} `yaml:"auth"`
}

// ResolveToken resolves token using deterministic precedence:
// 1) CHAMICORE_MCP_TOKEN
// 2) CHAMICORE_TOKEN
// 3) CLI config auth.token (only when AllowCLIConfigToken=true)
func ResolveToken(opts TokenSourceOptions) (TokenResolution, error) {
	if token := strings.TrimSpace(os.Getenv("CHAMICORE_MCP_TOKEN")); token != "" {
		return TokenResolution{Token: token, Source: TokenSourceMCPEnv}, nil
	}

	if token := strings.TrimSpace(os.Getenv("CHAMICORE_TOKEN")); token != "" {
		return TokenResolution{Token: token, Source: TokenSourceSharedEnv}, nil
	}

	if !opts.AllowCLIConfigToken {
		return TokenResolution{}, nil
	}

	configPath := expandPath(defaultIfEmpty(strings.TrimSpace(opts.CLIConfigPath), "~/.chamicore/config.yaml"))
	data, err := os.ReadFile(configPath)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		return TokenResolution{}, nil
	default:
		return TokenResolution{}, fmt.Errorf("reading CLI config token source: %w", err)
	}

	var cfg cliConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return TokenResolution{}, fmt.Errorf("decoding CLI config token source: %w", err)
	}

	token := strings.TrimSpace(cfg.Auth.Token)
	if token == "" {
		return TokenResolution{}, nil
	}

	return TokenResolution{Token: token, Source: TokenSourceCLIConfig}, nil
}

func defaultIfEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func expandPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return filepath.Clean(path)
}
