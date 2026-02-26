package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-mcp/internal/audit"
	"git.cscs.ch/openchami/chamicore-mcp/internal/policy"
)

func registerMCPHTTPRoutes(
	r chi.Router,
	registry *ToolRegistry,
	authorizer ToolAuthorizer,
	sessionAuth SessionAuthenticator,
	caller ToolCaller,
	version string,
	logger zerolog.Logger,
) {
	r.Route("/mcp/v1", func(r chi.Router) {
		r.Post("/initialize", handleInitializeHTTP(version))
		r.Get("/tools", handleListToolsHTTP(registry))
		r.Post("/tools/call", handleCallToolHTTP(registry, authorizer, sessionAuth, caller, logger))
		r.Post("/tools/call/sse", handleCallToolSSE(registry, authorizer, sessionAuth, caller, logger))
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
	sessionAuth SessionAuthenticator,
	caller ToolCaller,
	logger zerolog.Logger,
) http.HandlerFunc {
	auditLogger := audit.NewLogger(logger)

	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		mode := resolvedMode(authorizer)
		requestID := httputil.RequestIDFromContext(r.Context())
		sessionID := sessionIDFromHTTPRequest(r, requestID)

		params, tool, principal, rejectionDetail, ok := parseCallToolRequest(w, r, registry, authorizer, sessionAuth)
		auditEvent := audit.ToolCallCompletion{
			RequestID: requestID,
			SessionID: sessionID,
			Transport: "http",
			ToolName:  strings.TrimSpace(params.Name),
			Mode:      mode,
			CallerSub: principal.Subject,
			Arguments: params.Arguments,
			Result:    "error",
			Duration:  0,
		}
		defer func() {
			auditEvent.Duration = time.Since(started)
			auditLogger.Complete(auditEvent)
		}()

		if !ok {
			auditEvent.ErrorDetail = rejectionDetail
			return
		}

		auditEvent.ToolName = tool.Name
		logger.Info().Str("transport", "http").Str("tool", tool.Name).Msg("received tool call")
		if caller == nil {
			auditEvent.Result = "success"
			auditEvent.ResponseCode = http.StatusOK
			httputil.RespondJSON(w, http.StatusOK, toolCallResultFromExecution(tool.Name, resolvedMode(authorizer), map[string]any{}))
			return
		}
		payload, err := caller.Call(r.Context(), tool.Name, params.Arguments)
		if err != nil {
			auditEvent.ErrorDetail = toolErrorMessage(err)
			auditEvent.ResponseCode = toolErrorStatus(err)
			httputil.RespondProblem(w, r, toolErrorStatus(err), toolErrorMessage(err))
			return
		}
		auditEvent.Result = "success"
		auditEvent.ResponseCode = http.StatusOK
		httputil.RespondJSON(w, http.StatusOK, toolCallResultFromExecution(tool.Name, resolvedMode(authorizer), payload))
	}
}

func handleCallToolSSE(
	registry *ToolRegistry,
	authorizer ToolAuthorizer,
	sessionAuth SessionAuthenticator,
	caller ToolCaller,
	logger zerolog.Logger,
) http.HandlerFunc {
	auditLogger := audit.NewLogger(logger)

	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		mode := resolvedMode(authorizer)
		requestID := httputil.RequestIDFromContext(r.Context())
		sessionID := sessionIDFromHTTPRequest(r, requestID)

		params, tool, principal, rejectionDetail, ok := parseCallToolRequest(w, r, registry, authorizer, sessionAuth)
		auditEvent := audit.ToolCallCompletion{
			RequestID: requestID,
			SessionID: sessionID,
			Transport: "http-sse",
			ToolName:  strings.TrimSpace(params.Name),
			Mode:      mode,
			CallerSub: principal.Subject,
			Arguments: params.Arguments,
			Result:    "error",
			Duration:  0,
		}
		defer func() {
			auditEvent.Duration = time.Since(started)
			auditLogger.Complete(auditEvent)
		}()

		if !ok {
			auditEvent.ErrorDetail = rejectionDetail
			return
		}
		auditEvent.ToolName = tool.Name

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
			auditEvent.ErrorDetail = err.Error()
			auditEvent.ResponseCode = http.StatusInternalServerError
			return
		}
		_ = controller.Flush()

		if caller == nil {
			if err := writeSSEEvent(r.Context(), w, "result", toolCallResultFromExecution(tool.Name, resolvedMode(authorizer), map[string]any{})); err != nil {
				auditEvent.ErrorDetail = err.Error()
				auditEvent.ResponseCode = http.StatusInternalServerError
				return
			}
			_ = controller.Flush()
			_ = writeSSEEvent(r.Context(), w, "done", map[string]any{"status": "done"})
			_ = controller.Flush()
			auditEvent.Result = "success"
			auditEvent.ResponseCode = http.StatusOK
			return
		}

		payload, err := caller.Call(r.Context(), tool.Name, params.Arguments)
		if err != nil {
			if writeErr := writeSSEEvent(r.Context(), w, "result", toolCallResultFromError(tool.Name, resolvedMode(authorizer), err)); writeErr != nil {
				auditEvent.ErrorDetail = writeErr.Error()
				auditEvent.ResponseCode = http.StatusInternalServerError
				return
			}
			_ = controller.Flush()
			_ = writeSSEEvent(r.Context(), w, "done", map[string]any{"status": "done"})
			_ = controller.Flush()
			auditEvent.ErrorDetail = toolErrorMessage(err)
			auditEvent.ResponseCode = toolErrorStatus(err)
			return
		}

		if err := writeSSEEvent(r.Context(), w, "result", toolCallResultFromExecution(tool.Name, resolvedMode(authorizer), payload)); err != nil {
			auditEvent.ErrorDetail = err.Error()
			auditEvent.ResponseCode = http.StatusInternalServerError
			return
		}
		_ = controller.Flush()

		_ = writeSSEEvent(r.Context(), w, "done", map[string]any{"status": "done"})
		_ = controller.Flush()
		auditEvent.Result = "success"
		auditEvent.ResponseCode = http.StatusOK
	}
}

