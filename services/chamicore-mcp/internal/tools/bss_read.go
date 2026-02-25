package tools

import (
	"context"
	"strings"

	bssclient "git.cscs.ch/openchami/chamicore-bss/pkg/client"
)

func (r *Runner) bssBootParamsList(ctx context.Context, args map[string]any) (map[string]any, error) {
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

	resp, err := r.bss.List(ctx, bssclient.ListOptions{
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return nil, mapExecutionError(err, "listing BSS bootparams")
	}
	return toMap(resp)
}

func (r *Runner) bssBootParamsGet(ctx context.Context, args map[string]any) (map[string]any, error) {
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

	resp, err := r.bss.List(ctx, bssclient.ListOptions{
		ComponentID: componentID,
		Limit:       1,
		Offset:      0,
	})
	if err != nil {
		return nil, mapExecutionError(err, "getting BSS bootparams")
	}
	if resp == nil || len(resp.Items) == 0 {
		return nil, notFoundErrorf("bootparams not found for component_id %s", componentID)
	}
	return toMap(resp.Items[0])
}
