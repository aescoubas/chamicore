// Package api embeds the OpenAPI specification for the power service.
package api

import _ "embed"

// OpenAPISpec contains the raw OpenAPI 3.0 YAML specification.
//
//go:embed openapi.yaml
var OpenAPISpec []byte
