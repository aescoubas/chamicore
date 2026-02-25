package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequireConfirmation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                 string
		toolName             string
		confirmationRequired bool
		args                 map[string]any
		wantErr              string
	}{
		{
			name:     "no confirmation needed for read tool",
			toolName: "smd.components.list",
			args:     map[string]any{},
		},
		{
			name:     "delete tool requires confirmation",
			toolName: "discovery.targets.delete",
			args:     map[string]any{"id": "target-1"},
			wantErr:  "requires confirm=true",
		},
		{
			name:     "delete tool accepts confirm true",
			toolName: "discovery.targets.delete",
			args: map[string]any{
				"id":      "target-1",
				"confirm": true,
			},
		},
		{
			name:     "abort transition requires confirmation",
			toolName: "power.transitions.abort",
			args:     map[string]any{"id": "transition-1"},
			wantErr:  "requires confirm=true",
		},
		{
			name:     "destructive power operation requires confirmation",
			toolName: "power.transitions.create",
			args: map[string]any{
				"operation": "ForceOff",
				"nodes":     []any{"node-1"},
			},
			wantErr: "requires confirm=true",
		},
		{
			name:     "destructive power operation accepts confirmation",
			toolName: "power.transitions.create",
			args: map[string]any{
				"operation": "forcerestart",
				"confirm":   true,
			},
		},
		{
			name:     "non destructive power operation does not require confirmation",
			toolName: "power.transitions.create",
			args: map[string]any{
				"operation": "On",
			},
		},
		{
			name:                 "explicit confirmationRequired metadata is honored",
			toolName:             "custom.destructive",
			confirmationRequired: true,
			args:                 map[string]any{},
			wantErr:              "requires confirm=true",
		},
		{
			name:     "confirm must be boolean true",
			toolName: "discovery.targets.delete",
			args: map[string]any{
				"id":      "target-1",
				"confirm": "true",
			},
			wantErr: "requires confirm=true",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := RequireConfirmation(tc.toolName, tc.confirmationRequired, tc.args)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
