//go:build system

package system

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	baseclient "git.cscs.ch/openchami/chamicore-lib/httputil/client"
)

const (
	defaultAuthURL      = "http://127.0.0.1:3333"
	defaultSMDURL       = "http://127.0.0.1:27779"
	defaultBSSURL       = "http://127.0.0.1:27778"
	defaultCloudInitURL = "http://127.0.0.1:27777"

	defaultInternalToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	defaultReadyTimeout  = 2 * time.Minute
)

type endpoints struct {
	auth      string
	smd       string
	bss       string
	cloudInit string
}

func systemEndpoints() endpoints {
	return endpoints{
		auth:      envOrDefault("CHAMICORE_TEST_AUTH_URL", defaultAuthURL),
		smd:       envOrDefault("CHAMICORE_TEST_SMD_URL", defaultSMDURL),
		bss:       envOrDefault("CHAMICORE_TEST_BSS_URL", defaultBSSURL),
		cloudInit: envOrDefault("CHAMICORE_TEST_CLOUDINIT_URL", defaultCloudInitURL),
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

func systemContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 45*time.Second)
}

func waitForReadiness(t *testing.T, serviceName, baseURL string) {
	t.Helper()

	timeout := defaultReadyTimeout
	if raw := strings.TrimSpace(os.Getenv("CHAMICORE_TEST_READY_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			t.Fatalf("parse CHAMICORE_TEST_READY_TIMEOUT: %v", err)
		}
		timeout = parsed
	}

	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(timeout)

	var lastErr error
	var lastStatus int
	var lastBody string

	healthURL := baseURL + "/health"

	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err != nil {
			lastErr = err
			time.Sleep(1 * time.Second)
			continue
		}

		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			time.Sleep(1 * time.Second)
			continue
		}

		lastStatus = resp.StatusCode
		lastBody = strings.TrimSpace(string(bodyBytes))

		if resp.StatusCode == http.StatusOK {
			return
		}

		time.Sleep(1 * time.Second)
	}

	if lastErr != nil {
		t.Fatalf("%s not healthy at %s within %s: %v", serviceName, healthURL, timeout, lastErr)
	}
	t.Fatalf(
		"%s not healthy at %s within %s: status=%d body=%q",
		serviceName,
		healthURL,
		timeout,
		lastStatus,
		lastBody,
	)
}

func getText(t *testing.T, url string) (int, string) {
	t.Helper()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body for %s: %v", url, err)
	}

	return resp.StatusCode, string(body)
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func requireAPIErrorStatus(t *testing.T, err error, wantStatus int) {
	t.Helper()

	var apiErr *baseclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got: %T (%v)", err, err)
	}
	if apiErr.StatusCode != wantStatus {
		t.Fatalf("expected API status %d, got %d (%s)", wantStatus, apiErr.StatusCode, apiErr.Error())
	}
}
