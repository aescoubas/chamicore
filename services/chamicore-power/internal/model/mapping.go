// Package model contains internal domain models for the power service.
package model

import (
	"fmt"
	"strings"
	"time"
)

const (
	// MappingErrorCodeNotFound indicates that no node->BMC link exists.
	MappingErrorCodeNotFound = "mapping_not_found"
	// MappingErrorCodeEndpointMissing indicates missing BMC Redfish endpoint.
	MappingErrorCodeEndpointMissing = "endpoint_missing"
	// MappingErrorCodeCredentialMissing indicates missing BMC credential reference.
	MappingErrorCodeCredentialMissing = "credential_missing"
)

// BMCEndpoint stores per-BMC connectivity and credential reference.
type BMCEndpoint struct {
	BMCID              string
	Endpoint           string
	CredentialID       string
	Source             string
	InsecureSkipVerify bool
	LastSyncedAt       time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// NodeBMCLink stores node -> BMC ownership resolved from SMD topology.
type NodeBMCLink struct {
	NodeID       string
	BMCID        string
	Source       string
	LastSyncedAt time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// MappingApplyCounts reports mapping reconciliation mutations.
type MappingApplyCounts struct {
	EndpointsUpserted int `json:"endpoints_upserted"`
	EndpointsDeleted  int `json:"endpoints_deleted"`
	LinksUpserted     int `json:"links_upserted"`
	LinksDeleted      int `json:"links_deleted"`
}

// NodePowerMapping is the resolved per-node power-control routing data.
type NodePowerMapping struct {
	NodeID             string `json:"node_id"`
	BMCID              string `json:"bmc_id"`
	Endpoint           string `json:"endpoint"`
	CredentialID       string `json:"credential_id"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
}

// NodeMappingError is a per-node actionable mapping failure.
type NodeMappingError struct {
	Detail string `json:"detail"`
	NodeID string `json:"node_id"`
	Code   string `json:"code"`
}

// MissingNodeMappingError returns an actionable error for absent node mapping.
func MissingNodeMappingError(nodeID string) NodeMappingError {
	node := strings.TrimSpace(nodeID)
	return NodeMappingError{
		NodeID: node,
		Code:   MappingErrorCodeNotFound,
		Detail: fmt.Sprintf(
			`node %q has no BMC mapping in local cache; run POST /power/v1/admin/mappings/sync (discovery is not auto-triggered)`,
			node,
		),
	}
}

// MissingEndpointError returns an actionable error for unresolved BMC endpoint.
func MissingEndpointError(nodeID, bmcID string) NodeMappingError {
	node := strings.TrimSpace(nodeID)
	bmc := strings.TrimSpace(bmcID)
	return NodeMappingError{
		NodeID: node,
		Code:   MappingErrorCodeEndpointMissing,
		Detail: fmt.Sprintf(
			`node %q maps to BMC %q without a Redfish endpoint; run POST /power/v1/admin/mappings/sync or fix SMD interface data`,
			node,
			bmc,
		),
	}
}

// MissingCredentialError returns an actionable error for unresolved credential reference.
func MissingCredentialError(nodeID, bmcID string) NodeMappingError {
	node := strings.TrimSpace(nodeID)
	bmc := strings.TrimSpace(bmcID)
	return NodeMappingError{
		NodeID: node,
		Code:   MappingErrorCodeCredentialMissing,
		Detail: fmt.Sprintf(
			`node %q maps to BMC %q without credential_id binding; configure a per-BMC credential then resync`,
			node,
			bmc,
		),
	}
}
