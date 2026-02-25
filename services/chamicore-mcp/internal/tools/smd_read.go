package tools

import (
	"context"
	"strings"

	smdclient "git.cscs.ch/openchami/chamicore-smd/pkg/client"
)

func (r *Runner) smdComponentsList(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
		Type   string `json:"type"`
		State  string `json:"state"`
		Role   string `json:"role"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	if req.Limit < 0 || req.Offset < 0 {
		return nil, validationErrorf("limit and offset must be >= 0")
	}

	resp, err := r.smd.ListComponents(ctx, smdclient.ComponentListOptions{
		Limit:  req.Limit,
		Offset: req.Offset,
		Type:   strings.TrimSpace(req.Type),
		State:  strings.TrimSpace(req.State),
		Role:   strings.TrimSpace(req.Role),
	})
	if err != nil {
		return nil, mapExecutionError(err, "listing SMD components")
	}
	return toMap(resp)
}

func (r *Runner) smdComponentsGet(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, validationErrorf("id is required")
	}

	resp, err := r.smd.GetComponent(ctx, id)
	if err != nil {
		return nil, mapExecutionError(err, "getting SMD component")
	}
	return toMap(resp)
}

func (r *Runner) smdGroupsList(ctx context.Context, args map[string]any) (map[string]any, error) {
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

	resp, err := r.smd.ListGroups(ctx, smdclient.GroupListOptions{
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return nil, mapExecutionError(err, "listing SMD groups")
	}
	return toMap(resp)
}

func (r *Runner) smdGroupsGet(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		Label string `json:"label"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		return nil, validationErrorf("label is required")
	}

	resp, err := r.smd.GetGroup(ctx, label)
	if err != nil {
		return nil, mapExecutionError(err, "getting SMD group")
	}
	return toMap(resp)
}
