// TEMPLATE: Integration tests for PostgresStore in __SERVICE_FULL__
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
//
// These tests use testcontainers-go to spin up a real PostgreSQL instance.
// They verify that the store methods work correctly against a real database
// with actual migrations applied.
//
// Run with: go test -tags integration -race ./internal/store/...

//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// TEMPLATE: Update these imports to match your service module path.
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/model"
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/store"

	// Shared library packages.
	"git.cscs.ch/openchami/chamicore-lib/testutil"
)

// newTestStore creates a PostgresStore backed by a testcontainers PostgreSQL
// instance with migrations applied. The container is cleaned up when the test
// finishes.
func newTestStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	// TEMPLATE: Adjust the migration path relative to this test file.
	db := testutil.NewTestPostgres(t, "../../migrations/postgres")
	return store.NewPostgresStore(db)
}

func TestPostgresStore_Ping(t *testing.T) {
	st := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := st.Ping(ctx)
	require.NoError(t, err)
}

func TestPostgresStore_CreateAndGet__RESOURCE__(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	input := model.__RESOURCE__{
		// TEMPLATE: Set the fields for your resource.
		Name:        "integration-test",
		Description: "created by integration test",
	}

	created, err := st.Create__RESOURCE__(ctx, input)
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "integration-test", created.Name)
	assert.False(t, created.CreatedAt.IsZero())

	// Get the same resource back.
	got, err := st.Get__RESOURCE__(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, created.Name, got.Name)
	assert.Equal(t, created.Description, got.Description)
}

func TestPostgresStore_Get__RESOURCE___NotFound(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	_, err := st.Get__RESOURCE__(ctx, "nonexistent-id")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestPostgresStore_List__RESOURCE__s(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Create some resources.
	for i := 0; i < 5; i++ {
		_, err := st.Create__RESOURCE__(ctx, model.__RESOURCE__{
			Name: "list-item",
		})
		require.NoError(t, err)
	}

	items, total, err := st.List__RESOURCE__s(ctx, store.ListOptions{
		Limit:  3,
		Offset: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, items, 3)
}

func TestPostgresStore_Update__RESOURCE__(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	created, err := st.Create__RESOURCE__(ctx, model.__RESOURCE__{
		Name:        "original",
		Description: "original desc",
	})
	require.NoError(t, err)

	created.Name = "updated"
	created.Description = "updated desc"

	updated, err := st.Update__RESOURCE__(ctx, created)
	require.NoError(t, err)
	assert.Equal(t, "updated", updated.Name)
	assert.Equal(t, "updated desc", updated.Description)
	assert.True(t, updated.UpdatedAt.After(created.CreatedAt) || updated.UpdatedAt.Equal(created.CreatedAt))
}

func TestPostgresStore_Update__RESOURCE___NotFound(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	_, err := st.Update__RESOURCE__(ctx, model.__RESOURCE__{
		ID:   "nonexistent",
		Name: "does not matter",
	})
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestPostgresStore_Delete__RESOURCE__(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	created, err := st.Create__RESOURCE__(ctx, model.__RESOURCE__{
		Name: "to-delete",
	})
	require.NoError(t, err)

	err = st.Delete__RESOURCE__(ctx, created.ID)
	require.NoError(t, err)

	// Verify it is gone.
	_, err = st.Get__RESOURCE__(ctx, created.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestPostgresStore_Delete__RESOURCE___NotFound(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	err := st.Delete__RESOURCE__(ctx, "nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}
