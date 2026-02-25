package tools

import (
	"context"
	"strings"

	bssclient "git.cscs.ch/openchami/chamicore-bss/pkg/client"
	bsstypes "git.cscs.ch/openchami/chamicore-bss/pkg/types"
)

func (r *Runner) bssBootParamsUpsert(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		ComponentID string   `json:"component_id"`
		Role        *string  `json:"role,omitempty"`
		Kernel      string   `json:"kernel"`
		Initrd      string   `json:"initrd"`
		Params      []string `json:"params,omitempty"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}

	componentID := strings.TrimSpace(req.ComponentID)
	kernel := strings.TrimSpace(req.Kernel)
	initrd := strings.TrimSpace(req.Initrd)
	if componentID == "" {
		return nil, validationErrorf("component_id is required")
	}
	if kernel == "" {
		return nil, validationErrorf("kernel is required")
	}
	if initrd == "" {
		return nil, validationErrorf("initrd is required")
	}

	existing, err := r.bss.List(ctx, bssclient.ListOptions{
		ComponentID: componentID,
		Limit:       1,
		Offset:      0,
	})
	if err != nil {
		return nil, mapExecutionError(err, "loading existing BSS bootparams")
	}

	if existing == nil || len(existing.Items) == 0 {
		createReq := bsstypes.CreateBootParamRequest{
			ComponentID: componentID,
			KernelURI:   kernel,
			InitrdURI:   initrd,
		}
		if req.Role != nil {
			createReq.Role = strings.TrimSpace(*req.Role)
		}
		if req.Params != nil {
			createReq.Cmdline = strings.Join(trimStringList(req.Params), " ")
		}

		created, createErr := r.bss.Create(ctx, createReq)
		if createErr != nil {
			return nil, mapExecutionError(createErr, "creating BSS bootparams")
		}
		return toMap(created)
	}

	item := existing.Items[0]
	bootParamID := strings.TrimSpace(item.Metadata.ID)
	if bootParamID == "" {
		return nil, validationErrorf("existing bootparams for component_id %s has empty id", componentID)
	}

	patchReq := bsstypes.PatchBootParamRequest{
		KernelURI: &kernel,
		InitrdURI: &initrd,
	}
	if req.Role != nil {
		role := strings.TrimSpace(*req.Role)
		patchReq.Role = &role
	}
	if req.Params != nil {
		cmdline := strings.Join(trimStringList(req.Params), " ")
		patchReq.Cmdline = &cmdline
	}

	updated, patchErr := r.bss.Patch(ctx, bootParamID, patchReq)
	if patchErr != nil {
		return nil, mapExecutionError(patchErr, "updating BSS bootparams")
	}
	return toMap(updated)
}

func (r *Runner) bssBootParamsDelete(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		ComponentID string `json:"component_id"`
		Confirm     *bool  `json:"confirm,omitempty"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	componentID := strings.TrimSpace(req.ComponentID)
	if componentID == "" {
		return nil, validationErrorf("component_id is required")
	}

	existing, err := r.bss.List(ctx, bssclient.ListOptions{
		ComponentID: componentID,
		Limit:       1,
		Offset:      0,
	})
	if err != nil {
		return nil, mapExecutionError(err, "loading existing BSS bootparams")
	}
	if existing == nil || len(existing.Items) == 0 {
		return nil, notFoundErrorf("bootparams not found for component_id %s", componentID)
	}

	bootParamID := strings.TrimSpace(existing.Items[0].Metadata.ID)
	if bootParamID == "" {
		return nil, validationErrorf("existing bootparams for component_id %s has empty id", componentID)
	}

	if deleteErr := r.bss.Delete(ctx, bootParamID); deleteErr != nil {
		return nil, mapExecutionError(deleteErr, "deleting BSS bootparams")
	}
	return map[string]any{
		"status":       "deleted",
		"component_id": componentID,
		"id":           bootParamID,
	}, nil
}
