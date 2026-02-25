package server

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultProtocolVersion = "2024-11-05"
	defaultServerName      = "chamicore-mcp"
)

// ToolSpec represents a single MCP tool contract entry.
type ToolSpec struct {
	Name                 string         `yaml:"name" json:"name"`
	Capability           string         `yaml:"capability" json:"capability"`
	Description          string         `yaml:"description,omitempty" json:"description,omitempty"`
	RequiredScopes       []string       `yaml:"requiredScopes,omitempty" json:"requiredScopes,omitempty"`
	ConfirmationRequired bool           `yaml:"confirmationRequired,omitempty" json:"confirmationRequired,omitempty"`
	InputSchema          map[string]any `yaml:"inputSchema,omitempty" json:"inputSchema,omitempty"`
	OutputSchema         map[string]any `yaml:"outputSchema,omitempty" json:"outputSchema,omitempty"`
}

type toolContract struct {
	Version    string     `yaml:"version"`
	Service    string     `yaml:"service"`
	APIVersion string     `yaml:"apiVersion"`
	Tools      []ToolSpec `yaml:"tools"`
}

// ToolRegistry provides read-only access to parsed tools.
type ToolRegistry struct {
	contract toolContract
	byName   map[string]ToolSpec
}

// NewToolRegistry parses tools contract YAML and validates minimal invariants.
func NewToolRegistry(contractYAML []byte) (*ToolRegistry, error) {
	var parsed toolContract
	if err := yaml.Unmarshal(contractYAML, &parsed); err != nil {
		return nil, fmt.Errorf("decoding tool contract: %w", err)
	}
	if len(parsed.Tools) == 0 {
		return nil, fmt.Errorf("tool contract has no tools")
	}

	byName := make(map[string]ToolSpec, len(parsed.Tools))
	for _, tool := range parsed.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return nil, fmt.Errorf("tool contract contains empty tool name")
		}
		if _, exists := byName[name]; exists {
			return nil, fmt.Errorf("tool contract contains duplicate tool %q", name)
		}
		tool.Name = name
		tool.Capability = strings.TrimSpace(tool.Capability)
		if tool.Capability == "" {
			return nil, fmt.Errorf("tool %q has empty capability", name)
		}
		byName[name] = tool
	}

	return &ToolRegistry{
		contract: parsed,
		byName:   byName,
	}, nil
}

// List returns all registered tools in contract order.
func (r *ToolRegistry) List() []ToolSpec {
	items := make([]ToolSpec, 0, len(r.contract.Tools))
	items = append(items, r.contract.Tools...)
	return items
}

// Lookup returns a tool by name.
func (r *ToolRegistry) Lookup(name string) (ToolSpec, bool) {
	tool, ok := r.byName[strings.TrimSpace(name)]
	return tool, ok
}
