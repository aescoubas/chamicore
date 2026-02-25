package server

import "fmt"

// ToolAuthorizer is the central policy gate for all tool executions.
type ToolAuthorizer interface {
	Mode() string
	AuthorizeTool(name, capability string) error
}

func authorizeToolCall(authorizer ToolAuthorizer, tool ToolSpec) error {
	if authorizer == nil {
		return nil
	}
	if err := authorizer.AuthorizeTool(tool.Name, tool.Capability); err != nil {
		return fmt.Errorf("tool authorization denied: %w", err)
	}
	return nil
}

func resolvedMode(authorizer ToolAuthorizer) string {
	if authorizer == nil {
		return "read-only"
	}
	return authorizer.Mode()
}
