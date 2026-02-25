package api

import _ "embed"

// ToolsContract contains the MCP V1 tool contract.
//
//go:embed tools.yaml
var ToolsContract []byte
