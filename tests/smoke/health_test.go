//go:build smoke

package smoke

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	defaultAuthURL      = "http://127.0.0.1:3333"
	defaultSMDURL       = "http://127.0.0.1:27779"
	defaultBSSURL       = "http://127.0.0.1:27778"
	defaultCloudInitURL = "http://127.0.0.1:27777"
	defaultDiscoveryURL = "http://127.0.0.1:27776"
	defaultPowerURL     = "http://127.0.0.1:27775"
	defaultMCPURL       = "http://127.0.0.1:27774"

	defaultInternalToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	defaultHealthTimeout = 10 * time.Second
)

type smokeEndpoints struct {
	auth      string
	smd       string
	bss       string
	cloudInit string
	discovery string
	power     string
	mcp       string
}

type serviceHealth struct {
	name    string
	baseURL string
}

func TestSmoke_HealthEndpoints(t *testing.T) {
	endpoints := smokeTestEndpoints()
	waitForAllHealthy(t, []serviceHealth{
		{name: "auth", baseURL: endpoints.auth},
		{name: "smd", baseURL: endpoints.smd},
		{name: "bss", baseURL: endpoints.bss},
		{name: "cloud-init", baseURL: endpoints.cloudInit},
		{name: "discovery", baseURL: endpoints.discovery},
		{name: "power", baseURL: endpoints.power},
		{name: "mcp", baseURL: endpoints.mcp},
	}, defaultHealthTimeout)
}

func smokeTestEndpoints() smokeEndpoints {
	return smokeEndpoints{
		auth:      envOrDefault("CHAMICORE_TEST_AUTH_URL", defaultAuthURL),
		smd:       envOrDefault("CHAMICORE_TEST_SMD_URL", defaultSMDURL),
		bss:       envOrDefault("CHAMICORE_TEST_BSS_URL", defaultBSSURL),
		cloudInit: envOrDefault("CHAMICORE_TEST_CLOUDINIT_URL", defaultCloudInitURL),
		discovery: envOrDefault("CHAMICORE_TEST_DISCOVERY_URL", defaultDiscoveryURL),
		power:     envOrDefault("CHAMICORE_TEST_POWER_URL", defaultPowerURL),
		mcp:       envOrDefault("CHAMICORE_TEST_MCP_URL", defaultMCPURL),
	}
}

func authToken() string {
	return envOrDefault("CHAMICORE_INTERNAL_TOKEN", defaultInternalToken)
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func waitForAllHealthy(t *testing.T, services []serviceHealth, timeout time.Duration) {
	t.Helper()

	client := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(timeout)
	pending := map[string]string{}
	lastError := map[string]string{}

	for _, service := range services {
		pending[service.name] = strings.TrimRight(service.baseURL, "/")
	}

	for {
		for serviceName, baseURL := range pending {
			healthURL := baseURL + "/health"
			resp, err := client.Get(healthURL)
			if err != nil {
				lastError[serviceName] = err.Error()
				continue
			}

			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			if readErr != nil {
				lastError[serviceName] = readErr.Error()
				continue
			}

			if resp.StatusCode == http.StatusOK {
				delete(pending, serviceName)
				delete(lastError, serviceName)
				continue
			}

			lastError[serviceName] = fmt.Sprintf("status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		if len(pending) == 0 {
			return
		}
		if time.Now().After(deadline) {
			break
		}

		time.Sleep(250 * time.Millisecond)
	}

	parts := make([]string, 0, len(pending))
	for name := range pending {
		parts = append(parts, fmt.Sprintf("%s (%s)", name, lastError[name]))
	}

	t.Fatalf("services not healthy within %s: %s", timeout, strings.Join(parts, "; "))
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
