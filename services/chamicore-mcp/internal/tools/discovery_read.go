package tools

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	discoverytypes "git.cscs.ch/openchami/chamicore-discovery/pkg/types"
	"git.cscs.ch/openchami/chamicore-lib/httputil"
	baseclient "git.cscs.ch/openchami/chamicore-lib/httputil/client"
)

type discoveryClient struct {
	http *baseclient.Client
}

func newDiscoveryClient(baseURL, token string) *discoveryClient {
	return &discoveryClient{
		http: baseclient.New(baseclient.Config{
			BaseURL: strings.TrimSpace(baseURL),
			Token:   strings.TrimSpace(token),
		}),
	}
}

func (c *discoveryClient) ListTargets(ctx context.Context, limit, offset int) (*httputil.ResourceList[discoverytypes.Target], error) {
	path := "/discovery/v1/targets"
	params := url.Values{}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}
	if encoded := params.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var response httputil.ResourceList[discoverytypes.Target]
	if err := c.http.Get(ctx, path, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *discoveryClient) GetTarget(ctx context.Context, id string) (*httputil.Resource[discoverytypes.Target], error) {
	var response httputil.Resource[discoverytypes.Target]
	if err := c.http.Get(ctx, fmt.Sprintf("/discovery/v1/targets/%s", url.PathEscape(strings.TrimSpace(id))), &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *discoveryClient) ListScans(ctx context.Context, limit, offset int) (*httputil.ResourceList[discoverytypes.ScanJob], error) {
	path := "/discovery/v1/scans"
	params := url.Values{}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}
	if encoded := params.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var response httputil.ResourceList[discoverytypes.ScanJob]
	if err := c.http.Get(ctx, path, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *discoveryClient) GetScan(ctx context.Context, id string) (*httputil.Resource[discoverytypes.ScanJob], error) {
	var response httputil.Resource[discoverytypes.ScanJob]
	if err := c.http.Get(ctx, fmt.Sprintf("/discovery/v1/scans/%s", url.PathEscape(strings.TrimSpace(id))), &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *discoveryClient) ListDrivers(ctx context.Context) (*httputil.ResourceList[discoverytypes.DriverInfo], error) {
	var response httputil.ResourceList[discoverytypes.DriverInfo]
	if err := c.http.Get(ctx, "/discovery/v1/drivers", &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (r *Runner) discoveryTargetsList(ctx context.Context, args map[string]any) (map[string]any, error) {
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

	resp, err := r.discovery.ListTargets(ctx, req.Limit, req.Offset)
	if err != nil {
		return nil, mapExecutionError(err, "listing discovery targets")
	}
	return toMap(resp)
}

func (r *Runner) discoveryTargetsGet(ctx context.Context, args map[string]any) (map[string]any, error) {
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
	resp, err := r.discovery.GetTarget(ctx, id)
	if err != nil {
		return nil, mapExecutionError(err, "getting discovery target")
	}
	return toMap(resp)
}

func (r *Runner) discoveryScansList(ctx context.Context, args map[string]any) (map[string]any, error) {
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

	resp, err := r.discovery.ListScans(ctx, req.Limit, req.Offset)
	if err != nil {
		return nil, mapExecutionError(err, "listing discovery scans")
	}
	return toMap(resp)
}

func (r *Runner) discoveryScansGet(ctx context.Context, args map[string]any) (map[string]any, error) {
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
	resp, err := r.discovery.GetScan(ctx, id)
	if err != nil {
		return nil, mapExecutionError(err, "getting discovery scan")
	}
	return toMap(resp)
}

func (r *Runner) discoveryDriversList(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct{}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}
	resp, err := r.discovery.ListDrivers(ctx)
	if err != nil {
		return nil, mapExecutionError(err, "listing discovery drivers")
	}
	return toMap(resp)
}
