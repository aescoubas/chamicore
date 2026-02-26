// Package audit provides structured audit logging for MCP tool calls.
package audit

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

var (
	bearerTokenPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/]+=*`)
	keyValuePattern    = regexp.MustCompile(`(?i)\b(token|secret|password|authorization)\s*[:=]\s*([^\s,;]+)`)
)

// ToolCallCompletion captures one finalized tool-call outcome.
type ToolCallCompletion struct {
	RequestID    string
	SessionID    string
	Transport    string
	ToolName     string
	Mode         string
	CallerSub    string
	Arguments    map[string]any
	Result       string
	ErrorDetail  string
	Duration     time.Duration
	ResponseCode int
}

// TargetSummary is a redacted summary of call targets.
type TargetSummary struct {
	NodeIDs     []string `json:"node_ids,omitempty"`
	GroupIDs    []string `json:"group_ids,omitempty"`
	ResourceIDs []string `json:"resource_ids,omitempty"`
}

// Logger emits structured audit entries.
type Logger struct {
	logger zerolog.Logger
}

// NewLogger creates an audit logger.
func NewLogger(logger zerolog.Logger) *Logger {
	return &Logger{
		logger: logger.With().Str("component", "audit").Logger(),
	}
}

// Complete writes a single completion log entry for one tool call.
func (l *Logger) Complete(event ToolCallCompletion) {
	if l == nil {
		return
	}

	result := strings.TrimSpace(event.Result)
	if result == "" {
		result = "error"
	}

	tool := strings.TrimSpace(event.ToolName)
	if tool == "" {
		tool = "unknown"
	}
	mode := strings.TrimSpace(event.Mode)
	if mode == "" {
		mode = "read-only"
	}

	duration := event.Duration
	if duration < 0 {
		duration = 0
	}

	entry := l.logger.Info().
		Str("event", "mcp.tool_call.completed").
		Str("request_id", strings.TrimSpace(event.RequestID)).
		Str("session_id", strings.TrimSpace(event.SessionID)).
		Str("transport", strings.TrimSpace(event.Transport)).
		Str("tool", tool).
		Str("mode", mode).
		Str("caller_subject", strings.TrimSpace(event.CallerSub)).
		Str("result", result).
		Int64("duration_ms", duration.Milliseconds()).
		Interface("target", SummarizeTargets(event.Arguments))

	if event.ResponseCode > 0 {
		entry = entry.Int("response_code", event.ResponseCode)
	}
	if redactedError := RedactSensitiveText(event.ErrorDetail); redactedError != "" {
		entry = entry.Str("error_detail", redactedError)
	}

	entry.Msg("tool call completed")
}

// SummarizeTargets builds a compact target summary from tool arguments.
func SummarizeTargets(args map[string]any) TargetSummary {
	if args == nil {
		return TargetSummary{}
	}

	summary := TargetSummary{
		NodeIDs:  uniqueStrings(readStringSlice(args, "nodes", "node_ids")),
		GroupIDs: uniqueStrings(append(readStringSlice(args, "groups", "group_ids"), readString(args, "label", "group")...)),
		ResourceIDs: uniqueStrings(append(
			append(
				readStringSlice(args, "ids", "component_ids", "members"),
				readString(args, "id", "component_id", "request_id", "scan_id", "target_id", "name")...,
			),
			readStringSlice(args, "resources", "resource_ids")...,
		)),
	}

	// Keep summary concise by removing overlap from generic resource IDs.
	summary.ResourceIDs = diff(summary.ResourceIDs, summary.NodeIDs, summary.GroupIDs)
	return summary
}

// RedactSensitiveText removes obvious secrets from free-text error details.
func RedactSensitiveText(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	redacted := bearerTokenPattern.ReplaceAllString(trimmed, "Bearer [REDACTED]")
	redacted = keyValuePattern.ReplaceAllStringFunc(redacted, func(match string) string {
		parts := strings.SplitN(match, ":", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("%s: [REDACTED]", strings.TrimSpace(parts[0]))
		}
		parts = strings.SplitN(match, "=", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("%s=[REDACTED]", strings.TrimSpace(parts[0]))
		}
		return "[REDACTED]"
	})
	return redacted
}

func readString(args map[string]any, keys ...string) []string {
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		raw, ok := args[key]
		if !ok {
			continue
		}
		asString, ok := raw.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(asString)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func readStringSlice(args map[string]any, keys ...string) []string {
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		raw, ok := args[key]
		if !ok {
			continue
		}
		switch typed := raw.(type) {
		case []string:
			for _, item := range typed {
				trimmed := strings.TrimSpace(item)
				if trimmed != "" {
					values = append(values, trimmed)
				}
			}
		case []any:
			for _, item := range typed {
				asString, ok := item.(string)
				if !ok {
					continue
				}
				trimmed := strings.TrimSpace(asString)
				if trimmed != "" {
					values = append(values, trimmed)
				}
			}
		}
	}
	return values
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}
	if len(unique) == 0 {
		return nil
	}
	slices.Sort(unique)
	return unique
}

func diff(values []string, drops ...[]string) []string {
	if len(values) == 0 {
		return nil
	}
	dropSet := map[string]struct{}{}
	for _, list := range drops {
		for _, item := range list {
			dropSet[item] = struct{}{}
		}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := dropSet[value]; exists {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
