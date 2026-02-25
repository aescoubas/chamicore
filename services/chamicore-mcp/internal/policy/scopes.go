// Package policy defines execution guardrails for MCP tool calls.
package policy

import (
	"fmt"
	"slices"
	"strings"
)

// RequireScopes validates that granted scopes satisfy required tool scopes.
//
// V1 behavior:
// - Empty required scopes means no scope gate.
// - "admin" in granted scopes is treated as broad allow.
func RequireScopes(toolName string, required, granted []string) error {
	requiredScopes := normalizeScopeList(required)
	if len(requiredScopes) == 0 {
		return nil
	}

	grantedScopes := normalizeScopeList(granted)
	if slices.Contains(grantedScopes, "admin") {
		return nil
	}

	missing := make([]string, 0, len(requiredScopes))
	for _, scope := range requiredScopes {
		if !slices.Contains(grantedScopes, scope) {
			missing = append(missing, scope)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	tool := strings.TrimSpace(toolName)
	if tool == "" {
		tool = "unknown"
	}

	grantedSummary := "none"
	if len(grantedScopes) > 0 {
		grantedSummary = strings.Join(grantedScopes, ", ")
	}

	return fmt.Errorf(
		"tool %s missing required scope(s): %s (granted: %s)",
		tool,
		strings.Join(missing, ", "),
		grantedSummary,
	)
}

func normalizeScopeList(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		trimmed := strings.TrimSpace(scope)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
