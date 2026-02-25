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
	addMembersFn     func(context.Context, string, smdtypes.AddMembersRequest) error
	removeMemberFn   func(context.Context, string, string) error
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

func (m mockSMD) AddGroupMembers(ctx context.Context, name string, req smdtypes.AddMembersRequest) error {
	return m.addMembersFn(ctx, name, req)
}

func (m mockSMD) RemoveGroupMember(ctx context.Context, name, componentID string) error {
	return m.removeMemberFn(ctx, name, componentID)
}

type mockBSS struct {
	listFn   func(context.Context, bssclient.ListOptions) (*httputil.ResourceList[bsstypes.BootParam], error)
	createFn func(context.Context, bsstypes.CreateBootParamRequest) (*httputil.Resource[bsstypes.BootParam], error)
	patchFn  func(context.Context, string, bsstypes.PatchBootParamRequest) (*httputil.Resource[bsstypes.BootParam], error)
	deleteFn func(context.Context, string) error
}

func (m mockBSS) List(ctx context.Context, opts bssclient.ListOptions) (*httputil.ResourceList[bsstypes.BootParam], error) {
	return m.listFn(ctx, opts)
}

func (m mockBSS) Create(ctx context.Context, req bsstypes.CreateBootParamRequest) (*httputil.Resource[bsstypes.BootParam], error) {
	return m.createFn(ctx, req)
}

func (m mockBSS) Patch(ctx context.Context, id string, req bsstypes.PatchBootParamRequest) (*httputil.Resource[bsstypes.BootParam], error) {
	return m.patchFn(ctx, id, req)
}

func (m mockBSS) Delete(ctx context.Context, id string) error {
	return m.deleteFn(ctx, id)
}

type mockCloud struct {
	listFn   func(context.Context, cloudclient.ListOptions) (*httputil.ResourceList[cloudtypes.Payload], error)
	createFn func(context.Context, cloudtypes.CreatePayloadRequest) (*httputil.Resource[cloudtypes.Payload], error)
	patchFn  func(context.Context, string, cloudtypes.PatchPayloadRequest) (*httputil.Resource[cloudtypes.Payload], error)
	deleteFn func(context.Context, string) error
}

func (m mockCloud) List(ctx context.Context, opts cloudclient.ListOptions) (*httputil.ResourceList[cloudtypes.Payload], error) {
	return m.listFn(ctx, opts)
}

func (m mockCloud) Create(ctx context.Context, req cloudtypes.CreatePayloadRequest) (*httputil.Resource[cloudtypes.Payload], error) {
	return m.createFn(ctx, req)
}

func (m mockCloud) Patch(ctx context.Context, id string, req cloudtypes.PatchPayloadRequest) (*httputil.Resource[cloudtypes.Payload], error) {
	return m.patchFn(ctx, id, req)
}

func (m mockCloud) Delete(ctx context.Context, id string) error {
	return m.deleteFn(ctx, id)
}

