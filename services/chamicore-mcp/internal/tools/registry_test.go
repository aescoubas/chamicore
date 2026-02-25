package tools

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

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
	"github.com/stretchr/testify/require"
)

type mockSMD struct {
	listComponentsFn func(context.Context, smdclient.ComponentListOptions) (*httputil.ResourceList[smdtypes.Component], error)
	getComponentFn   func(context.Context, string) (*httputil.Resource[smdtypes.Component], error)
	listGroupsFn     func(context.Context, smdclient.GroupListOptions) (*httputil.ResourceList[smdtypes.Group], error)
	getGroupFn       func(context.Context, string) (*httputil.Resource[smdtypes.Group], error)
}

func (m mockSMD) ListComponents(ctx context.Context, opts smdclient.ComponentListOptions) (*httputil.ResourceList[smdtypes.Component], error) {
	return m.listComponentsFn(ctx, opts)
}
func (m mockSMD) GetComponent(ctx context.Context, id string) (*httputil.Resource[smdtypes.Component], error) {
	return m.getComponentFn(ctx, id)
}
func (m mockSMD) ListGroups(ctx context.Context, opts smdclient.GroupListOptions) (*httputil.ResourceList[smdtypes.Group], error) {
	return m.listGroupsFn(ctx, opts)
}
func (m mockSMD) GetGroup(ctx context.Context, name string) (*httputil.Resource[smdtypes.Group], error) {
	return m.getGroupFn(ctx, name)
}

type mockBSS struct {
	listFn func(context.Context, bssclient.ListOptions) (*httputil.ResourceList[bsstypes.BootParam], error)
}

func (m mockBSS) List(ctx context.Context, opts bssclient.ListOptions) (*httputil.ResourceList[bsstypes.BootParam], error) {
	return m.listFn(ctx, opts)
}

type mockCloud struct {
	listFn func(context.Context, cloudclient.ListOptions) (*httputil.ResourceList[cloudtypes.Payload], error)
}

func (m mockCloud) List(ctx context.Context, opts cloudclient.ListOptions) (*httputil.ResourceList[cloudtypes.Payload], error) {
	return m.listFn(ctx, opts)
}

type mockPower struct {
	getStatusFn func(context.Context, powerclient.PowerStatusOptions) (*httputil.Resource[powertypes.PowerStatus], error)
	listFn      func(context.Context, powerclient.ListTransitionsOptions) (*httputil.ResourceList[powertypes.Transition], error)
	getFn       func(context.Context, string) (*httputil.Resource[powertypes.Transition], error)
}

func (m mockPower) GetPowerStatus(ctx context.Context, opts powerclient.PowerStatusOptions) (*httputil.Resource[powertypes.PowerStatus], error) {
	return m.getStatusFn(ctx, opts)
}
func (m mockPower) ListTransitions(ctx context.Context, opts powerclient.ListTransitionsOptions) (*httputil.ResourceList[powertypes.Transition], error) {
	return m.listFn(ctx, opts)
}
func (m mockPower) GetTransition(ctx context.Context, id string) (*httputil.Resource[powertypes.Transition], error) {
	return m.getFn(ctx, id)
}

type mockDiscovery struct {
	listTargetsFn func(context.Context, int, int) (*httputil.ResourceList[discoverytypes.Target], error)
	getTargetFn   func(context.Context, string) (*httputil.Resource[discoverytypes.Target], error)
	listScansFn   func(context.Context, int, int) (*httputil.ResourceList[discoverytypes.ScanJob], error)
	getScanFn     func(context.Context, string) (*httputil.Resource[discoverytypes.ScanJob], error)
	listDriversFn func(context.Context) (*httputil.ResourceList[discoverytypes.DriverInfo], error)
}

