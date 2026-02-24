// Package store defines persistence contracts for the power service.
package store

import (
	"context"
	"errors"
	"time"

	"git.cscs.ch/openchami/chamicore-power/internal/model"
)

var (
	// ErrNotFound indicates the requested resource does not exist.
	ErrNotFound = errors.New("not found")
)

// Store defines persistence methods needed by P8.4 service scaffold.
type Store interface {
	// Ping checks DB connectivity for readiness probes.
	Ping(ctx context.Context) error
	// ReplaceTopologyMappings reconciles local mapping cache to desired SMD-derived state.
	ReplaceTopologyMappings(ctx context.Context, endpoints []model.BMCEndpoint, links []model.NodeBMCLink, syncedAt time.Time) (model.MappingApplyCounts, error)
	// ResolveNodeMappings resolves per-node BMC/credential routing info and reports per-node actionable missing errors.
	ResolveNodeMappings(ctx context.Context, nodeIDs []string) ([]model.NodePowerMapping, []model.NodeMappingError, error)
	// ListBMCEndpoints returns all cached BMC endpoint rows.
	ListBMCEndpoints(ctx context.Context) ([]model.BMCEndpoint, error)
	// ListNodeBMCLinks returns all cached node->BMC link rows.
	ListNodeBMCLinks(ctx context.Context) ([]model.NodeBMCLink, error)
}