func parseCallToolRequest(
	w http.ResponseWriter,
	r *http.Request,
	registry *ToolRegistry,
	authorizer ToolAuthorizer,
	sessionAuth SessionAuthenticator,
) (callToolParams, ToolSpec, SessionPrincipal, string, bool) {
	principal, err := authenticateHTTPToolCall(r, sessionAuth)
	if err != nil {
		status, detail := authFailureResponse(err)
		httputil.RespondProblem(w, r, status, detail)
		return callToolParams{}, ToolSpec{}, SessionPrincipal{}, detail, false
	}

	var params callToolParams
	if err := decodeJSONStrict(r, &params); err != nil {
		detail := fmt.Sprintf("invalid request body: %v", err)
		httputil.RespondProblem(w, r, http.StatusBadRequest, detail)
		return callToolParams{}, ToolSpec{}, principal, detail, false
	}

	name := strings.TrimSpace(params.Name)
	if name == "" {
		httputil.RespondProblem(w, r, http.StatusBadRequest, "tool name is required")
		return params, ToolSpec{}, principal, "tool name is required", false
	}

	tool, ok := registry.Lookup(name)
	if !ok {
		detail := fmt.Sprintf("unknown tool: %s", name)
		httputil.RespondProblem(w, r, http.StatusNotFound, detail)
		return params, ToolSpec{}, principal, detail, false
	}
	if err := authorizeToolCall(authorizer, tool); err != nil {
		httputil.RespondProblem(w, r, http.StatusForbidden, err.Error())
		return params, tool, principal, err.Error(), false
	}
	if err := policy.RequireConfirmation(tool.Name, tool.ConfirmationRequired, params.Arguments); err != nil {
		httputil.RespondProblem(w, r, http.StatusBadRequest, err.Error())
		return params, tool, principal, err.Error(), false
	}
	if err := requireToolScopes(tool, principal); err != nil {
		httputil.RespondProblem(w, r, http.StatusForbidden, err.Error())
		return params, tool, principal, err.Error(), false
	}

	return params, tool, principal, "", true
}

func authenticateHTTPToolCall(r *http.Request, authn SessionAuthenticator) (SessionPrincipal, error) {
	if authn == nil {
		return SessionPrincipal{}, fmt.Errorf("%w; set CHAMICORE_MCP_TOKEN or CHAMICORE_TOKEN", ErrSessionTokenMissing)
	}
	return authn.AuthenticateHTTP(r)
}

func authFailureResponse(err error) (int, string) {
	if err == nil {
		return http.StatusUnauthorized, "unauthorized"
	}
	switch {
	case errors.Is(err, ErrSessionTokenMissing):
		return http.StatusUnauthorized, "MCP session token is not configured; set CHAMICORE_MCP_TOKEN or CHAMICORE_TOKEN"
	case errors.Is(err, ErrBearerTokenMissing):
		return http.StatusUnauthorized, "missing or malformed Authorization header; expected Bearer <token>"
	case errors.Is(err, ErrBearerTokenInvalid):
		return http.StatusUnauthorized, "invalid bearer token for MCP session"
	default:
		return http.StatusUnauthorized, err.Error()
	}
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

func sessionIDFromHTTPRequest(r *http.Request, fallback string) string {
	if r == nil {
		return strings.TrimSpace(fallback)
	}
	if sessionID := strings.TrimSpace(r.Header.Get("MCP-Session-ID")); sessionID != "" {
		return sessionID
	}
	if sessionID := strings.TrimSpace(r.Header.Get("X-Session-ID")); sessionID != "" {
		return sessionID
	}
	return strings.TrimSpace(fallback)
}
