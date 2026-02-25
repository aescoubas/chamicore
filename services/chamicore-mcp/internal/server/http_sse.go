package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-mcp/internal/policy"
)

func registerMCPHTTPRoutes(
	r chi.Router,
	registry *ToolRegistry,
	authorizer ToolAuthorizer,
	caller ToolCaller,
	version string,
	logger zerolog.Logger,
) {
	r.Route("/mcp/v1", func(r chi.Router) {
		r.Post("/initialize", handleInitializeHTTP(version))
		r.Get("/tools", handleListToolsHTTP(registry))
		r.Post("/tools/call", handleCallToolHTTP(registry, authorizer, caller, logger))
		r.Post("/tools/call/sse", handleCallToolSSE(registry, authorizer, caller, logger))
	})
}

func handleInitializeHTTP(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		result := initializeResult{
			ProtocolVersion: defaultProtocolVersion,
		}
		result.ServerInfo.Name = defaultServerName
		result.ServerInfo.Version = strings.TrimSpace(version)
		result.Capabilities.Tools.ListChanged = false
		httputil.RespondJSON(w, http.StatusOK, result)
	}
}

func handleListToolsHTTP(registry *ToolRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		tools := make([]toolDescriptor, 0, len(registry.List()))
		for _, tool := range registry.List() {
			tools = append(tools, toolDescriptor{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
			})
		}
		httputil.RespondJSON(w, http.StatusOK, listToolsResult{Tools: tools})
	}
}

func handleCallToolHTTP(
	registry *ToolRegistry,
	authorizer ToolAuthorizer,
	caller ToolCaller,
	logger zerolog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, tool, ok := parseCallToolRequest(w, r, registry, authorizer)
		if !ok {
			return
		}
		logger.Info().Str("transport", "http").Str("tool", tool.Name).Msg("received tool call")
		if caller == nil {
			httputil.RespondJSON(w, http.StatusOK, toolCallResultFromExecution(tool.Name, resolvedMode(authorizer), map[string]any{}))
			return
		}
		payload, err := caller.Call(r.Context(), tool.Name, params.Arguments)
		if err != nil {
			httputil.RespondProblem(w, r, toolErrorStatus(err), toolErrorMessage(err))
			return
		}
		httputil.RespondJSON(w, http.StatusOK, toolCallResultFromExecution(tool.Name, resolvedMode(authorizer), payload))
	}
}

func handleCallToolSSE(
	registry *ToolRegistry,
	authorizer ToolAuthorizer,
	caller ToolCaller,
	logger zerolog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, tool, ok := parseCallToolRequest(w, r, registry, authorizer)
		if !ok {
			return
		}

		controller := http.NewResponseController(w)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		logger.Info().Str("transport", "http-sse").Str("tool", tool.Name).Msg("streaming tool call")

		if err := writeSSEEvent(r.Context(), w, "accepted", map[string]any{
			"tool":      tool.Name,
			"status":    "accepted",
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return
		}
		_ = controller.Flush()

		if caller == nil {
			if err := writeSSEEvent(r.Context(), w, "result", toolCallResultFromExecution(tool.Name, resolvedMode(authorizer), map[string]any{})); err != nil {
				return
			}
			_ = controller.Flush()
			_ = writeSSEEvent(r.Context(), w, "done", map[string]any{"status": "done"})
			_ = controller.Flush()
			return
		}

		payload, err := caller.Call(r.Context(), tool.Name, params.Arguments)
		if err != nil {
			if writeErr := writeSSEEvent(r.Context(), w, "result", toolCallResultFromError(tool.Name, resolvedMode(authorizer), err)); writeErr != nil {
				return
			}
			_ = controller.Flush()
			_ = writeSSEEvent(r.Context(), w, "done", map[string]any{"status": "done"})
			_ = controller.Flush()
			return
		}

		if err := writeSSEEvent(r.Context(), w, "result", toolCallResultFromExecution(tool.Name, resolvedMode(authorizer), payload)); err != nil {
			return
		}
		_ = controller.Flush()

		_ = writeSSEEvent(r.Context(), w, "done", map[string]any{"status": "done"})
		_ = controller.Flush()
	}
}

func parseCallToolRequest(
	w http.ResponseWriter,
	r *http.Request,
	registry *ToolRegistry,
	authorizer ToolAuthorizer,
) (callToolParams, ToolSpec, bool) {
	var params callToolParams
	if err := decodeJSONStrict(r, &params); err != nil {
		httputil.RespondProblemf(w, r, http.StatusBadRequest, "invalid request body: %v", err)
		return callToolParams{}, ToolSpec{}, false
	}

	name := strings.TrimSpace(params.Name)
	if name == "" {
		httputil.RespondProblem(w, r, http.StatusBadRequest, "tool name is required")
		return callToolParams{}, ToolSpec{}, false
	}

	tool, ok := registry.Lookup(name)
	if !ok {
		httputil.RespondProblemf(w, r, http.StatusNotFound, "unknown tool: %s", name)
		return callToolParams{}, ToolSpec{}, false
	}
	if err := authorizeToolCall(authorizer, tool); err != nil {
		httputil.RespondProblem(w, r, http.StatusForbidden, err.Error())
		return callToolParams{}, ToolSpec{}, false
	}
	if err := policy.RequireConfirmation(tool.Name, tool.ConfirmationRequired, params.Arguments); err != nil {
		httputil.RespondProblem(w, r, http.StatusBadRequest, err.Error())
		return callToolParams{}, ToolSpec{}, false
	}

	return params, tool, true
}

func writeSSEEvent(ctx context.Context, w http.ResponseWriter, event string, payload any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "event: %s\n", strings.TrimSpace(event)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

func decodeJSONStrict(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("request must contain exactly one JSON object")
	}
	return nil
}
