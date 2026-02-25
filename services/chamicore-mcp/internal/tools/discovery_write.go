package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	discoverytypes "git.cscs.ch/openchami/chamicore-discovery/pkg/types"
	"git.cscs.ch/openchami/chamicore-lib/httputil"
)

func (c *discoveryClient) CreateTarget(ctx context.Context, req discoverytypes.CreateTargetRequest) (*httputil.Resource[discoverytypes.Target], error) {
	var response httputil.Resource[discoverytypes.Target]
	if err := c.http.Post(ctx, "/discovery/v1/targets", req, &response); err != nil {
		return nil, fmt.Errorf("creating discovery target: %w", err)
	}
	return &response, nil
}

func (c *discoveryClient) PatchTarget(ctx context.Context, id string, req discoverytypes.PatchTargetRequest) (*httputil.Resource[discoverytypes.Target], error) {
	targetID := strings.TrimSpace(id)
	if targetID == "" {
		return nil, fmt.Errorf("target id is required")
	}
	var response httputil.Resource[discoverytypes.Target]
	if err := c.http.Patch(ctx, fmt.Sprintf("/discovery/v1/targets/%s", url.PathEscape(targetID)), req, &response); err != nil {
		return nil, fmt.Errorf("updating discovery target: %w", err)
	}
	return &response, nil
}

func (c *discoveryClient) DeleteTarget(ctx context.Context, id string) error {
	targetID := strings.TrimSpace(id)
	if targetID == "" {
		return fmt.Errorf("target id is required")
	}
	if err := c.http.Delete(ctx, fmt.Sprintf("/discovery/v1/targets/%s", url.PathEscape(targetID))); err != nil {
		return fmt.Errorf("deleting discovery target: %w", err)
	}
	return nil
}

func (c *discoveryClient) StartTargetScan(ctx context.Context, id string) (*httputil.Resource[discoverytypes.ScanJob], error) {
	targetID := strings.TrimSpace(id)
	if targetID == "" {
		return nil, fmt.Errorf("target id is required")
	}
	var response httputil.Resource[discoverytypes.ScanJob]
	if err := c.http.Post(ctx, fmt.Sprintf("/discovery/v1/targets/%s/scan", url.PathEscape(targetID)), struct{}{}, &response); err != nil {
		return nil, fmt.Errorf("starting target scan: %w", err)
	}
	return &response, nil
}

func (c *discoveryClient) StartScan(ctx context.Context, req discoverytypes.StartScanRequest) (*httputil.Resource[discoverytypes.ScanJob], error) {
	var response httputil.Resource[discoverytypes.ScanJob]
	if err := c.http.Post(ctx, "/discovery/v1/scans", req, &response); err != nil {
		return nil, fmt.Errorf("starting discovery scan: %w", err)
	}
	return &response, nil
}

func (c *discoveryClient) CancelScan(ctx context.Context, id string) error {
	scanID := strings.TrimSpace(id)
	if scanID == "" {
		return fmt.Errorf("scan id is required")
	}
	if err := c.http.Delete(ctx, fmt.Sprintf("/discovery/v1/scans/%s", url.PathEscape(scanID))); err != nil {
		return fmt.Errorf("deleting discovery scan: %w", err)
	}
	return nil
}

