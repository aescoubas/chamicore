// TEMPLATE: PostgreSQL store implementation for __SERVICE_FULL__
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
//
// Uses Masterminds/squirrel for query building with PostgreSQL dollar-sign
// placeholders. All queries accept context.Context for cancellation and tracing.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/lib/pq"

	// TEMPLATE: Update this import to match your service module path.
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/model"
)

// pqUniqueViolation is the PostgreSQL error code for unique constraint violations.
const pqUniqueViolation = "23505"

// PostgresStore implements the Store interface using PostgreSQL.
type PostgresStore struct {
	db *sql.DB
	sb sq.StatementBuilderType
}

// NewPostgresStore creates a new PostgresStore with the given database connection.
// It configures squirrel to use PostgreSQL-style $1, $2 placeholders.
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{
		db: db,
		sb: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

// ---------------------------------------------------------------------------
// Ping
// ---------------------------------------------------------------------------

// Ping verifies that the database connection is alive.
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

// List__RESOURCE__s retrieves a paginated list of __RESOURCE_LOWER__ records.
func (s *PostgresStore) List__RESOURCE__s(ctx context.Context, opts ListOptions) ([]model.__RESOURCE__, int, error) {
	// -- Count query for total pagination metadata. ----------------------
	countQuery := s.sb.
		Select("COUNT(*)").
		From("__SCHEMA__.__RESOURCE_TABLE__")

	// TEMPLATE: Apply filters to the count query.
	// Example:
	// if opts.Type != "" {
	//     countQuery = countQuery.Where(sq.Eq{"type": opts.Type})
	// }

	countSQL, countArgs, err := countQuery.ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("building count query: %w", err)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("executing count query: %w", err)
	}

	if total == 0 {
		return []model.__RESOURCE__{}, 0, nil
	}

	// -- Data query with pagination. -------------------------------------
	dataQuery := s.sb.
		Select(
			"id",
			"name",
			"description",
			// TEMPLATE: List all columns that map to your model fields.
			"created_at",
			"updated_at",
		).
		From("__SCHEMA__.__RESOURCE_TABLE__").
		OrderBy("created_at DESC").
		Limit(uint64(opts.Limit)).
		Offset(uint64(opts.Offset))

	// TEMPLATE: Apply the same filters to the data query.
	// if opts.Type != "" {
	//     dataQuery = dataQuery.Where(sq.Eq{"type": opts.Type})
	// }

	dataSQL, dataArgs, err := dataQuery.ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("building data query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, dataSQL, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("executing data query: %w", err)
	}
	defer rows.Close()

	var items []model.__RESOURCE__
	for rows.Next() {
		var m model.__RESOURCE__
		if err := rows.Scan(
			&m.ID,
			&m.Name,
			&m.Description,
			// TEMPLATE: Scan all columns in the same order as SELECT.
			&m.CreatedAt,
			&m.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scanning row: %w", err)
		}
		items = append(items, m)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating rows: %w", err)
	}

	return items, total, nil
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

// Get__RESOURCE__ retrieves a single __RESOURCE_LOWER__ by ID.
func (s *PostgresStore) Get__RESOURCE__(ctx context.Context, id string) (model.__RESOURCE__, error) {
	query := s.sb.
		Select(
			"id",
			"name",
			"description",
			// TEMPLATE: List all columns.
			"created_at",
			"updated_at",
		).
		From("__SCHEMA__.__RESOURCE_TABLE__").
		Where(sq.Eq{"id": id})

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return model.__RESOURCE__{}, fmt.Errorf("building query: %w", err)
	}

	var m model.__RESOURCE__
	err = s.db.QueryRowContext(ctx, sqlStr, args...).Scan(
		&m.ID,
		&m.Name,
		&m.Description,
		// TEMPLATE: Scan all columns.
		&m.CreatedAt,
		&m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.__RESOURCE__{}, ErrNotFound
		}
		return model.__RESOURCE__{}, fmt.Errorf("querying __RESOURCE_LOWER__: %w", err)
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

// Create__RESOURCE__ inserts a new __RESOURCE_LOWER__ record and returns the created entity.
func (s *PostgresStore) Create__RESOURCE__(ctx context.Context, m model.__RESOURCE__) (model.__RESOURCE__, error) {
	now := time.Now().UTC()

	query := s.sb.
		Insert("__SCHEMA__.__RESOURCE_TABLE__").
		Columns(
			"id",
			"name",
			"description",
			// TEMPLATE: List all insertable columns.
			"created_at",
			"updated_at",
		).
		Values(
			m.ID,
			m.Name,
			m.Description,
			// TEMPLATE: Provide values for each column.
			now,
			now,
		).
		// TEMPLATE: If you use gen_random_uuid() in the DB, use this:
		// Suffix("RETURNING id, created_at, updated_at")
		Suffix("RETURNING id, created_at, updated_at")

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return model.__RESOURCE__{}, fmt.Errorf("building insert query: %w", err)
	}

	err = s.db.QueryRowContext(ctx, sqlStr, args...).Scan(
		&m.ID,
		&m.CreatedAt,
		&m.UpdatedAt,
	)
	if err != nil {
		if isPQUniqueViolation(err) {
			return model.__RESOURCE__{}, ErrConflict
		}
		return model.__RESOURCE__{}, fmt.Errorf("inserting __RESOURCE_LOWER__: %w", err)
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

// Update__RESOURCE__ performs a full replacement update of a __RESOURCE_LOWER__ record.
func (s *PostgresStore) Update__RESOURCE__(ctx context.Context, m model.__RESOURCE__) (model.__RESOURCE__, error) {
	now := time.Now().UTC()

	query := s.sb.
		Update("__SCHEMA__.__RESOURCE_TABLE__").
		Set("name", m.Name).
		Set("description", m.Description).
		// TEMPLATE: Set all updatable columns.
		Set("updated_at", now).
		Where(sq.Eq{"id": m.ID}).
		Suffix("RETURNING updated_at")

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return model.__RESOURCE__{}, fmt.Errorf("building update query: %w", err)
	}

	err = s.db.QueryRowContext(ctx, sqlStr, args...).Scan(&m.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.__RESOURCE__{}, ErrNotFound
		}
		return model.__RESOURCE__{}, fmt.Errorf("updating __RESOURCE_LOWER__: %w", err)
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

// Delete__RESOURCE__ removes a __RESOURCE_LOWER__ record by ID.
func (s *PostgresStore) Delete__RESOURCE__(ctx context.Context, id string) error {
	query := s.sb.
		Delete("__SCHEMA__.__RESOURCE_TABLE__").
		Where(sq.Eq{"id": id})

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return fmt.Errorf("building delete query: %w", err)
	}

	result, err := s.db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return fmt.Errorf("deleting __RESOURCE_LOWER__: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// isPQUniqueViolation checks whether the error is a PostgreSQL unique
// constraint violation (error code 23505).
func isPQUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == pqUniqueViolation
	}
	return false
}
