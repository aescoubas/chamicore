//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	defaultMCPRWHealthTimeout = 30 * time.Second
)

func TestSmoke_MCPModeAndConfirmationGates(t *testing.T) {
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

	token := authToken()

	componentID := uniqueID("node-mcp-ro")
	writeArgs := map[string]any{
		"component_id": componentID,
		"kernel":       "http://172.16.10.1:8080/pxe/vmlinuz",
		"initrd":       "http://172.16.10.1:8080/pxe/initrd.img",
		"params":       []string{"console=ttyS0", "ip=dhcp"},
	}

	status, body, err := callMCPTool(t.Context(), endpoints.mcp, token, "bss.bootparams.upsert", writeArgs)
	if err != nil {
		t.Fatalf("read-only MCP write call failed: %v", err)
	}
	if status != http.StatusForbidden {
		t.Fatalf("read-only mode should deny write tool, got status=%d body=%s", status, body)
	}
	if !strings.Contains(strings.ToLower(body), "requires read-write mode") {
		t.Fatalf("read-only denial did not mention mode gate, body=%s", body)
	}

	readWriteBaseURL := resolveReadWriteMCPURL(t, endpoints, token)
	readWriteComponentID := uniqueID("node-mcp-rw")
	readWriteArgs := map[string]any{
		"component_id": readWriteComponentID,
		"kernel":       "http://172.16.10.1:8080/pxe/vmlinuz",
		"initrd":       "http://172.16.10.1:8080/pxe/initrd.img",
		"params":       []string{"console=ttyS0", "ip=dhcp"},
	}

	status, body, err = callMCPTool(t.Context(), readWriteBaseURL, token, "bss.bootparams.upsert", readWriteArgs)
	if err != nil {
		t.Fatalf("read-write MCP upsert call failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("read-write mode should allow write tool, got status=%d body=%s", status, body)
	}
	assertMCPToolCallSuccess(t, body, "bss.bootparams.upsert")

	status, body, err = callMCPTool(t.Context(), readWriteBaseURL, token, "bss.bootparams.delete", map[string]any{
		"component_id": readWriteComponentID,
	})
	if err != nil {
		t.Fatalf("destructive MCP delete call failed: %v", err)
	}
	if status != http.StatusBadRequest {
		t.Fatalf("destructive call without confirm should fail with 400, got status=%d body=%s", status, body)
	}
	if !strings.Contains(strings.ToLower(body), "requires confirm=true") {
		t.Fatalf("destructive denial did not mention confirm=true, body=%s", body)
	}

	status, body, err = callMCPTool(t.Context(), readWriteBaseURL, token, "bss.bootparams.delete", map[string]any{
		"component_id": readWriteComponentID,
		"confirm":      true,
	})
	if err != nil {
		t.Fatalf("confirmed destructive MCP delete call failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("confirmed destructive call should succeed, got status=%d body=%s", status, body)
	}
	assertMCPToolCallSuccess(t, body, "bss.bootparams.delete")
}

func resolveReadWriteMCPURL(t *testing.T, endpoints smokeEndpoints, token string) string {
	t.Helper()

	if configured := strings.TrimSpace(os.Getenv("CHAMICORE_TEST_MCP_RW_URL")); configured != "" {
		if err := waitForHealthURL(t.Context(), strings.TrimRight(configured, "/")+"/health", defaultMCPRWHealthTimeout); err != nil {
			t.Fatalf("configured CHAMICORE_TEST_MCP_RW_URL is not healthy: %v", err)
		}
		return strings.TrimRight(configured, "/")
	}
	return startLocalReadWriteMCP(t, endpoints, token)
}

func startLocalReadWriteMCP(t *testing.T, endpoints smokeEndpoints, token string) string {
	t.Helper()

	addr, err := freeTCPAddr()
	if err != nil {
		t.Fatalf("allocating local MCP read-write listen address: %v", err)
	}
	baseURL := "http://" + addr

	binPath := fmt.Sprintf("%s/chamicore-mcp-smoke", t.TempDir())
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/chamicore-mcp")
	buildCmd.Dir = "../../services/chamicore-mcp"
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building local read-write MCP binary: %v\noutput:\n%s", err, strings.TrimSpace(string(buildOutput)))
	}

	cmdCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(cmdCtx, binPath)
	logBuffer := &bytes.Buffer{}
	cmd.Stdout = logBuffer
	cmd.Stderr = logBuffer
	cmd.Env = append(os.Environ(),
		"CHAMICORE_MCP_TRANSPORT=http",
		"CHAMICORE_MCP_MODE=read-write",
		"CHAMICORE_MCP_ENABLE_WRITE=true",
		"CHAMICORE_MCP_ALLOW_CLI_CONFIG_TOKEN=false",
		"CHAMICORE_MCP_METRICS_ENABLED=false",
		"CHAMICORE_MCP_TRACES_ENABLED=false",
		"CHAMICORE_MCP_DEV_MODE=false",
		fmt.Sprintf("CHAMICORE_MCP_LISTEN_ADDR=%s", addr),
		fmt.Sprintf("CHAMICORE_MCP_TOKEN=%s", token),
		fmt.Sprintf("CHAMICORE_MCP_AUTH_URL=%s", endpoints.auth),
		fmt.Sprintf("CHAMICORE_MCP_SMD_URL=%s", endpoints.smd),
		fmt.Sprintf("CHAMICORE_MCP_BSS_URL=%s", endpoints.bss),
		fmt.Sprintf("CHAMICORE_MCP_CLOUD_INIT_URL=%s", endpoints.cloudInit),
		fmt.Sprintf("CHAMICORE_MCP_DISCOVERY_URL=%s", endpoints.discovery),
		fmt.Sprintf("CHAMICORE_MCP_POWER_URL=%s", endpoints.power),
	)

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("starting local read-write MCP service: %v", err)
	}

	t.Cleanup(func() {
		cancel()
		waitDone := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(waitDone)
		}()

		select {
		case <-waitDone:
		case <-time.After(5 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-waitDone
		}
	})

	if err := waitForHealthURL(t.Context(), baseURL+"/health", defaultMCPRWHealthTimeout); err != nil {
		cancel()
		_ = cmd.Wait()
		t.Fatalf("local read-write MCP did not become healthy: %v\nlogs:\n%s", err, strings.TrimSpace(logBuffer.String()))
	}

	return baseURL
}

