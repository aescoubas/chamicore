// Package store provides the PostgreSQL-backed power store.
package store

import (
	"context"
	"database/sql"

	sq "github.com/Masterminds/squirrel"
)

// PostgresStore implements Store using PostgreSQL.
type PostgresStore struct {
	db *sql.DB
	sb sq.StatementBuilderType
}

// NewPostgresStore creates a new store instance.
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{
		db: db,
		sb: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

// Ping checks database connectivity.
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
