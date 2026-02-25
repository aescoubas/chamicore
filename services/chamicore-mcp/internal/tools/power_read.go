package tools

import (
	"context"
	"strings"

	powerclient "git.cscs.ch/openchami/chamicore-power/pkg/client"
)

func (r *Runner) powerStatusGet(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		Nodes  []string `json:"nodes"`
		Groups []string `json:"groups"`
		Limit  int      `json:"limit"`
		Offset int      `json:"offset"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	if req.Limit < 0 || req.Offset < 0 {
		return nil, validationErrorf("limit and offset must be >= 0")
	}

	resp, err := r.power.GetPowerStatus(ctx, powerclient.PowerStatusOptions{
		Nodes:  trimStringList(req.Nodes),
		Groups: trimStringList(req.Groups),
	})
	if err != nil {
		return nil, mapExecutionError(err, "getting power status")
	}

	// API currently returns full status list. Apply optional local pagination.
	if resp != nil && (req.Offset > 0 || req.Limit > 0) {
		total := len(resp.Spec.NodeStatuses)
		if req.Offset >= total {
			resp.Spec.NodeStatuses = resp.Spec.NodeStatuses[:0]
		} else {
			end := total
			if req.Limit > 0 && req.Offset+req.Limit < end {
				end = req.Offset + req.Limit
			}
			resp.Spec.NodeStatuses = resp.Spec.NodeStatuses[req.Offset:end]
		}
	}

	return toMap(resp)
}

func (r *Runner) powerTransitionsList(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	if req.Limit < 0 || req.Offset < 0 {
		return nil, validationErrorf("limit and offset must be >= 0")
	}

	resp, err := r.power.ListTransitions(ctx, powerclient.ListTransitionsOptions{
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return nil, mapExecutionError(err, "listing power transitions")
	}
	return toMap(resp)
}

func (r *Runner) powerTransitionsGet(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	transitionID := strings.TrimSpace(req.ID)
	if transitionID == "" {
		return nil, validationErrorf("id is required")
	}

	resp, err := r.power.GetTransition(ctx, transitionID)
	if err != nil {
		return nil, mapExecutionError(err, "getting power transition")
	}
	return toMap(resp)
}

func trimStringList(values []string) []string {
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		trimmed = append(trimmed, normalized)
	}
	return trimmed
}
