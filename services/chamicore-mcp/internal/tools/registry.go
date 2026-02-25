// Package tools provides MCP tool execution backed by Chamicore typed clients.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	bssclient "git.cscs.ch/openchami/chamicore-bss/pkg/client"
	bsstypes "git.cscs.ch/openchami/chamicore-bss/pkg/types"
	cloudclient "git.cscs.ch/openchami/chamicore-cloud-init/pkg/client"
	cloudtypes "git.cscs.ch/openchami/chamicore-cloud-init/pkg/types"
	discoverytypes "git.cscs.ch/openchami/chamicore-discovery/pkg/types"
	"git.cscs.ch/openchami/chamicore-lib/httputil"
	baseclient "git.cscs.ch/openchami/chamicore-lib/httputil/client"
	powerclient "git.cscs.ch/openchami/chamicore-power/pkg/client"
	powertypes "git.cscs.ch/openchami/chamicore-power/pkg/types"
	smdclient "git.cscs.ch/openchami/chamicore-smd/pkg/client"
	smdtypes "git.cscs.ch/openchami/chamicore-smd/pkg/types"
)

const defaultHealthTimeout = 5 * time.Second

// Config configures client endpoints for MCP tools.
type Config struct {
	AuthURL      string
	SMDURL       string
	BSSURL       string
	CloudInitURL string
	DiscoveryURL string
	PowerURL     string
}

// Runner executes MCP tool calls.
type Runner struct {
	healthClient *http.Client
	healthTarget []serviceHealthTarget

	smd       smdClient
	bss       bssClient
	cloudInit cloudClient
	discovery discoveryToolClient
	power     powerClient
}

type serviceHealthTarget struct {
	Name string
	URL  string
}

type smdClient interface {
	ListComponents(ctx context.Context, opts smdclient.ComponentListOptions) (*httputil.ResourceList[smdtypes.Component], error)
	GetComponent(ctx context.Context, id string) (*httputil.Resource[smdtypes.Component], error)
	ListGroups(ctx context.Context, opts smdclient.GroupListOptions) (*httputil.ResourceList[smdtypes.Group], error)
	GetGroup(ctx context.Context, name string) (*httputil.Resource[smdtypes.Group], error)
	AddGroupMembers(ctx context.Context, name string, req smdtypes.AddMembersRequest) error
	RemoveGroupMember(ctx context.Context, name, componentID string) error
}

type bssClient interface {
	List(ctx context.Context, opts bssclient.ListOptions) (*httputil.ResourceList[bsstypes.BootParam], error)
	Create(ctx context.Context, req bsstypes.CreateBootParamRequest) (*httputil.Resource[bsstypes.BootParam], error)
	Patch(ctx context.Context, id string, req bsstypes.PatchBootParamRequest) (*httputil.Resource[bsstypes.BootParam], error)
	Delete(ctx context.Context, id string) error
}

type cloudClient interface {
	List(ctx context.Context, opts cloudclient.ListOptions) (*httputil.ResourceList[cloudtypes.Payload], error)
	Create(ctx context.Context, req cloudtypes.CreatePayloadRequest) (*httputil.Resource[cloudtypes.Payload], error)
	Patch(ctx context.Context, id string, req cloudtypes.PatchPayloadRequest) (*httputil.Resource[cloudtypes.Payload], error)
	Delete(ctx context.Context, id string) error
}

type powerClient interface {
	GetPowerStatus(ctx context.Context, opts powerclient.PowerStatusOptions) (*httputil.Resource[powertypes.PowerStatus], error)
	ListTransitions(ctx context.Context, opts powerclient.ListTransitionsOptions) (*httputil.ResourceList[powertypes.Transition], error)
	GetTransition(ctx context.Context, id string) (*httputil.Resource[powertypes.Transition], error)
	CreateTransition(ctx context.Context, req powertypes.CreateTransitionRequest) (*httputil.Resource[powertypes.Transition], error)
	AbortTransition(ctx context.Context, id string) (*httputil.Resource[powertypes.Transition], error)
	WaitTransition(ctx context.Context, id string, opts powerclient.WaitTransitionOptions) (*httputil.Resource[powertypes.Transition], error)
}

