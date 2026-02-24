// Package store defines persistence contracts for the power service.
package store

import "context"

// Store defines persistence methods needed by P8.4 service scaffold.
type Store interface {
	// Ping checks DB connectivity for readiness probes.
	Ping(ctx context.Context) error
}
