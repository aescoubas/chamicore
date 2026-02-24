//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"git.cscs.ch/openchami/chamicore-lib/testutil"
	"git.cscs.ch/openchami/chamicore-power/internal/model"
	"git.cscs.ch/openchami/chamicore-power/internal/store"
)

func newTestStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	db := testutil.NewTestPostgres(t, "../../migrations/postgres")
	return store.NewPostgresStore(db)
}

func TestPostgresStore_ReplaceTopologyMappings_CreateUpdateDelete(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := st.ReplaceTopologyMappings(ctx, []model.BMCEndpoint{
		{BMCID: "bmc-1", Endpoint: "https://10.0.0.1", CredentialID: "cred-1", Source: "smd"},
		{BMCID: "bmc-old", Endpoint: "https://10.0.0.99", CredentialID: "cred-old", Source: "smd"},
	}, []model.NodeBMCLink{
		{NodeID: "node-1", BMCID: "bmc-1", Source: "smd"},
		{NodeID: "node-old", BMCID: "bmc-old", Source: "smd"},
	}, now)
	require.NoError(t, err)

	counts, err := st.ReplaceTopologyMappings(ctx, []model.BMCEndpoint{
		{BMCID: "bmc-1", Endpoint: "https://10.0.0.10", CredentialID: "cred-1-new", Source: "smd"},
		{BMCID: "bmc-2", Endpoint: "https://10.0.0.2", CredentialID: "cred-2", Source: "smd"},
	}, []model.NodeBMCLink{
		{NodeID: "node-1", BMCID: "bmc-1", Source: "smd"},
		{NodeID: "node-2", BMCID: "bmc-2", Source: "smd"},
	}, now.Add(time.Second))
	require.NoError(t, err)
	assert.Equal(t, 2, counts.EndpointsUpserted)
	assert.Equal(t, 2, counts.LinksUpserted)
	assert.Equal(t, 1, counts.EndpointsDeleted)
	assert.Equal(t, 1, counts.LinksDeleted)

	endpoints, err := st.ListBMCEndpoints(ctx)
	require.NoError(t, err)
	require.Len(t, endpoints, 2)
	assert.Equal(t, "bmc-1", endpoints[0].BMCID)
	assert.Equal(t, "https://10.0.0.10", endpoints[0].Endpoint)
	assert.Equal(t, "cred-1-new", endpoints[0].CredentialID)
	assert.Equal(t, "bmc-2", endpoints[1].BMCID)

	links, err := st.ListNodeBMCLinks(ctx)
	require.NoError(t, err)
	require.Len(t, links, 2)
	assert.Equal(t, "node-1", links[0].NodeID)
	assert.Equal(t, "bmc-1", links[0].BMCID)
	assert.Equal(t, "node-2", links[1].NodeID)
	assert.Equal(t, "bmc-2", links[1].BMCID)
}

func TestPostgresStore_ReplaceTopologyMappings_EmptyClearsAll(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := st.ReplaceTopologyMappings(ctx, []model.BMCEndpoint{
		{BMCID: "bmc-1", Endpoint: "https://10.0.0.1", CredentialID: "cred-1", Source: "smd"},
	}, []model.NodeBMCLink{
		{NodeID: "node-1", BMCID: "bmc-1", Source: "smd"},
	}, now)
	require.NoError(t, err)

	counts, err := st.ReplaceTopologyMappings(ctx, nil, nil, now.Add(time.Second))
	require.NoError(t, err)
	assert.Equal(t, 0, counts.EndpointsUpserted)
	assert.Equal(t, 0, counts.LinksUpserted)
	assert.Equal(t, 1, counts.EndpointsDeleted)
	assert.Equal(t, 1, counts.LinksDeleted)

	endpoints, err := st.ListBMCEndpoints(ctx)
	require.NoError(t, err)
	assert.Empty(t, endpoints)
	links, err := st.ListNodeBMCLinks(ctx)
	require.NoError(t, err)
	assert.Empty(t, links)
}

func TestPostgresStore_ResolveNodeMappings_MissingEdgeCases(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := st.ReplaceTopologyMappings(ctx, []model.BMCEndpoint{
		{BMCID: "bmc-ok", Endpoint: "https://10.0.0.1", CredentialID: "cred-ok", Source: "smd"},
		{BMCID: "bmc-no-endpoint", Endpoint: "", CredentialID: "cred-no-endpoint", Source: "smd"},
		{BMCID: "bmc-no-cred", Endpoint: "https://10.0.0.3", CredentialID: "", Source: "smd"},
	}, []model.NodeBMCLink{
		{NodeID: "node-ok", BMCID: "bmc-ok", Source: "smd"},
		{NodeID: "node-no-endpoint", BMCID: "bmc-no-endpoint", Source: "smd"},
		{NodeID: "node-no-cred", BMCID: "bmc-no-cred", Source: "smd"},
	}, now)
	require.NoError(t, err)

	resolved, missing, err := st.ResolveNodeMappings(
		ctx,
		[]string{"node-ok", "node-no-endpoint", "node-no-cred", "node-missing"},
	)
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, "node-ok", resolved[0].NodeID)
	assert.Equal(t, "bmc-ok", resolved[0].BMCID)
	assert.Equal(t, "https://10.0.0.1", resolved[0].Endpoint)
	assert.Equal(t, "cred-ok", resolved[0].CredentialID)

	require.Len(t, missing, 3)
	assert.Equal(t, "node-no-endpoint", missing[0].NodeID)
	assert.Equal(t, model.MappingErrorCodeEndpointMissing, missing[0].Code)
	assert.Contains(t, missing[0].Detail, "/power/v1/admin/mappings/sync")

	assert.Equal(t, "node-no-cred", missing[1].NodeID)
	assert.Equal(t, model.MappingErrorCodeCredentialMissing, missing[1].Code)

	assert.Equal(t, "node-missing", missing[2].NodeID)
	assert.Equal(t, model.MappingErrorCodeNotFound, missing[2].Code)
	assert.Contains(t, missing[2].Detail, "discovery is not auto-triggered")
}
