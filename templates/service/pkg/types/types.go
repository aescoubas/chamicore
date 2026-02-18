// TEMPLATE: Public API types for __SERVICE_FULL__
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
//
// These types define the wire format of the __SERVICE__ API. They are imported
// by the client SDK, CLI, other services, and integration tests. They must
// remain backward-compatible across minor versions.
//
// Conventions:
//   - Resource envelope: every response wraps the payload in a standard
//     envelope with kind, apiVersion, metadata, and spec fields.
//   - Request types use value fields for required data and pointer fields
//     for optional/patchable data.
//   - JSON tags use camelCase to match the OpenAPI schema.
//   - Validation is NOT performed in this package; handlers validate inputs.
package types

import "time"

// ===========================================================================
// Resource Envelope — standard wrapper for all API responses.
// ===========================================================================

// Resource is the standard envelope for a single API resource.
// The type parameter T is the spec type (e.g., __RESOURCE__).
type Resource[T any] struct {
	// Kind identifies the resource type (e.g., "__RESOURCE__").
	Kind string `json:"kind"`

	// APIVersion identifies the API version (e.g., "__API_VERSION__").
	APIVersion string `json:"apiVersion"`

	// Metadata contains resource identity and audit fields.
	Metadata ResourceMetadata `json:"metadata"`

	// Spec contains the resource-specific payload.
	Spec T `json:"spec"`
}

// ResourceMetadata carries identity and audit fields common to all resources.
type ResourceMetadata struct {
	// ID is the unique resource identifier (UUID).
	ID string `json:"id"`

	// CreatedAt is the timestamp when the resource was created.
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the timestamp of the last modification.
	UpdatedAt time.Time `json:"updatedAt"`
}

// ResourceList is the standard envelope for a collection of API resources.
type ResourceList[T any] struct {
	// Kind identifies the list type (e.g., "__RESOURCE__List").
	Kind string `json:"kind"`

	// APIVersion identifies the API version.
	APIVersion string `json:"apiVersion"`

	// Metadata contains pagination information.
	Metadata ListMetadata `json:"metadata"`

	// Items is the slice of resources in this page.
	Items []T `json:"items"`
}

// ListMetadata carries pagination information for list responses.
type ListMetadata struct {
	// TotalCount is the total number of matching resources (across all pages).
	TotalCount int `json:"totalCount"`

	// Limit is the maximum number of items in this page.
	Limit int `json:"limit"`

	// Offset is the number of items skipped before this page.
	Offset int `json:"offset"`
}

// ===========================================================================
// __RESOURCE__ — the primary resource type.
// ===========================================================================

// __RESOURCE__ is the public representation of a __RESOURCE_LOWER__ resource.
// This appears in the "spec" field of the resource envelope.
//
// TEMPLATE: Add, rename, or remove fields to match your API design.
type __RESOURCE__ struct {
	// Name is a human-readable label.
	Name string `json:"name"`

	// Description is an optional longer description.
	Description string `json:"description,omitempty"`

	// TEMPLATE: Add resource-specific fields here. Examples:
	//
	// Type is the classification (e.g., "Node", "Switch").
	// Type string `json:"type"`
	//
	// Status is the operational state.
	// Status string `json:"status,omitempty"`
	//
	// Tags is a set of key-value labels.
	// Tags map[string]string `json:"tags,omitempty"`
}

// ===========================================================================
// Request types — used for Create, Update, and Patch operations.
// ===========================================================================

// Create__RESOURCE__Request is the request body for creating a new __RESOURCE_LOWER__.
// All required fields use value types; optional fields use pointers.
//
// TEMPLATE: Adjust fields to match your creation payload.
type Create__RESOURCE__Request struct {
	// Name is required.
	Name string `json:"name"`

	// Description is optional.
	Description string `json:"description,omitempty"`

	// TEMPLATE: Add creation-specific fields.
	// Type string `json:"type"`
}

// Update__RESOURCE__Request is the request body for a full replacement (PUT).
// All fields are required because PUT replaces the entire resource.
//
// TEMPLATE: Adjust fields to match your update payload.
type Update__RESOURCE__Request struct {
	// Name is required.
	Name string `json:"name"`

	// Description is required (send empty string to clear).
	Description string `json:"description"`

	// TEMPLATE: Add all updatable fields.
}

// Patch__RESOURCE__Request is the request body for a partial update (PATCH).
// All fields are pointers: nil means "do not change", non-nil means "set to
// this value" (including zero values like empty string).
//
// TEMPLATE: Adjust fields to match your patchable fields.
type Patch__RESOURCE__Request struct {
	// Name, if non-nil, replaces the current name.
	Name *string `json:"name,omitempty"`

	// Description, if non-nil, replaces the current description.
	Description *string `json:"description,omitempty"`

	// TEMPLATE: Add all patchable fields as pointers.
	// Type *string `json:"type,omitempty"`
}

// ===========================================================================
// RFC 9457 Problem Details — standard error response type.
// ===========================================================================

// ProblemDetail represents an RFC 9457 Problem Details response.
// This type is defined here for client-side error parsing. The server
// constructs these via chamicore-lib helpers (chamihttp.RespondProblem).
type ProblemDetail struct {
	// Type is a URI reference identifying the problem type.
	// Default: "about:blank"
	Type string `json:"type"`

	// Title is a short, human-readable summary.
	Title string `json:"title"`

	// Status is the HTTP status code.
	Status int `json:"status"`

	// Detail is a human-readable explanation specific to this occurrence.
	Detail string `json:"detail,omitempty"`

	// Instance is a URI reference identifying the specific occurrence.
	Instance string `json:"instance,omitempty"`

	// Errors is an optional list of field-level validation errors.
	Errors []ValidationError `json:"errors,omitempty"`
}

// ValidationError represents a single field-level validation failure.
type ValidationError struct {
	// Field is the JSON field path (e.g., "name", "spec.type").
	Field string `json:"field"`

	// Message describes what is wrong with the field value.
	Message string `json:"message"`
}