func (m mockDiscovery) ListTargets(ctx context.Context, limit, offset int) (*httputil.ResourceList[discoverytypes.Target], error) {
	return m.listTargetsFn(ctx, limit, offset)
}
func (m mockDiscovery) GetTarget(ctx context.Context, id string) (*httputil.Resource[discoverytypes.Target], error) {
	return m.getTargetFn(ctx, id)
}
func (m mockDiscovery) ListScans(ctx context.Context, limit, offset int) (*httputil.ResourceList[discoverytypes.ScanJob], error) {
	return m.listScansFn(ctx, limit, offset)
}
func (m mockDiscovery) GetScan(ctx context.Context, id string) (*httputil.Resource[discoverytypes.ScanJob], error) {
	return m.getScanFn(ctx, id)
}
func (m mockDiscovery) ListDrivers(ctx context.Context) (*httputil.ResourceList[discoverytypes.DriverInfo], error) {
	return m.listDriversFn(ctx)
}

func TestCall_ValidationError(t *testing.T) {
	runner := newMockRunner()

	_, err := runner.Call(context.Background(), "smd.components.get", map[string]any{})
	require.Error(t, err)
	var toolErr *ToolError
	require.ErrorAs(t, err, &toolErr)
	require.Equal(t, 400, toolErr.StatusCode())
}

func TestCall_DownstreamErrorMapping(t *testing.T) {
	runner := newMockRunner()
	runner.smd = mockSMD{
		listComponentsFn: runner.smd.ListComponents,
		getComponentFn: func(context.Context, string) (*httputil.Resource[smdtypes.Component], error) {
			return nil, &baseclient.APIError{
				StatusCode: 404,
				Problem: httputil.ProblemDetail{
					Status: 404,
					Detail: "component not found",
				},
			}
		},
		listGroupsFn: runner.smd.ListGroups,
		getGroupFn:   runner.smd.GetGroup,
	}

	_, err := runner.Call(context.Background(), "smd.components.get", map[string]any{"id": "x0"})
	require.Error(t, err)
	var toolErr *ToolError
	require.ErrorAs(t, err, &toolErr)
	require.Equal(t, 404, toolErr.StatusCode())
	require.Contains(t, toolErr.Error(), "component not found")
}

func TestCall_AllReadToolsSuccess(t *testing.T) {
	runner := newMockRunner()
	cases := []struct {
		name string
		args map[string]any
	}{
		{name: "cluster.health_summary", args: map[string]any{}},
		{name: "smd.components.list", args: map[string]any{"limit": 10, "offset": 0}},
		{name: "smd.components.get", args: map[string]any{"id": "x0"}},
		{name: "smd.groups.list", args: map[string]any{"limit": 10, "offset": 0}},
		{name: "smd.groups.get", args: map[string]any{"label": "g"}},
		{name: "bss.bootparams.list", args: map[string]any{"limit": 10, "offset": 0}},
		{name: "bss.bootparams.get", args: map[string]any{"component_id": "x0"}},
		{name: "cloudinit.payloads.list", args: map[string]any{"limit": 10, "offset": 0}},
		{name: "cloudinit.payloads.get", args: map[string]any{"component_id": "x0"}},
		{name: "power.status.get", args: map[string]any{"nodes": []string{"x0"}}},
		{name: "power.transitions.list", args: map[string]any{"limit": 10, "offset": 0}},
		{name: "power.transitions.get", args: map[string]any{"id": "t1"}},
		{name: "discovery.targets.list", args: map[string]any{"limit": 10, "offset": 0}},
		{name: "discovery.targets.get", args: map[string]any{"id": "d1"}},
		{name: "discovery.scans.list", args: map[string]any{"limit": 10, "offset": 0}},
		{name: "discovery.scans.get", args: map[string]any{"id": "s1"}},
		{name: "discovery.drivers.list", args: map[string]any{}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result, err := runner.Call(context.Background(), tc.name, tc.args)
			require.NoError(t, err)
			require.NotEmpty(t, result)
		})
	}
}

