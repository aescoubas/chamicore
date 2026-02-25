package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ToolCaller executes one tool call and returns structured content.
type ToolCaller interface {
	Call(ctx context.Context, name string, args map[string]any) (map[string]any, error)
}

type statusCoder interface {
	StatusCode() int
}

func toolErrorStatus(err error) int {
	var withStatus statusCoder
	if err != nil && errors.As(err, &withStatus) {
		status := withStatus.StatusCode()
		if status >= 400 && status <= 599 {
			return status
		}
	}
	return http.StatusInternalServerError
}

func toolErrorMessage(err error) string {
	if err == nil {
		return "unknown tool execution error"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "unknown tool execution error"
	}
	return message
}

func toolCallResultFromExecution(name, mode string, payload map[string]any) callToolResult {
	return callToolResult{
		Content: []contentBlock{
			{
				Type: "text",
				Text: fmt.Sprintf("tool %s executed", strings.TrimSpace(name)),
			},
		},
		IsError: false,
		StructuredContent: map[string]any{
			"tool":   strings.TrimSpace(name),
			"mode":   strings.TrimSpace(mode),
			"status": "ok",
			"result": payload,
		},
	}
}

func toolCallResultFromError(name, mode string, err error) callToolResult {
	return callToolResult{
		Content: []contentBlock{
			{
				Type: "text",
				Text: toolErrorMessage(err),
			},
		},
		IsError: true,
		StructuredContent: map[string]any{
			"tool":   strings.TrimSpace(name),
			"mode":   strings.TrimSpace(mode),
			"status": "error",
			"error": map[string]any{
				"status":  toolErrorStatus(err),
				"message": toolErrorMessage(err),
			},
		},
	}
}