type mockPower struct {
	getStatusFn func(context.Context, powerclient.PowerStatusOptions) (*httputil.Resource[powertypes.PowerStatus], error)
	listFn      func(context.Context, powerclient.ListTransitionsOptions) (*httputil.ResourceList[powertypes.Transition], error)
	getFn       func(context.Context, string) (*httputil.Resource[powertypes.Transition], error)
	createFn    func(context.Context, powertypes.CreateTransitionRequest) (*httputil.Resource[powertypes.Transition], error)
	abortFn     func(context.Context, string) (*httputil.Resource[powertypes.Transition], error)
	waitFn      func(context.Context, string, powerclient.WaitTransitionOptions) (*httputil.Resource[powertypes.Transition], error)
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

func (m mockPower) CreateTransition(ctx context.Context, req powertypes.CreateTransitionRequest) (*httputil.Resource[powertypes.Transition], error) {
	return m.createFn(ctx, req)
}

func (m mockPower) AbortTransition(ctx context.Context, id string) (*httputil.Resource[powertypes.Transition], error) {
	return m.abortFn(ctx, id)
}

func (m mockPower) WaitTransition(ctx context.Context, id string, opts powerclient.WaitTransitionOptions) (*httputil.Resource[powertypes.Transition], error) {
	return m.waitFn(ctx, id, opts)
}

type mockDiscovery struct {
	listTargetsFn     func(context.Context, int, int) (*httputil.ResourceList[discoverytypes.Target], error)
	getTargetFn       func(context.Context, string) (*httputil.Resource[discoverytypes.Target], error)
	listScansFn       func(context.Context, int, int) (*httputil.ResourceList[discoverytypes.ScanJob], error)
	getScanFn         func(context.Context, string) (*httputil.Resource[discoverytypes.ScanJob], error)
	listDriversFn     func(context.Context) (*httputil.ResourceList[discoverytypes.DriverInfo], error)
	createTargetFn    func(context.Context, discoverytypes.CreateTargetRequest) (*httputil.Resource[discoverytypes.Target], error)
	patchTargetFn     func(context.Context, string, discoverytypes.PatchTargetRequest) (*httputil.Resource[discoverytypes.Target], error)
	deleteTargetFn    func(context.Context, string) error
	startTargetScanFn func(context.Context, string) (*httputil.Resource[discoverytypes.ScanJob], error)
	startScanFn       func(context.Context, discoverytypes.StartScanRequest) (*httputil.Resource[discoverytypes.ScanJob], error)
	cancelScanFn      func(context.Context, string) error
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

func (m mockDiscovery) CreateTarget(ctx context.Context, req discoverytypes.CreateTargetRequest) (*httputil.Resource[discoverytypes.Target], error) {
	return m.createTargetFn(ctx, req)
}

func (m mockDiscovery) PatchTarget(ctx context.Context, id string, req discoverytypes.PatchTargetRequest) (*httputil.Resource[discoverytypes.Target], error) {
	return m.patchTargetFn(ctx, id, req)
}

func (m mockDiscovery) DeleteTarget(ctx context.Context, id string) error {
	return m.deleteTargetFn(ctx, id)
}

func (m mockDiscovery) StartTargetScan(ctx context.Context, id string) (*httputil.Resource[discoverytypes.ScanJob], error) {
	return m.startTargetScanFn(ctx, id)
}

func (m mockDiscovery) StartScan(ctx context.Context, req discoverytypes.StartScanRequest) (*httputil.Resource[discoverytypes.ScanJob], error) {
	return m.startScanFn(ctx, req)
}

func (m mockDiscovery) CancelScan(ctx context.Context, id string) error {
	return m.cancelScanFn(ctx, id)
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
		listGroupsFn:   runner.smd.ListGroups,
		getGroupFn:     runner.smd.GetGroup,
		addMembersFn:   runner.smd.AddGroupMembers,
		removeMemberFn: runner.smd.RemoveGroupMember,
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

func TestCall_AllWriteToolsSuccess(t *testing.T) {
	runner := newMockRunner()
	cases := []struct {
		name string
		args map[string]any
	}{
		{name: "smd.groups.members.add", args: map[string]any{"label": "g", "members": []string{"x0"}}},
		{name: "smd.groups.members.remove", args: map[string]any{"label": "g", "members": []string{"x0"}}},
		{name: "bss.bootparams.upsert", args: map[string]any{"component_id": "x0", "kernel": "k", "initrd": "i"}},
		{name: "bss.bootparams.delete", args: map[string]any{"component_id": "x0", "confirm": true}},
		{name: "cloudinit.payloads.upsert", args: map[string]any{"component_id": "x0", "user_data": "#cloud-config"}},
		{name: "cloudinit.payloads.delete", args: map[string]any{"component_id": "x0", "confirm": true}},
		{name: "power.transitions.create", args: map[string]any{"operation": "On", "nodes": []string{"x0"}}},
		{name: "power.transitions.abort", args: map[string]any{"id": "t1", "confirm": true}},
		{name: "power.transitions.wait", args: map[string]any{"id": "t1", "timeout_seconds": 1, "poll_interval_seconds": 1}},
		{name: "discovery.targets.create", args: map[string]any{"name": "rack-1", "endpoint": "10.0.0.1", "driver": "redfish"}},
		{name: "discovery.targets.update", args: map[string]any{"id": "d1", "name": "rack-renamed"}},
		{name: "discovery.targets.delete", args: map[string]any{"id": "d1", "confirm": true}},
		{name: "discovery.target.scan", args: map[string]any{"id": "d1"}},
		{name: "discovery.scan.trigger", args: map[string]any{"driver": "redfish", "endpoint": "10.0.0.1"}},
		{name: "discovery.scan.delete", args: map[string]any{"id": "s1", "confirm": true}},
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

func TestCall_BSSBootParamsUpsertCreatePath(t *testing.T) {
	runner := newMockRunner()
	created := false
	runner.bss = mockBSS{
		listFn: func(context.Context, bssclient.ListOptions) (*httputil.ResourceList[bsstypes.BootParam], error) {
			return &httputil.ResourceList[bsstypes.BootParam]{Kind: "BootParamList", APIVersion: "boot/v1"}, nil
		},
		createFn: func(_ context.Context, req bsstypes.CreateBootParamRequest) (*httputil.Resource[bsstypes.BootParam], error) {
			created = true
			require.Equal(t, "x1000", req.ComponentID)
			require.Equal(t, "console=ttyS0 root=/dev/vda1", req.Cmdline)
			return &httputil.Resource[bsstypes.BootParam]{
				Kind:       "BootParam",
				APIVersion: "boot/v1",
				Metadata:   httputil.Metadata{ID: "bp-created"},
			}, nil
		},
		patchFn:  runner.bss.Patch,
		deleteFn: runner.bss.Delete,
	}

	result, err := runner.Call(context.Background(), "bss.bootparams.upsert", map[string]any{
		"component_id": "x1000",
		"kernel":       "http://k",
		"initrd":       "http://i",
		"params":       []string{"console=ttyS0", "root=/dev/vda1"},
	})
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "bp-created", result["metadata"].(map[string]any)["id"])
}

func TestCall_CloudInitUpsertCreatePathUsesNetworkData(t *testing.T) {
	runner := newMockRunner()
	created := false
	runner.cloudInit = mockCloud{
		listFn: func(context.Context, cloudclient.ListOptions) (*httputil.ResourceList[cloudtypes.Payload], error) {
			return &httputil.ResourceList[cloudtypes.Payload]{Kind: "PayloadList", APIVersion: "cloud-init/v1"}, nil
		},
		createFn: func(_ context.Context, req cloudtypes.CreatePayloadRequest) (*httputil.Resource[cloudtypes.Payload], error) {
			created = true
			require.Equal(t, "network-config", req.VendorData)
			return &httputil.Resource[cloudtypes.Payload]{
				Kind:       "Payload",
				APIVersion: "cloud-init/v1",
				Metadata:   httputil.Metadata{ID: "pl-created"},
			}, nil
		},
		patchFn:  runner.cloudInit.Patch,
		deleteFn: runner.cloudInit.Delete,
	}

	result, err := runner.Call(context.Background(), "cloudinit.payloads.upsert", map[string]any{
		"component_id": "x0",
		"user_data":    "#cloud-config",
		"network_data": "network-config",
	})
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "pl-created", result["metadata"].(map[string]any)["id"])
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
			getGroupFn: func(_ context.Context, name string) (*httputil.Resource[smdtypes.Group], error) {
				return &httputil.Resource[smdtypes.Group]{
					Kind:       "Group",
					APIVersion: "hsm/v2",
					Metadata:   httputil.Metadata{ID: name},
				}, nil
			},
			addMembersFn: func(context.Context, string, smdtypes.AddMembersRequest) error {
				return nil
			},
			removeMemberFn: func(context.Context, string, string) error {
				return nil
			},
		},
		bss: mockBSS{
			listFn: func(_ context.Context, opts bssclient.ListOptions) (*httputil.ResourceList[bsstypes.BootParam], error) {
				items := []httputil.Resource[bsstypes.BootParam]{}
				if strings.TrimSpace(opts.ComponentID) != "" {
					items = append(items, httputil.Resource[bsstypes.BootParam]{
						Kind:       "BootParam",
						APIVersion: "boot/v1",
						Metadata:   httputil.Metadata{ID: "bp-1"},
					})
				}
				return &httputil.ResourceList[bsstypes.BootParam]{Kind: "BootParamList", APIVersion: "boot/v1", Items: items}, nil
			},
			createFn: func(_ context.Context, _ bsstypes.CreateBootParamRequest) (*httputil.Resource[bsstypes.BootParam], error) {
				return &httputil.Resource[bsstypes.BootParam]{
					Kind:       "BootParam",
					APIVersion: "boot/v1",
					Metadata:   httputil.Metadata{ID: "bp-created"},
				}, nil
			},
			patchFn: func(_ context.Context, id string, _ bsstypes.PatchBootParamRequest) (*httputil.Resource[bsstypes.BootParam], error) {
				return &httputil.Resource[bsstypes.BootParam]{
					Kind:       "BootParam",
					APIVersion: "boot/v1",
					Metadata:   httputil.Metadata{ID: id},
				}, nil
			},
			deleteFn: func(context.Context, string) error {
				return nil
			},
		},
		cloudInit: mockCloud{
			listFn: func(_ context.Context, opts cloudclient.ListOptions) (*httputil.ResourceList[cloudtypes.Payload], error) {
				items := []httputil.Resource[cloudtypes.Payload]{}
				if strings.TrimSpace(opts.ComponentID) != "" {
					items = append(items, httputil.Resource[cloudtypes.Payload]{
						Kind:       "Payload",
						APIVersion: "cloud-init/v1",
						Metadata:   httputil.Metadata{ID: "pl-1"},
					})
				}
				return &httputil.ResourceList[cloudtypes.Payload]{Kind: "PayloadList", APIVersion: "cloud-init/v1", Items: items}, nil
			},
			createFn: func(_ context.Context, _ cloudtypes.CreatePayloadRequest) (*httputil.Resource[cloudtypes.Payload], error) {
				return &httputil.Resource[cloudtypes.Payload]{
					Kind:       "Payload",
					APIVersion: "cloud-init/v1",
					Metadata:   httputil.Metadata{ID: "pl-created"},
				}, nil
			},
			patchFn: func(_ context.Context, id string, _ cloudtypes.PatchPayloadRequest) (*httputil.Resource[cloudtypes.Payload], error) {
				return &httputil.Resource[cloudtypes.Payload]{
					Kind:       "Payload",
					APIVersion: "cloud-init/v1",
					Metadata:   httputil.Metadata{ID: id},
				}, nil
			},
			deleteFn: func(context.Context, string) error {
				return nil
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
			createFn: func(_ context.Context, _ powertypes.CreateTransitionRequest) (*httputil.Resource[powertypes.Transition], error) {
				return &httputil.Resource[powertypes.Transition]{
					Kind:       "Transition",
					APIVersion: "power/v1",
					Metadata:   httputil.Metadata{ID: "t-created"},
				}, nil
			},
			abortFn: func(_ context.Context, id string) (*httputil.Resource[powertypes.Transition], error) {
				return &httputil.Resource[powertypes.Transition]{
					Kind:       "Transition",
					APIVersion: "power/v1",
					Metadata:   httputil.Metadata{ID: id},
				}, nil
			},
			waitFn: func(_ context.Context, id string, _ powerclient.WaitTransitionOptions) (*httputil.Resource[powertypes.Transition], error) {
				return &httputil.Resource[powertypes.Transition]{
					Kind:       "Transition",
					APIVersion: "power/v1",
					Metadata:   httputil.Metadata{ID: id},
				}, nil
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
			createTargetFn: func(_ context.Context, _ discoverytypes.CreateTargetRequest) (*httputil.Resource[discoverytypes.Target], error) {
				return &httputil.Resource[discoverytypes.Target]{
					Kind:       "Target",
					APIVersion: "discovery/v1",
					Metadata:   httputil.Metadata{ID: "target-created"},
				}, nil
			},
			patchTargetFn: func(_ context.Context, id string, _ discoverytypes.PatchTargetRequest) (*httputil.Resource[discoverytypes.Target], error) {
				return &httputil.Resource[discoverytypes.Target]{
					Kind:       "Target",
					APIVersion: "discovery/v1",
					Metadata:   httputil.Metadata{ID: id},
				}, nil
			},
			deleteTargetFn: func(context.Context, string) error {
				return nil
			},
			startTargetScanFn: func(_ context.Context, _ string) (*httputil.Resource[discoverytypes.ScanJob], error) {
				return &httputil.Resource[discoverytypes.ScanJob]{
					Kind:       "Scan",
					APIVersion: "discovery/v1",
					Metadata:   httputil.Metadata{ID: "scan-target"},
				}, nil
			},
			startScanFn: func(_ context.Context, _ discoverytypes.StartScanRequest) (*httputil.Resource[discoverytypes.ScanJob], error) {
				return &httputil.Resource[discoverytypes.ScanJob]{
					Kind:       "Scan",
					APIVersion: "discovery/v1",
					Metadata:   httputil.Metadata{ID: "scan-trigger"},
				}, nil
			},
			cancelScanFn: func(context.Context, string) error {
				return nil
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