func newMockRunner() *Runner {
	return &Runner{
		healthClient: newTestHealthClient(),
		healthTarget: []serviceHealthTarget{
			{Name: "smd", URL: "http://health.local"},
		},
		smd: mockSMD{
			listComponentsFn: func(context.Context, smdclient.ComponentListOptions) (*httputil.ResourceList[smdtypes.Component], error) {
				return &httputil.ResourceList[smdtypes.Component]{Kind: "ComponentList", APIVersion: "hsm/v2"}, nil
			},
			getComponentFn: func(context.Context, string) (*httputil.Resource[smdtypes.Component], error) {
				return &httputil.Resource[smdtypes.Component]{Kind: "Component", APIVersion: "hsm/v2"}, nil
			},
			listGroupsFn: func(context.Context, smdclient.GroupListOptions) (*httputil.ResourceList[smdtypes.Group], error) {
				return &httputil.ResourceList[smdtypes.Group]{Kind: "GroupList", APIVersion: "hsm/v2"}, nil
			},
			getGroupFn: func(context.Context, string) (*httputil.Resource[smdtypes.Group], error) {
				return &httputil.Resource[smdtypes.Group]{Kind: "Group", APIVersion: "hsm/v2"}, nil
			},
		},
		bss: mockBSS{
			listFn: func(_ context.Context, opts bssclient.ListOptions) (*httputil.ResourceList[bsstypes.BootParam], error) {
				items := []httputil.Resource[bsstypes.BootParam]{}
				if strings.TrimSpace(opts.ComponentID) != "" {
					items = append(items, httputil.Resource[bsstypes.BootParam]{Kind: "BootParam", APIVersion: "boot/v1"})
				}
				return &httputil.ResourceList[bsstypes.BootParam]{Kind: "BootParamList", APIVersion: "boot/v1", Items: items}, nil
			},
		},
		cloudInit: mockCloud{
			listFn: func(_ context.Context, opts cloudclient.ListOptions) (*httputil.ResourceList[cloudtypes.Payload], error) {
				items := []httputil.Resource[cloudtypes.Payload]{}
				if strings.TrimSpace(opts.ComponentID) != "" {
					items = append(items, httputil.Resource[cloudtypes.Payload]{Kind: "Payload", APIVersion: "cloud-init/v1"})
				}
				return &httputil.ResourceList[cloudtypes.Payload]{Kind: "PayloadList", APIVersion: "cloud-init/v1", Items: items}, nil
			},
		},
		power: mockPower{
			getStatusFn: func(context.Context, powerclient.PowerStatusOptions) (*httputil.Resource[powertypes.PowerStatus], error) {
				return &httputil.Resource[powertypes.PowerStatus]{Kind: "PowerStatus", APIVersion: "power/v1"}, nil
			},
			listFn: func(context.Context, powerclient.ListTransitionsOptions) (*httputil.ResourceList[powertypes.Transition], error) {
				return &httputil.ResourceList[powertypes.Transition]{Kind: "TransitionList", APIVersion: "power/v1"}, nil
			},
			getFn: func(context.Context, string) (*httputil.Resource[powertypes.Transition], error) {
				return &httputil.Resource[powertypes.Transition]{Kind: "Transition", APIVersion: "power/v1"}, nil
			},
		},
		discovery: mockDiscovery{
			listTargetsFn: func(context.Context, int, int) (*httputil.ResourceList[discoverytypes.Target], error) {
				return &httputil.ResourceList[discoverytypes.Target]{Kind: "TargetList", APIVersion: "discovery/v1"}, nil
			},
			getTargetFn: func(context.Context, string) (*httputil.Resource[discoverytypes.Target], error) {
				return &httputil.Resource[discoverytypes.Target]{Kind: "Target", APIVersion: "discovery/v1"}, nil
			},
			listScansFn: func(context.Context, int, int) (*httputil.ResourceList[discoverytypes.ScanJob], error) {
				return &httputil.ResourceList[discoverytypes.ScanJob]{Kind: "ScanList", APIVersion: "discovery/v1"}, nil
			},
			getScanFn: func(context.Context, string) (*httputil.Resource[discoverytypes.ScanJob], error) {
				return &httputil.Resource[discoverytypes.ScanJob]{Kind: "Scan", APIVersion: "discovery/v1"}, nil
			},
			listDriversFn: func(context.Context) (*httputil.ResourceList[discoverytypes.DriverInfo], error) {
				return &httputil.ResourceList[discoverytypes.DriverInfo]{Kind: "DriverList", APIVersion: "discovery/v1"}, nil
			},
		},
	}
}

func newTestHealthClient() *http.Client {
	return &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			payload := `{"status":"ok"}`
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(payload)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
