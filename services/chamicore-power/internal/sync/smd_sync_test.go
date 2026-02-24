package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-power/internal/model"
	"git.cscs.ch/openchami/chamicore-power/internal/store"
	smdclient "git.cscs.ch/openchami/chamicore-smd/pkg/client"
	smdtypes "git.cscs.ch/openchami/chamicore-smd/pkg/types"
)

type mockSMDClient struct {
	listComponentsFn         func(ctx context.Context, opts smdclient.ComponentListOptions) (*httputil.ResourceList[smdtypes.Component], error)
	listEthernetInterfacesFn func(ctx context.Context, opts smdclient.InterfaceListOptions) (*httputil.ResourceList[smdtypes.EthernetInterface], error)
}

func (m *mockSMDClient) ListComponents(ctx context.Context, opts smdclient.ComponentListOptions) (*httputil.ResourceList[smdtypes.Component], error) {
	return m.listComponentsFn(ctx, opts)
}

func (m *mockSMDClient) ListEthernetInterfaces(ctx context.Context, opts smdclient.InterfaceListOptions) (*httputil.ResourceList[smdtypes.EthernetInterface], error) {
	return m.listEthernetInterfacesFn(ctx, opts)
}

type memoryStore struct {
	mu        sync.Mutex
	endpoints map[string]model.BMCEndpoint
	links     map[string]model.NodeBMCLink
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		endpoints: make(map[string]model.BMCEndpoint),
		links:     make(map[string]model.NodeBMCLink),
	}
}

func (m *memoryStore) Ping(ctx context.Context) error {
	return nil
}

func (m *memoryStore) ReplaceTopologyMappings(
	ctx context.Context,
	endpoints []model.BMCEndpoint,
	links []model.NodeBMCLink,
	syncedAt time.Time,
) (model.MappingApplyCounts, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	newEndpoints := make(map[string]model.BMCEndpoint, len(endpoints))
	for _, endpoint := range endpoints {
		copyEndpoint := endpoint
		copyEndpoint.LastSyncedAt = syncedAt
		newEndpoints[endpoint.BMCID] = copyEndpoint
	}

	newLinks := make(map[string]model.NodeBMCLink, len(links))
	for _, link := range links {
		copyLink := link
		copyLink.LastSyncedAt = syncedAt
		newLinks[link.NodeID] = copyLink
	}

	var counts model.MappingApplyCounts
	counts.EndpointsUpserted = len(newEndpoints)
	counts.LinksUpserted = len(newLinks)

	for bmcID := range m.endpoints {
		if _, ok := newEndpoints[bmcID]; !ok {
			counts.EndpointsDeleted++
		}
	}
	for nodeID := range m.links {
		if _, ok := newLinks[nodeID]; !ok {
			counts.LinksDeleted++
		}
	}

	m.endpoints = newEndpoints
	m.links = newLinks
	return counts, nil
}

func (m *memoryStore) ResolveNodeMappings(
	ctx context.Context,
	nodeIDs []string,
) ([]model.NodePowerMapping, []model.NodeMappingError, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	resolved := make([]model.NodePowerMapping, 0, len(nodeIDs))
	missing := make([]model.NodeMappingError, 0)

	for _, nodeID := range nodeIDs {
		link, ok := m.links[nodeID]
		if !ok {
			missing = append(missing, model.MissingNodeMappingError(nodeID))
			continue
		}

		endpoint, ok := m.endpoints[link.BMCID]
		if !ok || endpoint.Endpoint == "" {
			missing = append(missing, model.MissingEndpointError(nodeID, link.BMCID))
			continue
		}
		if endpoint.CredentialID == "" {
			missing = append(missing, model.MissingCredentialError(nodeID, link.BMCID))
			continue
		}

		resolved = append(resolved, model.NodePowerMapping{
			NodeID:             nodeID,
			BMCID:              link.BMCID,
			Endpoint:           endpoint.Endpoint,
			CredentialID:       endpoint.CredentialID,
			InsecureSkipVerify: endpoint.InsecureSkipVerify,
		})
	}

	return resolved, missing, nil
}

func (m *memoryStore) ListBMCEndpoints(ctx context.Context) ([]model.BMCEndpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	keys := make([]string, 0, len(m.endpoints))
	for bmcID := range m.endpoints {
		keys = append(keys, bmcID)
	}
	sort.Strings(keys)

	items := make([]model.BMCEndpoint, 0, len(keys))
	for _, bmcID := range keys {
		items = append(items, m.endpoints[bmcID])
	}
	return items, nil
}

