package tools

import (
	"context"
	"strings"

	cloudclient "git.cscs.ch/openchami/chamicore-cloud-init/pkg/client"
)

func (r *Runner) cloudInitPayloadsList(ctx context.Context, args map[string]any) (map[string]any, error) {
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

	resp, err := r.cloudInit.List(ctx, cloudclient.ListOptions{
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return nil, mapExecutionError(err, "listing cloud-init payloads")
	}
	return toMap(resp)
}

func (r *Runner) cloudInitPayloadsGet(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		ComponentID string `json:"component_id"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	componentID := strings.TrimSpace(req.ComponentID)
	if componentID == "" {
		return nil, validationErrorf("component_id is required")
	}

	resp, err := r.cloudInit.List(ctx, cloudclient.ListOptions{
		ComponentID: componentID,
		Limit:       1,
		Offset:      0,
	})
	if err != nil {
		return nil, mapExecutionError(err, "getting cloud-init payload")
	}
	if resp == nil || len(resp.Items) == 0 {
		return nil, notFoundErrorf("cloud-init payload not found for component_id %s", componentID)
	}
	return toMap(resp.Items[0])
}
