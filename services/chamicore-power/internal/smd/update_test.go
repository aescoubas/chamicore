package smd

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
	smdtypes "git.cscs.ch/openchami/chamicore-smd/pkg/types"
)

type mockComponentClient struct {
	patchComponentFn func(ctx context.Context, id string, req smdtypes.PatchComponentRequest) (*httputil.Resource[smdtypes.Component], error)
}

func (m *mockComponentClient) PatchComponent(
	ctx context.Context,
	id string,
	req smdtypes.PatchComponentRequest,
) (*httputil.Resource[smdtypes.Component], error) {
	return m.patchComponentFn(ctx, id, req)
}

func TestUpdater_UpdateNodePowerState_OnToReady(t *testing.T) {
	client := &mockComponentClient{
		patchComponentFn: func(ctx context.Context, id string, req smdtypes.PatchComponentRequest) (*httputil.Resource[smdtypes.Component], error) {
			require.Equal(t, "node-1", id)
			require.NotNil(t, req.State)
			assert.Equal(t, "Ready", *req.State)
			return &httputil.Resource[smdtypes.Component]{}, nil
		},
	}

	updater := NewUpdater(client)
	err := updater.UpdateNodePowerState(context.Background(), " node-1 ", " On ")
	require.NoError(t, err)
}

func TestUpdater_UpdateNodePowerState_OffToOff(t *testing.T) {
	client := &mockComponentClient{
		patchComponentFn: func(ctx context.Context, id string, req smdtypes.PatchComponentRequest) (*httputil.Resource[smdtypes.Component], error) {
			require.Equal(t, "node-2", id)
			require.NotNil(t, req.State)
			assert.Equal(t, "Off", *req.State)
			return &httputil.Resource[smdtypes.Component]{}, nil
		},
	}

	updater := NewUpdater(client)
	err := updater.UpdateNodePowerState(context.Background(), "node-2", "off")
	require.NoError(t, err)
}

func TestUpdater_UpdateNodePowerState_UnsupportedState(t *testing.T) {
	updater := NewUpdater(&mockComponentClient{
		patchComponentFn: func(ctx context.Context, id string, req smdtypes.PatchComponentRequest) (*httputil.Resource[smdtypes.Component], error) {
			t.Fatal("PatchComponent must not be called for unsupported state")
			return nil, nil
		},
	})

	err := updater.UpdateNodePowerState(context.Background(), "node-1", "PoweringOn")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedPowerState)
}

func TestUpdater_UpdateNodePowerState_PatchFailure(t *testing.T) {
	updater := NewUpdater(&mockComponentClient{
		patchComponentFn: func(ctx context.Context, id string, req smdtypes.PatchComponentRequest) (*httputil.Resource[smdtypes.Component], error) {
			return nil, errors.New("smd unavailable")
		},
	})

	err := updater.UpdateNodePowerState(context.Background(), "node-1", "On")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "patching SMD component")
}

func TestUpdater_UpdateNodePowerState_NoClient(t *testing.T) {
	updater := NewUpdater(nil)
	err := updater.UpdateNodePowerState(context.Background(), "node-1", "On")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}