func (r *Runner) discoveryTargetsCreate(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		Name               string `json:"name"`
		Endpoint           string `json:"endpoint"`
		Driver             string `json:"driver"`
		CredentialID       string `json:"credential_id,omitempty"`
		InsecureSkipVerify *bool  `json:"insecure_skip_verify,omitempty"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)
	endpoint := strings.TrimSpace(req.Endpoint)
	driverName := strings.ToLower(strings.TrimSpace(req.Driver))
	if name == "" {
		return nil, validationErrorf("name is required")
	}
	if endpoint == "" {
		return nil, validationErrorf("endpoint is required")
	}
	if driverName == "" {
		return nil, validationErrorf("driver is required")
	}

	resource, err := r.discovery.CreateTarget(ctx, discoverytypes.CreateTargetRequest{
		Name:         name,
		Driver:       driverName,
		Addresses:    []string{endpoint},
		CredentialID: strings.TrimSpace(req.CredentialID),
	})
	if err != nil {
		return nil, mapExecutionError(err, "creating discovery target")
	}

	result, err := toMap(resource)
	if err != nil {
		return nil, err
	}
	if req.InsecureSkipVerify != nil {
		result["warning"] = "insecure_skip_verify is accepted for compatibility but not persisted in discovery target schema"
	}
	return result, nil
}

func (r *Runner) discoveryTargetsUpdate(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		ID                 string  `json:"id"`
		Name               *string `json:"name,omitempty"`
		Endpoint           *string `json:"endpoint,omitempty"`
		Driver             *string `json:"driver,omitempty"`
		CredentialID       *string `json:"credential_id,omitempty"`
		InsecureSkipVerify *bool   `json:"insecure_skip_verify,omitempty"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}

	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, validationErrorf("id is required")
	}

	patchReq := discoverytypes.PatchTargetRequest{}
	changed := false
	if req.Name != nil {
		value := strings.TrimSpace(*req.Name)
		patchReq.Name = &value
		changed = true
	}
	if req.Endpoint != nil {
		endpoint := strings.TrimSpace(*req.Endpoint)
		addresses := []string{endpoint}
		patchReq.Addresses = &addresses
		changed = true
	}
	if req.Driver != nil {
		driverName := strings.ToLower(strings.TrimSpace(*req.Driver))
		patchReq.Driver = &driverName
		changed = true
	}
	if req.CredentialID != nil {
		credentialID := strings.TrimSpace(*req.CredentialID)
		patchReq.CredentialID = &credentialID
		changed = true
	}
	if req.InsecureSkipVerify != nil {
		changed = true
	}
	if !changed {
		return nil, validationErrorf("at least one mutable field must be provided")
	}
	if patchReq.Addresses != nil && len(*patchReq.Addresses) > 0 && strings.TrimSpace((*patchReq.Addresses)[0]) == "" {
		return nil, validationErrorf("endpoint cannot be empty")
	}

	resource, err := r.discovery.PatchTarget(ctx, id, patchReq)
	if err != nil {
		return nil, mapExecutionError(err, "updating discovery target")
	}

	result, err := toMap(resource)
	if err != nil {
		return nil, err
	}
	if req.InsecureSkipVerify != nil {
		result["warning"] = "insecure_skip_verify is accepted for compatibility but not persisted in discovery target schema"
	}
	return result, nil
}

func (r *Runner) discoveryTargetsDelete(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		ID      string `json:"id"`
		Confirm *bool  `json:"confirm,omitempty"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, validationErrorf("id is required")
	}

	if err := r.discovery.DeleteTarget(ctx, id); err != nil {
		return nil, mapExecutionError(err, "deleting discovery target")
	}
	return map[string]any{
		"status": "deleted",
		"id":     id,
	}, nil
}

func (r *Runner) discoveryTargetScan(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		ID    string `json:"id"`
		Async *bool  `json:"async,omitempty"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, validationErrorf("id is required")
	}

	resource, err := r.discovery.StartTargetScan(ctx, id)
	if err != nil {
		return nil, mapExecutionError(err, "starting discovery target scan")
	}

	result, err := toMap(resource)
	if err != nil {
		return nil, err
	}
	if req.Async != nil {
		result["requested_async"] = *req.Async
	}
	return result, nil
}

func (r *Runner) discoveryScanTrigger(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		Driver             string `json:"driver"`
		Endpoint           string `json:"endpoint,omitempty"`
		CredentialID       string `json:"credential_id,omitempty"`
		InsecureSkipVerify *bool  `json:"insecure_skip_verify,omitempty"`
		TimeoutSeconds     *int   `json:"timeout_seconds,omitempty"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}

	driverName := strings.ToLower(strings.TrimSpace(req.Driver))
	if driverName == "" {
		return nil, validationErrorf("driver is required")
	}
	if req.TimeoutSeconds != nil && *req.TimeoutSeconds <= 0 {
		return nil, validationErrorf("timeout_seconds must be > 0")
	}

	scanReq := discoverytypes.StartScanRequest{
		Driver:       driverName,
		CredentialID: strings.TrimSpace(req.CredentialID),
	}
	if endpoint := strings.TrimSpace(req.Endpoint); endpoint != "" {
		scanReq.Addresses = []string{endpoint}
	}

	if req.InsecureSkipVerify != nil || req.TimeoutSeconds != nil {
		data := map[string]any{}
		if req.InsecureSkipVerify != nil {
			data["insecure_skip_verify"] = *req.InsecureSkipVerify
		}
		if req.TimeoutSeconds != nil {
			data["timeout_seconds"] = *req.TimeoutSeconds
		}
		encoded, err := json.Marshal(data)
		if err != nil {
			return nil, validationErrorf("invalid scan options: %v", err)
		}
		scanReq.Data = encoded
	}

	resource, err := r.discovery.StartScan(ctx, scanReq)
	if err != nil {
		return nil, mapExecutionError(err, "starting discovery scan")
	}
	return toMap(resource)
}

func (r *Runner) discoveryScanDelete(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		ID      string `json:"id"`
		Confirm *bool  `json:"confirm,omitempty"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, validationErrorf("id is required")
	}

	if err := r.discovery.CancelScan(ctx, id); err != nil {
		return nil, mapExecutionError(err, "deleting discovery scan")
	}
	return map[string]any{
		"status": "deleted",
		"id":     id,
	}, nil
}