func (m *memoryStore) ListNodeBMCLinks(ctx context.Context) ([]model.NodeBMCLink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	keys := make([]string, 0, len(m.links))
	for nodeID := range m.links {
		keys = append(keys, nodeID)
	}
	sort.Strings(keys)

	items := make([]model.NodeBMCLink, 0, len(keys))
	for _, nodeID := range keys {
		items = append(items, m.links[nodeID])
	}
	return items, nil
}

func TestSyncOnce_ReconcileCreateUpdateDeleteAndMissing(t *testing.T) {
	st := newMemoryStore()
	st.endpoints["bmc-1"] = model.BMCEndpoint{
		BMCID:        "bmc-1",
		Endpoint:     "https://old-bmc-1.example",
		CredentialID: "cred-existing",
	}
	st.endpoints["bmc-old"] = model.BMCEndpoint{
		BMCID:        "bmc-old",
		Endpoint:     "https://old-bmc.example",
		CredentialID: "cred-old",
	}
	st.links["node-stale"] = model.NodeBMCLink{
		NodeID: "node-stale",
		BMCID:  "bmc-old",
	}

	smd := &mockSMDClient{
		listComponentsFn: func(ctx context.Context, opts smdclient.ComponentListOptions) (*httputil.ResourceList[smdtypes.Component], error) {
			parentBMC1 := "bmc-1"
			parentBMC2 := "bmc-2"
			return &httputil.ResourceList[smdtypes.Component]{
				Kind:       "ComponentList",
				APIVersion: "hsm/v2",
				Items: []httputil.Resource[smdtypes.Component]{
					{Spec: smdtypes.Component{ID: "bmc-1", Type: "BMC"}},
					{Spec: smdtypes.Component{ID: "bmc-2", Type: "BMC"}},
					{Spec: smdtypes.Component{ID: "node-1", Type: "Node", ParentID: &parentBMC1}},
					{Spec: smdtypes.Component{ID: "node-2", Type: "Node", ParentID: &parentBMC2}},
				},
			}, nil
		},
		listEthernetInterfacesFn: func(ctx context.Context, opts smdclient.InterfaceListOptions) (*httputil.ResourceList[smdtypes.EthernetInterface], error) {
			return &httputil.ResourceList[smdtypes.EthernetInterface]{
				Kind:       "EthernetInterfaceList",
				APIVersion: "hsm/v2",
				Items: []httputil.Resource[smdtypes.EthernetInterface]{
					{Spec: smdtypes.EthernetInterface{
						ComponentID: "bmc-1",
						IPAddrs:     json.RawMessage(`["10.1.0.10"]`),
					}},
					{Spec: smdtypes.EthernetInterface{
						ComponentID: "bmc-2",
						IPAddrs:     json.RawMessage(`[]`),
					}},
				},
			}, nil
		},
	}

	s := New(st, smd, Config{
		Interval:            time.Second,
		SyncOnStartup:       true,
		DefaultCredentialID: "cred-default",
	}, zerolog.Nop())

	require.NoError(t, s.SyncOnce(context.Background()))

	status := s.Status()
	assert.True(t, status.Ready)
	assert.Equal(t, 2, status.LastCounts.EndpointsUpserted)
	assert.Equal(t, 2, status.LastCounts.LinksUpserted)
	assert.Equal(t, 1, status.LastCounts.EndpointsDeleted)
	assert.Equal(t, 1, status.LastCounts.LinksDeleted)

	endpoints, err := st.ListBMCEndpoints(context.Background())
	require.NoError(t, err)
	require.Len(t, endpoints, 2)

	assert.Equal(t, "https://10.1.0.10", endpoints[0].Endpoint)
	assert.Equal(t, "cred-existing", endpoints[0].CredentialID)
	assert.Equal(t, "bmc-1", endpoints[0].BMCID)

	assert.Equal(t, "bmc-2", endpoints[1].BMCID)
	assert.Equal(t, "", endpoints[1].Endpoint)
	assert.Equal(t, "cred-default", endpoints[1].CredentialID)

	resolved, missing, err := st.ResolveNodeMappings(context.Background(), []string{"node-1", "node-2", "node-missing"})
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, "node-1", resolved[0].NodeID)
	assert.Equal(t, "bmc-1", resolved[0].BMCID)

	require.Len(t, missing, 2)
	assert.Equal(t, model.MappingErrorCodeEndpointMissing, missing[0].Code)
	assert.Equal(t, "node-2", missing[0].NodeID)
	assert.Equal(t, model.MappingErrorCodeNotFound, missing[1].Code)
	assert.Equal(t, "node-missing", missing[1].NodeID)
}

