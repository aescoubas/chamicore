// TEMPLATE: Public API types for __SERVICE_FULL__
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
//
// These types define the wire format of the __SERVICE__ API. They are imported
// by the client SDK, CLI, other services, and integration tests. They must
// remain backward-compatible across minor versions.
//
// Conventions:
//   - Resource envelope types (Resource, ResourceList, Metadata, ListMetadata)
//     come from chamicore-lib/httputil — do NOT redefine them here.
//   - Request types use value fields for required data and pointer fields
//     for optional/patchable data.
//   - JSON tags use camelCase to match the OpenAPI schema.
//   - Validation is NOT performed in this package; handlers validate inputs.
package types

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
