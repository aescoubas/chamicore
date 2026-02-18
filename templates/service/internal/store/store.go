// TEMPLATE: Store interface for __SERVICE_FULL__
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
//
// The Store interface defines the data access contract for the service.
// All implementations (PostgreSQL, in-memory for tests, etc.) satisfy this
// interface. Methods accept context.Context as the first parameter for
// cancellation, timeout, and trace propagation.
package store

import (
	"context"
	"errors"

	// TEMPLATE: Update this import to match your service module path.
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/model"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	// ErrNotFound is returned when the requested resource does not exist.
	ErrNotFound = errors.New("resource not found")

	// ErrConflict is returned when a write operation would violate a
	// uniqueness constraint (e.g., duplicate key).
	ErrConflict = errors.New("resource conflict")
)

// ---------------------------------------------------------------------------
// List options
// ---------------------------------------------------------------------------

// ListOptions carries pagination and optional filter parameters for list
// operations. Extend this struct with service-specific filter fields.
type ListOptions struct {
	Limit  int
	Offset int

	// TEMPLATE: Add filter fields here.
	// Example:
	// Type   string // Filter by resource type.
	// Status string // Filter by status.
}

// ---------------------------------------------------------------------------
// Store interface
// ---------------------------------------------------------------------------

// Store defines the data access methods for __RESOURCE_LOWER__ resources.
// Every method:
//   - Takes context.Context as first argument.
//   - Returns domain model types from the model package.
//   - Returns sentinel errors (ErrNotFound, ErrConflict) for expected
//     error conditions; all other errors indicate infrastructure failures.
type Store interface {
	// Ping checks database connectivity. Used by the readiness probe.
	Ping(ctx context.Context) error

	// List__RESOURCE__s retrieves a paginated slice of __RESOURCE_LOWER__ records.
	// Returns the items, the total count (for pagination metadata), and any error.
	List__RESOURCE__s(ctx context.Context, opts ListOptions) ([]model.__RESOURCE__, int, error)

	// Get__RESOURCE__ retrieves a single __RESOURCE_LOWER__ by its unique ID.
	// Returns ErrNotFound if the resource does not exist.
	Get__RESOURCE__(ctx context.Context, id string) (model.__RESOURCE__, error)

	// Create__RESOURCE__ inserts a new __RESOURCE_LOWER__ record.
	// Returns the created record (with generated ID and timestamps) or ErrConflict.
	Create__RESOURCE__(ctx context.Context, m model.__RESOURCE__) (model.__RESOURCE__, error)

	// Update__RESOURCE__ performs a full replacement of an existing __RESOURCE_LOWER__.
	// Returns the updated record or ErrNotFound.
	Update__RESOURCE__(ctx context.Context, m model.__RESOURCE__) (model.__RESOURCE__, error)

	// Delete__RESOURCE__ removes a __RESOURCE_LOWER__ by its unique ID.
	// Returns ErrNotFound if the resource does not exist.
	Delete__RESOURCE__(ctx context.Context, id string) error

	// TEMPLATE: Add additional store methods below.
	// Examples:
	// List__RESOURCE__sByParent(ctx context.Context, parentID string, opts ListOptions) ([]model.__RESOURCE__, int, error)
	// Search__RESOURCE__s(ctx context.Context, query string, opts ListOptions) ([]model.__RESOURCE__, int, error)
}
