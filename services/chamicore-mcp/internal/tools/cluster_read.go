package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
)

func (r *Runner) clusterHealthSummary(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct{}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}

	services := make([]map[string]any, 0, len(r.healthTarget))
	for _, target := range r.healthTarget {
		health, healthDetail := r.probeEndpoint(ctx, strings.TrimRight(target.URL, "/")+"/health")
		readiness, readinessDetail := r.probeEndpoint(ctx, strings.TrimRight(target.URL, "/")+"/readiness")
		detail := healthDetail
		if strings.TrimSpace(detail) == "" {
			detail = readinessDetail
		}
		services = append(services, map[string]any{
			"name":      target.Name,
			"health":    health,
			"readiness": readiness,
			"detail":    strings.TrimSpace(detail),
		})
	}

	return map[string]any{
		"services": services,
	}, nil
}

func (r *Runner) probeEndpoint(ctx context.Context, endpoint string) (string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "error", err.Error()
	}

	resp, err := r.healthClient.Do(req)
	if err != nil {
		return "error", err.Error()
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "error", fmt.Sprintf("read response body: %v", readErr)
	}

	detail := strings.TrimSpace(string(body))
	var payload map[string]any
	if jsonErr := json.Unmarshal(body, &payload); jsonErr == nil {
		if status, ok := payload["status"].(string); ok && strings.TrimSpace(status) != "" {
			detail = strings.TrimSpace(status)
		}
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return "ok", detail
	case http.StatusServiceUnavailable:
		return "not-ready", detail
	default:
		return "error", httputil.ProblemDetail{
			Status: resp.StatusCode,
			Detail: detail,
		}.Detail
	}
}
