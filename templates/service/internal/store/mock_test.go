// TEMPLATE: Mock store for handler tests in __SERVICE_FULL__
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
//
// This mock uses the hand-written function-field pattern required by AGENTS.md.
// Each Store method delegates to a corresponding Fn field. Unused Fn fields
// left nil will panic if called — this is intentional, as it means the test
// hit an unexpected code path.
package store

import (
	"context"

	// TEMPLATE: Update this import to match your service module path.
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/model"
)

// MockStore implements the Store interface for testing. Set only the Fn fields
// needed for the test case being exercised.
type MockStore struct {
	PingFn            func(ctx context.Context) error
	List__RESOURCE__sFn  func(ctx context.Context, opts ListOptions) ([]model.__RESOURCE__, int, error)
	Get__RESOURCE__Fn    func(ctx context.Context, id string) (model.__RESOURCE__, error)
	Create__RESOURCE__Fn func(ctx context.Context, m model.__RESOURCE__) (model.__RESOURCE__, error)
	Update__RESOURCE__Fn func(ctx context.Context, m model.__RESOURCE__) (model.__RESOURCE__, error)
	Delete__RESOURCE__Fn func(ctx context.Context, id string) error
}

func (m *MockStore) Ping(ctx context.Context) error {
	return m.PingFn(ctx)
}

func (m *MockStore) List__RESOURCE__s(ctx context.Context, opts ListOptions) ([]model.__RESOURCE__, int, error) {
	return m.List__RESOURCE__sFn(ctx, opts)
}

func (m *MockStore) Get__RESOURCE__(ctx context.Context, id string) (model.__RESOURCE__, error) {
	return m.Get__RESOURCE__Fn(ctx, id)
}

func (m *MockStore) Create__RESOURCE__(ctx context.Context, model model.__RESOURCE__) (model.__RESOURCE__, error) {
	return m.Create__RESOURCE__Fn(ctx, model)
}

func (m *MockStore) Update__RESOURCE__(ctx context.Context, model model.__RESOURCE__) (model.__RESOURCE__, error) {
	return m.Update__RESOURCE__Fn(ctx, model)
}

func (m *MockStore) Delete__RESOURCE__(ctx context.Context, id string) error {
	return m.Delete__RESOURCE__Fn(ctx, id)
}