func callMCPTool(ctx context.Context, baseURL, token, name string, args map[string]any) (int, string, error) {
	requestBody, err := json.Marshal(map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return 0, "", fmt.Errorf("encoding tool call request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/mcp/v1/tools/call",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return 0, "", fmt.Errorf("creating tool call request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("executing tool call request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return resp.StatusCode, "", fmt.Errorf("reading tool call response: %w", err)
	}
	return resp.StatusCode, strings.TrimSpace(string(body)), nil
}

func assertMCPToolCallSuccess(t *testing.T, body, expectedTool string) {
	t.Helper()

	var parsed struct {
		IsError           bool           `json:"isError"`
		StructuredContent map[string]any `json:"structuredContent"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("invalid MCP success payload: %v body=%s", err, body)
	}
	if parsed.IsError {
		t.Fatalf("expected MCP success but got isError=true body=%s", body)
	}

	toolName, _ := parsed.StructuredContent["tool"].(string)
	if strings.TrimSpace(toolName) != expectedTool {
		t.Fatalf("unexpected MCP tool in response: got=%q want=%q body=%s", toolName, expectedTool, body)
	}

	status, _ := parsed.StructuredContent["status"].(string)
	if strings.TrimSpace(status) != "ok" {
		t.Fatalf("unexpected MCP response status: got=%q want=%q body=%s", status, "ok", body)
	}
}

func waitForHealthURL(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 1 * time.Second}
	last := ""

	for {
		if time.Now().After(deadline) {
			break
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("creating health request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			last = err.Error()
			time.Sleep(250 * time.Millisecond)
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		if readErr != nil {
			last = readErr.Error()
			time.Sleep(250 * time.Millisecond)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		last = fmt.Sprintf("status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
		time.Sleep(250 * time.Millisecond)
	}

	if last == "" {
		last = "timed out"
	}
	return fmt.Errorf("health not ready at %s within %s (%s)", url, timeout, last)
}

func freeTCPAddr() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	return listener.Addr().String(), nil
}