func TestSyncOnce_NotModified(t *testing.T) {
	st := newMemoryStore()
	calls := 0
	seenIfNoneMatch := ""

	componentList := &httputil.ResourceList[smdtypes.Component]{
		Kind:       "ComponentList",
		APIVersion: "hsm/v2",
		Items: []httputil.Resource[smdtypes.Component]{
			{Spec: smdtypes.Component{ID: "bmc-1", Type: "BMC"}},
		},
	}

	smd := &mockSMDClient{
		listComponentsFn: func(ctx context.Context, opts smdclient.ComponentListOptions) (*httputil.ResourceList[smdtypes.Component], error) {
			calls++
			seenIfNoneMatch = opts.IfNoneMatch
			if calls == 1 {
				return componentList, nil
			}
			return nil, nil
		},
		listEthernetInterfacesFn: func(ctx context.Context, opts smdclient.InterfaceListOptions) (*httputil.ResourceList[smdtypes.EthernetInterface], error) {
			return &httputil.ResourceList[smdtypes.EthernetInterface]{
				Kind:       "EthernetInterfaceList",
				APIVersion: "hsm/v2",
				Items: []httputil.Resource[smdtypes.EthernetInterface]{
					{Spec: smdtypes.EthernetInterface{
						ComponentID: "bmc-1",
						IPAddrs:     json.RawMessage(`["10.2.0.1"]`),
					}},
				},
			}, nil
		},
	}

	s := New(st, smd, Config{Interval: time.Second}, zerolog.Nop())
	require.NoError(t, s.SyncOnce(context.Background()))
	first := s.Status()
	require.NotEmpty(t, first.LastComponentETag)

	require.NoError(t, s.SyncOnce(context.Background()))
	second := s.Status()
	assert.True(t, second.NotModified)
	assert.Equal(t, int64(2), second.SuccessfulRuns)
	assert.Equal(t, first.LastComponentETag, seenIfNoneMatch)
}

func TestSyncOnce_InterfaceChangeForcesComponentRefresh(t *testing.T) {
	st := newMemoryStore()
	var conditionalCalls int
	var refreshCalls int
	firstCall := true
	interfaceAddress := "10.9.0.1"

	smd := &mockSMDClient{
		listComponentsFn: func(ctx context.Context, opts smdclient.ComponentListOptions) (*httputil.ResourceList[smdtypes.Component], error) {
			if opts.IfNoneMatch == "" {
				refreshCalls++
			} else {
				conditionalCalls++
			}

			parent := "bmc-1"
			list := &httputil.ResourceList[smdtypes.Component]{
				Kind:       "ComponentList",
				APIVersion: "hsm/v2",
				Items: []httputil.Resource[smdtypes.Component]{
					{Spec: smdtypes.Component{ID: "bmc-1", Type: "BMC"}},
					{Spec: smdtypes.Component{ID: "node-1", Type: "Node", ParentID: &parent}},
				},
			}
			if firstCall {
				firstCall = false
				return list, nil
			}
			if opts.IfNoneMatch != "" {
				return nil, nil
			}
			return list, nil
		},
		listEthernetInterfacesFn: func(ctx context.Context, opts smdclient.InterfaceListOptions) (*httputil.ResourceList[smdtypes.EthernetInterface], error) {
			return &httputil.ResourceList[smdtypes.EthernetInterface]{
				Kind:       "EthernetInterfaceList",
				APIVersion: "hsm/v2",
				Items: []httputil.Resource[smdtypes.EthernetInterface]{
					{Spec: smdtypes.EthernetInterface{
						ComponentID: "bmc-1",
						IPAddrs:     json.RawMessage(`["` + interfaceAddress + `"]`),
					}},
				},
			}, nil
		},
	}

	s := New(st, smd, Config{Interval: time.Second}, zerolog.Nop())
	require.NoError(t, s.SyncOnce(context.Background()))

	interfaceAddress = "10.9.0.2"
	require.NoError(t, s.SyncOnce(context.Background()))

	endpoints, err := st.ListBMCEndpoints(context.Background())
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	assert.Equal(t, "https://10.9.0.2", endpoints[0].Endpoint)
	assert.GreaterOrEqual(t, refreshCalls, 2)
}