type discoveryToolClient interface {
	ListTargets(ctx context.Context, limit, offset int) (*httputil.ResourceList[discoverytypes.Target], error)
	GetTarget(ctx context.Context, id string) (*httputil.Resource[discoverytypes.Target], error)
	ListScans(ctx context.Context, limit, offset int) (*httputil.ResourceList[discoverytypes.ScanJob], error)
	GetScan(ctx context.Context, id string) (*httputil.Resource[discoverytypes.ScanJob], error)
	ListDrivers(ctx context.Context) (*httputil.ResourceList[discoverytypes.DriverInfo], error)
	CreateTarget(ctx context.Context, req discoverytypes.CreateTargetRequest) (*httputil.Resource[discoverytypes.Target], error)
	PatchTarget(ctx context.Context, id string, req discoverytypes.PatchTargetRequest) (*httputil.Resource[discoverytypes.Target], error)
	DeleteTarget(ctx context.Context, id string) error
	StartTargetScan(ctx context.Context, id string) (*httputil.Resource[discoverytypes.ScanJob], error)
	StartScan(ctx context.Context, req discoverytypes.StartScanRequest) (*httputil.Resource[discoverytypes.ScanJob], error)
	CancelScan(ctx context.Context, id string) error
}

// ToolError carries an HTTP-style status code and message for tool failures.
type ToolError struct {
	statusCode int
	message    string
}

// Error implements error.
func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.message)
}

// StatusCode returns the attached status code.
func (e *ToolError) StatusCode() int {
	if e == nil || e.statusCode == 0 {
		return http.StatusInternalServerError
	}
	return e.statusCode
}

// NewRunner creates an MCP tool runner backed by typed service clients.
func NewRunner(cfg Config, token string) (*Runner, error) {
	smd := smdclient.New(smdclient.Config{
		BaseURL: strings.TrimSpace(cfg.SMDURL),
		Token:   strings.TrimSpace(token),
	})

	bss, err := bssclient.New(bssclient.Config{
		BaseURL: strings.TrimSpace(cfg.BSSURL),
		Token:   strings.TrimSpace(token),
	})
	if err != nil {
		return nil, fmt.Errorf("initializing BSS client: %w", err)
	}

	cloudInit, err := cloudclient.New(cloudclient.Config{
		BaseURL: strings.TrimSpace(cfg.CloudInitURL),
		Token:   strings.TrimSpace(token),
	})
	if err != nil {
		return nil, fmt.Errorf("initializing Cloud-Init client: %w", err)
	}

	power, err := powerclient.New(powerclient.Config{
		BaseURL: strings.TrimSpace(cfg.PowerURL),
		Token:   strings.TrimSpace(token),
	})
	if err != nil {
		return nil, fmt.Errorf("initializing power client: %w", err)
	}

	discovery := newDiscoveryClient(strings.TrimSpace(cfg.DiscoveryURL), strings.TrimSpace(token))

	return &Runner{
		healthClient: &http.Client{Timeout: defaultHealthTimeout},
		healthTarget: []serviceHealthTarget{
			{Name: "auth", URL: strings.TrimSpace(cfg.AuthURL)},
			{Name: "smd", URL: strings.TrimSpace(cfg.SMDURL)},
			{Name: "bss", URL: strings.TrimSpace(cfg.BSSURL)},
			{Name: "cloud-init", URL: strings.TrimSpace(cfg.CloudInitURL)},
			{Name: "discovery", URL: strings.TrimSpace(cfg.DiscoveryURL)},
			{Name: "power", URL: strings.TrimSpace(cfg.PowerURL)},
		},
		smd:       smd,
		bss:       bss,
		cloudInit: cloudInit,
		discovery: discovery,
		power:     power,
	}, nil
}

