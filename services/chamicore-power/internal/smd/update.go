// Package smd provides SMD-side state update helpers for power transitions.
package smd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-smd/pkg/types"
)

var (
	// ErrUnsupportedPowerState indicates the input power state has no SMD mapping.
	ErrUnsupportedPowerState = errors.New("unsupported power state")
)

// ComponentClient defines the SMD API calls needed for state updates.
type ComponentClient interface {
	PatchComponent(ctx context.Context, id string, req types.PatchComponentRequest) (*httputil.Resource[types.Component], error)
}

// Updater patches node state in SMD after successful power verification.
type Updater struct {
	client ComponentClient
}

// NewUpdater creates an SMD updater from an SMD client.
func NewUpdater(client ComponentClient) *Updater {
	return &Updater{client: client}
}

// UpdateNodePowerState maps a final Redfish state to an SMD component state and
// patches the component in SMD.
func (u *Updater) UpdateNodePowerState(ctx context.Context, nodeID, powerState string) error {
	if u == nil || u.client == nil {
		return fmt.Errorf("smd updater is not configured")
	}

	componentID := strings.TrimSpace(nodeID)
	if componentID == "" {
		return fmt.Errorf("node id is required")
	}

	targetState, err := componentStateForPowerState(powerState)
	if err != nil {
		return err
	}

	req := types.PatchComponentRequest{
		State: &targetState,
	}
	if _, err := u.client.PatchComponent(ctx, componentID, req); err != nil {
		return fmt.Errorf("patching SMD component %q state to %q: %w", componentID, targetState, err)
	}

	return nil
}

func componentStateForPowerState(powerState string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(powerState)) {
	case "on":
		return "Ready", nil
	case "off":
		return "Off", nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedPowerState, strings.TrimSpace(powerState))
	}
}
