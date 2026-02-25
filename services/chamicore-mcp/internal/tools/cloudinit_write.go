package tools

import (
	"context"
	"encoding/json"
	"strings"

	cloudclient "git.cscs.ch/openchami/chamicore-cloud-init/pkg/client"
	cloudtypes "git.cscs.ch/openchami/chamicore-cloud-init/pkg/types"
)

func (r *Runner) cloudInitPayloadsUpsert(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		ComponentID string          `json:"component_id"`
		Role        *string         `json:"role,omitempty"`
		UserData    string          `json:"user_data"`
		MetaData    json.RawMessage `json:"meta_data,omitempty"`
		NetworkData *string         `json:"network_data,omitempty"`
		VendorData  *string         `json:"vendor_data,omitempty"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}

	componentID := strings.TrimSpace(req.ComponentID)
	userData := req.UserData
	if componentID == "" {
		return nil, validationErrorf("component_id is required")
	}
	if strings.TrimSpace(userData) == "" {
		return nil, validationErrorf("user_data is required")
	}

	existing, err := r.cloudInit.List(ctx, cloudclient.ListOptions{
		ComponentID: componentID,
		Limit:       1,
		Offset:      0,
	})
	if err != nil {
		return nil, mapExecutionError(err, "loading existing cloud-init payload")
	}

	if existing == nil || len(existing.Items) == 0 {
		createReq := cloudtypes.CreatePayloadRequest{
			ComponentID: componentID,
			UserData:    userData,
			MetaData:    cloneJSONRaw(req.MetaData),
		}
		if req.Role != nil {
			createReq.Role = strings.TrimSpace(*req.Role)
		}
		createReq.VendorData = pickVendorData(req.NetworkData, req.VendorData)

		created, createErr := r.cloudInit.Create(ctx, createReq)
		if createErr != nil {
			return nil, mapExecutionError(createErr, "creating cloud-init payload")
		}
		return toMap(created)
	}

	payloadID := strings.TrimSpace(existing.Items[0].Metadata.ID)
	if payloadID == "" {
		return nil, validationErrorf("existing cloud-init payload for component_id %s has empty id", componentID)
	}

	patchReq := cloudtypes.PatchPayloadRequest{
		UserData: &userData,
	}
	if req.Role != nil {
		role := strings.TrimSpace(*req.Role)
		patchReq.Role = &role
	}
	if req.MetaData != nil {
		meta := cloneJSONRaw(req.MetaData)
		patchReq.MetaData = &meta
	}
	if req.NetworkData != nil || req.VendorData != nil {
		vendorData := pickVendorData(req.NetworkData, req.VendorData)
		patchReq.VendorData = &vendorData
	}

	updated, patchErr := r.cloudInit.Patch(ctx, payloadID, patchReq)
	if patchErr != nil {
		return nil, mapExecutionError(patchErr, "updating cloud-init payload")
	}
	return toMap(updated)
}

func (r *Runner) cloudInitPayloadsDelete(ctx context.Context, args map[string]any) (map[string]any, error) {
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

	existing, err := r.cloudInit.List(ctx, cloudclient.ListOptions{
		ComponentID: componentID,
		Limit:       1,
		Offset:      0,
	})
	if err != nil {
		return nil, mapExecutionError(err, "loading existing cloud-init payload")
	}
	if existing == nil || len(existing.Items) == 0 {
		return nil, notFoundErrorf("cloud-init payload not found for component_id %s", componentID)
	}

	payloadID := strings.TrimSpace(existing.Items[0].Metadata.ID)
	if payloadID == "" {
		return nil, validationErrorf("existing cloud-init payload for component_id %s has empty id", componentID)
	}

	if deleteErr := r.cloudInit.Delete(ctx, payloadID); deleteErr != nil {
		return nil, mapExecutionError(deleteErr, "deleting cloud-init payload")
	}
	return map[string]any{
		"status":       "deleted",
		"component_id": componentID,
		"id":           payloadID,
	}, nil
}

func pickVendorData(networkData, vendorData *string) string {
	switch {
	case networkData != nil:
		return strings.TrimSpace(*networkData)
	case vendorData != nil:
		return strings.TrimSpace(*vendorData)
	default:
		return ""
	}
}

func cloneJSONRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	cloned := make(json.RawMessage, len(raw))
	copy(cloned, raw)
	return cloned
}
