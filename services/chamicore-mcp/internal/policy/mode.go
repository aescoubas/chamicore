// Package policy defines execution guardrails for MCP tool calls.
package policy

import (
	"fmt"
	"strings"
)

const (
	// ModeReadOnly allows only read capability tools.
	ModeReadOnly = "read-only"
	// ModeReadWrite allows read and write capability tools.
	ModeReadWrite = "read-write"
)

// Guard enforces mode-based tool execution policy.
type Guard struct {
	mode string
}

// NewGuard validates mode configuration and returns an execution guard.
//
// read-write mode requires enableWrite=true for dual-control safety.
func NewGuard(mode string, enableWrite bool) (*Guard, error) {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" {
		normalized = ModeReadOnly
	}

	switch normalized {
	case ModeReadOnly:
		return &Guard{mode: normalized}, nil
	case ModeReadWrite:
		if !enableWrite {
			return nil, fmt.Errorf("read-write mode requires CHAMICORE_MCP_ENABLE_WRITE=true")
		}
		return &Guard{mode: normalized}, nil
	default:
		return nil, fmt.Errorf("invalid mode %q (allowed: %s|%s)", normalized, ModeReadOnly, ModeReadWrite)
	}
}

// Mode returns the resolved mode.
func (g *Guard) Mode() string {
	if g == nil {
		return ModeReadOnly
	}
	return g.mode
}

// AuthorizeTool allows or denies tool execution based on tool capability.
func (g *Guard) AuthorizeTool(name, capability string) error {
	mode := g.Mode()
	toolName := strings.TrimSpace(name)
	if toolName == "" {
		toolName = "unknown"
	}

	switch strings.ToLower(strings.TrimSpace(capability)) {
	case "read":
		return nil
	case "write":
		if mode == ModeReadWrite {
			return nil
		}
		return fmt.Errorf("tool %s requires read-write mode", toolName)
	default:
		return fmt.Errorf("tool %s has unknown capability %q", toolName, strings.TrimSpace(capability))
	}
}