// Call executes one MCP tool by name and returns JSON-like map content.
func (r *Runner) Call(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	switch strings.TrimSpace(name) {
	case "cluster.health_summary":
		return r.clusterHealthSummary(ctx, args)

	case "smd.components.list":
		return r.smdComponentsList(ctx, args)
	case "smd.components.get":
		return r.smdComponentsGet(ctx, args)
	case "smd.groups.list":
		return r.smdGroupsList(ctx, args)
	case "smd.groups.get":
		return r.smdGroupsGet(ctx, args)
	case "smd.groups.members.add":
		return r.smdGroupsMembersAdd(ctx, args)
	case "smd.groups.members.remove":
		return r.smdGroupsMembersRemove(ctx, args)

	case "bss.bootparams.list":
		return r.bssBootParamsList(ctx, args)
	case "bss.bootparams.get":
		return r.bssBootParamsGet(ctx, args)
	case "bss.bootparams.upsert":
		return r.bssBootParamsUpsert(ctx, args)
	case "bss.bootparams.delete":
		return r.bssBootParamsDelete(ctx, args)

	case "cloudinit.payloads.list":
		return r.cloudInitPayloadsList(ctx, args)
	case "cloudinit.payloads.get":
		return r.cloudInitPayloadsGet(ctx, args)
	case "cloudinit.payloads.upsert":
		return r.cloudInitPayloadsUpsert(ctx, args)
	case "cloudinit.payloads.delete":
		return r.cloudInitPayloadsDelete(ctx, args)

	case "power.status.get":
		return r.powerStatusGet(ctx, args)
	case "power.transitions.list":
		return r.powerTransitionsList(ctx, args)
	case "power.transitions.get":
		return r.powerTransitionsGet(ctx, args)
	case "power.transitions.create":
		return r.powerTransitionsCreate(ctx, args)
	case "power.transitions.abort":
		return r.powerTransitionsAbort(ctx, args)
	case "power.transitions.wait":
		return r.powerTransitionsWait(ctx, args)

	case "discovery.targets.list":
		return r.discoveryTargetsList(ctx, args)
	case "discovery.targets.get":
		return r.discoveryTargetsGet(ctx, args)
	case "discovery.scans.list":
		return r.discoveryScansList(ctx, args)
	case "discovery.scans.get":
		return r.discoveryScansGet(ctx, args)
	case "discovery.drivers.list":
		return r.discoveryDriversList(ctx, args)
	case "discovery.targets.create":
		return r.discoveryTargetsCreate(ctx, args)
	case "discovery.targets.update":
		return r.discoveryTargetsUpdate(ctx, args)
	case "discovery.targets.delete":
		return r.discoveryTargetsDelete(ctx, args)
	case "discovery.target.scan":
		return r.discoveryTargetScan(ctx, args)
	case "discovery.scan.trigger":
		return r.discoveryScanTrigger(ctx, args)
	case "discovery.scan.delete":
		return r.discoveryScanDelete(ctx, args)

	default:
		return nil, validationErrorf("tool %s is not implemented", strings.TrimSpace(name))
	}
}

func validationErrorf(format string, args ...any) error {
	return &ToolError{
		statusCode: http.StatusBadRequest,
		message:    fmt.Sprintf(format, args...),
	}
}

func notFoundErrorf(format string, args ...any) error {
	return &ToolError{
		statusCode: http.StatusNotFound,
		message:    fmt.Sprintf(format, args...),
	}
}

func mapExecutionError(err error, fallback string) error {
	if err == nil {
		return nil
	}
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr
	}
	var apiErr *baseclient.APIError
	if errors.As(err, &apiErr) {
		detail := strings.TrimSpace(apiErr.Problem.Detail)
		if detail == "" {
			detail = strings.TrimSpace(apiErr.Problem.Title)
		}
		if detail == "" {
			detail = fallback
		}
		return &ToolError{
			statusCode: apiErr.StatusCode,
			message:    detail,
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &ToolError{
			statusCode: http.StatusGatewayTimeout,
			message:    fallback + ": request timed out",
		}
	}
	if errors.Is(err, context.Canceled) {
		return &ToolError{
			statusCode: http.StatusRequestTimeout,
			message:    fallback + ": request canceled",
		}
	}
	return &ToolError{
		statusCode: http.StatusInternalServerError,
		message:    fmt.Sprintf("%s: %v", fallback, err),
	}
}

func decodeArgsStrict(args map[string]any, out any) error {
	if args == nil {
		args = map[string]any{}
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return validationErrorf("invalid tool arguments: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return validationErrorf("invalid tool arguments: %v", err)
	}
	if decoder.More() {
		return validationErrorf("tool arguments must be a single JSON object")
	}
	return nil
}

func toMap(v any) (map[string]any, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encoding tool response: %w", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, fmt.Errorf("decoding tool response: %w", err)
	}
	return decoded, nil
}