func TestSyncOnce_ComponentFetchError(t *testing.T) {
	st := newMemoryStore()
	smd := &mockSMDClient{
		listComponentsFn: func(ctx context.Context, opts smdclient.ComponentListOptions) (*httputil.ResourceList[smdtypes.Component], error) {
			return nil, errors.New("boom")
		},
		listEthernetInterfacesFn: func(ctx context.Context, opts smdclient.InterfaceListOptions) (*httputil.ResourceList[smdtypes.EthernetInterface], error) {
			return nil, nil
		},
	}

	s := New(st, smd, Config{Interval: time.Second}, zerolog.Nop())
	err := s.SyncOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing components from SMD")

	status := s.Status()
	assert.False(t, status.Ready)
	assert.Equal(t, int64(1), status.FailedRuns)
	assert.NotEmpty(t, status.LastError)
}

func TestTrigger_ContextTimeoutWhenLoopNotRunning(t *testing.T) {
	s := New(newMemoryStore(), &mockSMDClient{}, Config{}, zerolog.Nop())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := s.Trigger(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRun_TriggerSync(t *testing.T) {
	st := newMemoryStore()
	smd := &mockSMDClient{
		listComponentsFn: func(ctx context.Context, opts smdclient.ComponentListOptions) (*httputil.ResourceList[smdtypes.Component], error) {
			parent := "bmc-1"
			return &httputil.ResourceList[smdtypes.Component]{
				Kind:       "ComponentList",
				APIVersion: "hsm/v2",
				Items: []httputil.Resource[smdtypes.Component]{
					{Spec: smdtypes.Component{ID: "bmc-1", Type: "BMC"}},
					{Spec: smdtypes.Component{ID: "node-1", Type: "Node", ParentID: &parent}},
				},
			}, nil
		},
		listEthernetInterfacesFn: func(ctx context.Context, opts smdclient.InterfaceListOptions) (*httputil.ResourceList[smdtypes.EthernetInterface], error) {
			return &httputil.ResourceList[smdtypes.EthernetInterface]{
				Kind:       "EthernetInterfaceList",
				APIVersion: "hsm/v2",
				Items: []httputil.Resource[smdtypes.EthernetInterface]{
					{Spec: smdtypes.EthernetInterface{
						ComponentID: "bmc-1",
						IPAddrs:     json.RawMessage(`["10.3.0.1"]`),
					}},
				},
			}, nil
		},
	}

	s := New(st, smd, Config{Interval: time.Hour, SyncOnStartup: false}, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	triggerCtx, triggerCancel := context.WithTimeout(context.Background(), time.Second)
	defer triggerCancel()
	require.NoError(t, s.Trigger(triggerCtx))

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sync loop did not stop after context cancellation")
	}
}

func TestBuildDesiredMappings_PreservesExistingCredentialAndNormalizesEndpoint(t *testing.T) {
	parent := "bmc-9"
	components := &httputil.ResourceList[smdtypes.Component]{
		Kind:       "ComponentList",
		APIVersion: "hsm/v2",
		Items: []httputil.Resource[smdtypes.Component]{
			{Spec: smdtypes.Component{ID: "bmc-9", Type: "BMC"}},
			{Spec: smdtypes.Component{ID: "node-9", Type: "Node", ParentID: &parent}},
		},
	}
	interfaces := &httputil.ResourceList[smdtypes.EthernetInterface]{
		Kind:       "EthernetInterfaceList",
		APIVersion: "hsm/v2",
		Items: []httputil.Resource[smdtypes.EthernetInterface]{
			{
				Spec: smdtypes.EthernetInterface{
					ComponentID: "bmc-9",
					IPAddrs:     json.RawMessage(`["https://bmc-9.example:9443/redfish/v1"]`),
				},
			},
		},
	}

	endpoints, links := buildDesiredMappings(
		components,
		interfaces,
		[]model.BMCEndpoint{{
			BMCID:        "bmc-9",
			CredentialID: "cred-kept",
		}},
		"cred-default",
	)

	require.Len(t, endpoints, 1)
	assert.Equal(t, "https://bmc-9.example:9443", endpoints[0].Endpoint)
	assert.Equal(t, "cred-kept", endpoints[0].CredentialID)
	require.Len(t, links, 1)
	assert.Equal(t, "node-9", links[0].NodeID)
	assert.Equal(t, "bmc-9", links[0].BMCID)
}

var _ store.Store = (*memoryStore)(nil)
