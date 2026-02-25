package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/rs/zerolog"

	"git.cscs.ch/openchami/chamicore-mcp/internal/policy"
)

const (
	rpcCodeInvalidRequest = -32600
	rpcCodeMethodNotFound = -32601
	rpcCodeInvalidParams  = -32602
	rpcCodeInternalError  = -32603
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	ServerInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
	Capabilities struct {
		Tools struct {
			ListChanged bool `json:"listChanged"`
		} `json:"tools"`
	} `json:"capabilities"`
}

type listToolsResult struct {
	Tools []toolDescriptor `json:"tools"`
}

type toolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

type callToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type callToolResult struct {
	Content           []contentBlock `json:"content"`
	IsError           bool           `json:"isError"`
	StructuredContent map[string]any `json:"structuredContent,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// RunStdio handles MCP requests over stdin/stdout using JSON-RPC line-delimited messages.
func RunStdio(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
	registry *ToolRegistry,
	authorizer ToolAuthorizer,
	caller ToolCaller,
	version string,
	logger zerolog.Logger,
) error {
	scanner := bufio.NewScanner(in)
	// Allow larger requests in stdio mode (up to 4 MiB per message).
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	writer := bufio.NewWriter(out)
	defer writer.Flush()

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			if writeErr := writeRPC(writer, rpcResponse{
				JSONRPC: "2.0",
				Error: &rpcError{
					Code:    rpcCodeInvalidRequest,
					Message: fmt.Sprintf("invalid json-rpc payload: %v", err),
				},
			}); writeErr != nil {
				return writeErr
			}
			continue
		}

		resp := handleRPCRequest(ctx, req, registry, authorizer, caller, version, logger)
		if err := writeRPC(writer, resp); err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading stdio request: %w", err)
	}
	return nil
}

func writeRPC(w *bufio.Writer, resp rpcResponse) error {
	encoded, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("encoding rpc response: %w", err)
	}
	if _, err := w.Write(encoded); err != nil {
		return fmt.Errorf("writing rpc response: %w", err)
	}
	if err := w.WriteByte('\n'); err != nil {
		return fmt.Errorf("writing rpc newline: %w", err)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flushing rpc response: %w", err)
	}
	return nil
}

func handleRPCRequest(
	ctx context.Context,
	req rpcRequest,
	registry *ToolRegistry,
	authorizer ToolAuthorizer,
	caller ToolCaller,
	version string,
	logger zerolog.Logger,
) rpcResponse {
	response := rpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	if strings.TrimSpace(req.JSONRPC) != "2.0" {
		response.Error = &rpcError{
			Code:    rpcCodeInvalidRequest,
			Message: "jsonrpc must be 2.0",
		}
		return response
	}

	switch strings.TrimSpace(req.Method) {
	case "initialize":
		result := initializeResult{ProtocolVersion: defaultProtocolVersion}
		result.ServerInfo.Name = defaultServerName
		result.ServerInfo.Version = strings.TrimSpace(version)
		result.Capabilities.Tools.ListChanged = false
		response.Result = result
		return response

	case "tools/list":
		items := make([]toolDescriptor, 0, len(registry.List()))
		for _, tool := range registry.List() {
			items = append(items, toolDescriptor{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
			})
		}
		response.Result = listToolsResult{Tools: items}
		return response

	case "tools/call":
		var params callToolParams
		if len(req.Params) == 0 {
			response.Error = &rpcError{
				Code:    rpcCodeInvalidParams,
				Message: "missing params",
			}
			return response
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			response.Error = &rpcError{
				Code:    rpcCodeInvalidParams,
				Message: fmt.Sprintf("invalid tools/call params: %v", err),
			}
			return response
		}
		name := strings.TrimSpace(params.Name)
		tool, ok := registry.Lookup(name)
		if !ok {
			response.Error = &rpcError{
				Code:    rpcCodeInvalidParams,
				Message: fmt.Sprintf("unknown tool: %s", name),
			}
			return response
		}
		if err := authorizeToolCall(authorizer, tool); err != nil {
			response.Error = &rpcError{
				Code:    rpcCodeInvalidParams,
				Message: err.Error(),
			}
			return response
		}
		if err := policy.RequireConfirmation(tool.Name, tool.ConfirmationRequired, params.Arguments); err != nil {
			response.Error = &rpcError{
				Code:    rpcCodeInvalidParams,
				Message: err.Error(),
			}
			return response
		}
		logger.Info().Str("transport", "stdio").Str("tool", tool.Name).Msg("received tool call")
		if caller == nil {
			response.Result = callToolResult{
				Content: []contentBlock{
					{
						Type: "text",
						Text: fmt.Sprintf("tool %s accepted (no caller configured)", tool.Name),
					},
				},
				IsError: false,
				StructuredContent: map[string]any{
					"tool":   tool.Name,
					"status": "accepted",
					"mode":   resolvedMode(authorizer),
				},
			}
			return response
		}

		payload, err := caller.Call(ctx, tool.Name, params.Arguments)
		if err != nil {
			response.Result = toolCallResultFromError(tool.Name, resolvedMode(authorizer), err)
			return response
		}
		response.Result = toolCallResultFromExecution(tool.Name, resolvedMode(authorizer), payload)
		return response

	default:
		response.Error = &rpcError{
			Code:    rpcCodeMethodNotFound,
			Message: fmt.Sprintf("unknown method: %s", strings.TrimSpace(req.Method)),
		}
		return response
	}
}
