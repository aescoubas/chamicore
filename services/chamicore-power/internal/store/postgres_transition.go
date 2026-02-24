// Package store provides transition persistence for the power service.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"

	"git.cscs.ch/openchami/chamicore-power/internal/engine"
)

const (
	defaultTransitionPageLimit = 100
	maxTransitionPageLimit     = 1000
)

// CreateTransition persists a transition row and all per-node task rows.
func (s *PostgresStore) CreateTransition(
	ctx context.Context,
	transition engine.Transition,
	tasks []engine.Task,
) (engine.Transition, []engine.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return engine.Transition{}, nil, fmt.Errorf("starting transition transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	createdTransition, err := s.insertTransitionTx(ctx, tx, transition)
	if err != nil {
		return engine.Transition{}, nil, err
	}

	createdTasks := make([]engine.Task, 0, len(tasks))
	for _, task := range tasks {
		task.TransitionID = createdTransition.ID
		createdTask, insertErr := s.insertTransitionTaskTx(ctx, tx, task)
		if insertErr != nil {
			return engine.Transition{}, nil, insertErr
		}
		createdTasks = append(createdTasks, createdTask)
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return engine.Transition{}, nil, fmt.Errorf("committing transition transaction: %w", commitErr)
	}

	return createdTransition, createdTasks, nil
}

// UpdateTransition updates mutable transition fields by ID.
func (s *PostgresStore) UpdateTransition(ctx context.Context, transition engine.Transition) (engine.Transition, error) {
	id := strings.TrimSpace(transition.ID)
	if id == "" {
		return engine.Transition{}, fmt.Errorf("transition id is required")
	}

	transition.ID = id
	if transition.UpdatedAt.IsZero() {
		transition.UpdatedAt = time.Now().UTC()
	}

	query := s.sb.
		Update("power.transitions").
		Set("request_id", strings.TrimSpace(transition.RequestID)).
		Set("operation", strings.TrimSpace(transition.Operation)).
		Set("state", strings.TrimSpace(transition.State)).
		Set("requested_by", strings.TrimSpace(transition.RequestedBy)).
		Set("dry_run", transition.DryRun).
		Set("target_count", transition.TargetCount).
		Set("success_count", transition.SuccessCount).
		Set("failure_count", transition.FailureCount).
		Set("queued_at", transition.QueuedAt.UTC()).
		Set("started_at", optionalTimeValue(transition.StartedAt)).
		Set("completed_at", optionalTimeValue(transition.CompletedAt)).
		Set("updated_at", transition.UpdatedAt.UTC()).
		Where(sq.Eq{"id": id})

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return engine.Transition{}, fmt.Errorf("building transition update query: %w", err)
	}

	res, err := s.db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return engine.Transition{}, fmt.Errorf("updating transition %q: %w", id, err)
	}

	affected, err := rowsAffectedAsInt(res, "transition update")
	if err != nil {
		return engine.Transition{}, err
	}
	if affected == 0 {
		return engine.Transition{}, ErrNotFound
	}

	updated, err := s.GetTransition(ctx, id)
	if err != nil {
		return engine.Transition{}, err
	}
	return updated, nil
}

// UpdateTransitionTask updates mutable task fields by ID.
func (s *PostgresStore) UpdateTransitionTask(ctx context.Context, task engine.Task) (engine.Task, error) {
	id := strings.TrimSpace(task.ID)
	if id == "" {
		return engine.Task{}, fmt.Errorf("task id is required")
	}

	task.ID = id
	task.TransitionID = strings.TrimSpace(task.TransitionID)
	task.NodeID = strings.TrimSpace(task.NodeID)
	task.BMCID = strings.TrimSpace(task.BMCID)
	task.BMCEndpoint = strings.TrimSpace(task.BMCEndpoint)
	task.Operation = strings.TrimSpace(task.Operation)
	task.State = strings.TrimSpace(task.State)
	task.FinalPowerState = strings.TrimSpace(task.FinalPowerState)
	task.ErrorDetail = strings.TrimSpace(task.ErrorDetail)
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = time.Now().UTC()
	}

	query := s.sb.
		Update("power.transition_tasks").
		Set("transition_id", task.TransitionID).
		Set("node_id", task.NodeID).
		Set("bmc_id", task.BMCID).
		Set("bmc_endpoint", task.BMCEndpoint).
		Set("operation", task.Operation).
		Set("state", task.State).
		Set("dry_run", task.DryRun).
		Set("attempt_count", task.AttemptCount).
		Set("final_power_state", task.FinalPowerState).
		Set("error_detail", task.ErrorDetail).
		Set("queued_at", task.QueuedAt.UTC()).
		Set("started_at", optionalTimeValue(task.StartedAt)).
		Set("completed_at", optionalTimeValue(task.CompletedAt)).
		Set("updated_at", task.UpdatedAt.UTC()).
		Where(sq.Eq{"id": id})

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return engine.Task{}, fmt.Errorf("building transition task update query: %w", err)
	}

	res, err := s.db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return engine.Task{}, fmt.Errorf("updating transition task %q: %w", id, err)
	}

	affected, err := rowsAffectedAsInt(res, "transition task update")
	if err != nil {
		return engine.Task{}, err
	}
	if affected == 0 {
		return engine.Task{}, ErrNotFound
	}

	return task, nil
}

// ListTransitions returns paginated transitions ordered by newest first.
func (s *PostgresStore) ListTransitions(ctx context.Context, limit, offset int) ([]engine.Transition, int, error) {
	limit = normalizeTransitionPageLimit(limit)
	if offset < 0 {
		offset = 0
	}

	countQuery := s.sb.Select("COUNT(*)").From("power.transitions")
	countSQL, countArgs, err := countQuery.ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("building transitions count query: %w", err)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting transitions: %w", err)
	}

	query := s.sb.
		Select(
			"id",
			"request_id",
			"operation",
			"state",
			"requested_by",
			"dry_run",
			"target_count",
			"success_count",
			"failure_count",
			"queued_at",
			"started_at",
			"completed_at",
			"created_at",
			"updated_at",
		).
		From("power.transitions").
		OrderBy("queued_at DESC", "id DESC").
		Limit(uint64(limit)).
		Offset(uint64(offset))

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("building transitions list query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing transitions: %w", err)
	}
	defer rows.Close()

	items := make([]engine.Transition, 0, limit)
	for rows.Next() {
		item, scanErr := scanTransition(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, item)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, 0, fmt.Errorf("iterating transition rows: %w", rowsErr)
	}

	return items, total, nil
}

// GetTransition returns a single transition by ID.
func (s *PostgresStore) GetTransition(ctx context.Context, id string) (engine.Transition, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return engine.Transition{}, ErrNotFound
	}

	query := s.sb.
		Select(
			"id",
			"request_id",
			"operation",
			"state",
			"requested_by",
			"dry_run",
			"target_count",
			"success_count",
			"failure_count",
			"queued_at",
			"started_at",
			"completed_at",
			"created_at",
			"updated_at",
		).
		From("power.transitions").
		Where(sq.Eq{"id": id})

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return engine.Transition{}, fmt.Errorf("building transition get query: %w", err)
	}

	row := s.db.QueryRowContext(ctx, sqlStr, args...)
	transition, err := scanTransition(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return engine.Transition{}, ErrNotFound
		}
		return engine.Transition{}, err
	}

	return transition, nil
}

// ListTransitionTasks returns all tasks for a transition ordered by node ID.
func (s *PostgresStore) ListTransitionTasks(ctx context.Context, transitionID string) ([]engine.Task, error) {
	transitionID = strings.TrimSpace(transitionID)
	if transitionID == "" {
		return []engine.Task{}, nil
	}

	query := s.sb.
		Select(
			"id",
			"transition_id",
			"node_id",
			"bmc_id",
			"bmc_endpoint",
			"operation",
			"state",
			"dry_run",
			"attempt_count",
			"final_power_state",
			"error_detail",
			"queued_at",
			"started_at",
			"completed_at",
			"created_at",
			"updated_at",
		).
		From("power.transition_tasks").
		Where(sq.Eq{"transition_id": transitionID}).
		OrderBy("node_id ASC")

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building transition task list query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("listing transition tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]engine.Task, 0)
	for rows.Next() {
		task, scanErr := scanTransitionTask(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		tasks = append(tasks, task)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterating transition task rows: %w", rowsErr)
	}

	return tasks, nil
}

// ListLatestTransitionTasksByNode returns latest task row per node for requested node IDs.
func (s *PostgresStore) ListLatestTransitionTasksByNode(ctx context.Context, nodeIDs []string) ([]engine.Task, error) {
	_, queryNodeIDs := normalizeNodeIDs(nodeIDs)
	if len(queryNodeIDs) == 0 {
		return []engine.Task{}, nil
	}

	query := s.sb.
		Select(
			"DISTINCT ON (node_id) id",
			"transition_id",
			"node_id",
			"bmc_id",
			"bmc_endpoint",
			"operation",
			"state",
			"dry_run",
			"attempt_count",
			"final_power_state",
			"error_detail",
			"queued_at",
			"started_at",
			"completed_at",
			"created_at",
			"updated_at",
		).
		From("power.transition_tasks").
		Where(sq.Eq{"node_id": queryNodeIDs}).
		OrderBy("node_id ASC", "updated_at DESC", "created_at DESC")

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building latest transition task query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("listing latest transition tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]engine.Task, 0, len(queryNodeIDs))
	for rows.Next() {
		task, scanErr := scanTransitionTask(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		tasks = append(tasks, task)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterating latest transition task rows: %w", rowsErr)
	}

	return tasks, nil
}

func (s *PostgresStore) insertTransitionTx(
	ctx context.Context,
	tx *sql.Tx,
	transition engine.Transition,
) (engine.Transition, error) {
	now := time.Now().UTC()
	transition.ID = strings.TrimSpace(transition.ID)
	transition.RequestID = strings.TrimSpace(transition.RequestID)
	transition.Operation = strings.TrimSpace(transition.Operation)
	transition.State = strings.TrimSpace(transition.State)
	transition.RequestedBy = strings.TrimSpace(transition.RequestedBy)
	if transition.QueuedAt.IsZero() {
		transition.QueuedAt = now
	}
	if transition.CreatedAt.IsZero() {
		transition.CreatedAt = now
	}
	if transition.UpdatedAt.IsZero() {
		transition.UpdatedAt = transition.CreatedAt
	}

	query := s.sb.Insert("power.transitions")
	if transition.ID != "" {
		query = query.
			Columns(
				"id",
				"request_id",
				"operation",
				"state",
				"requested_by",
				"dry_run",
				"target_count",
				"success_count",
				"failure_count",
				"queued_at",
				"started_at",
				"completed_at",
				"created_at",
				"updated_at",
			).
			Values(
				transition.ID,
				transition.RequestID,
				transition.Operation,
				transition.State,
				transition.RequestedBy,
				transition.DryRun,
				transition.TargetCount,
				transition.SuccessCount,
				transition.FailureCount,
				transition.QueuedAt.UTC(),
				optionalTimeValue(transition.StartedAt),
				optionalTimeValue(transition.CompletedAt),
				transition.CreatedAt.UTC(),
				transition.UpdatedAt.UTC(),
			)
	} else {
		query = query.
			Columns(
				"request_id",
				"operation",
				"state",
				"requested_by",
				"dry_run",
				"target_count",
				"success_count",
				"failure_count",
				"queued_at",
				"started_at",
				"completed_at",
				"created_at",
				"updated_at",
			).
			Values(
				transition.RequestID,
				transition.Operation,
				transition.State,
				transition.RequestedBy,
				transition.DryRun,
				transition.TargetCount,
				transition.SuccessCount,
				transition.FailureCount,
				transition.QueuedAt.UTC(),
				optionalTimeValue(transition.StartedAt),
				optionalTimeValue(transition.CompletedAt),
				transition.CreatedAt.UTC(),
				transition.UpdatedAt.UTC(),
			)
	}

	query = query.Suffix(`
RETURNING id,
          request_id,
          operation,
          state,
          requested_by,
          dry_run,
          target_count,
          success_count,
          failure_count,
          queued_at,
          started_at,
          completed_at,
          created_at,
          updated_at`)

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return engine.Transition{}, fmt.Errorf("building transition insert query: %w", err)
	}

	row := tx.QueryRowContext(ctx, sqlStr, args...)
	created, err := scanTransition(row)
	if err != nil {
		return engine.Transition{}, fmt.Errorf("inserting transition: %w", err)
	}
	return created, nil
}

func (s *PostgresStore) insertTransitionTaskTx(
	ctx context.Context,
	tx *sql.Tx,
	task engine.Task,
) (engine.Task, error) {
	now := time.Now().UTC()
	task.ID = strings.TrimSpace(task.ID)
	task.TransitionID = strings.TrimSpace(task.TransitionID)
	task.NodeID = strings.TrimSpace(task.NodeID)
	task.BMCID = strings.TrimSpace(task.BMCID)
	task.BMCEndpoint = strings.TrimSpace(task.BMCEndpoint)
	task.Operation = strings.TrimSpace(task.Operation)
	task.State = strings.TrimSpace(task.State)
	task.FinalPowerState = strings.TrimSpace(task.FinalPowerState)
	task.ErrorDetail = strings.TrimSpace(task.ErrorDetail)
	if task.QueuedAt.IsZero() {
		task.QueuedAt = now
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = task.CreatedAt
	}

	query := s.sb.Insert("power.transition_tasks")
	if task.ID != "" {
		query = query.
			Columns(
				"id",
				"transition_id",
				"node_id",
				"bmc_id",
				"bmc_endpoint",
				"operation",
				"state",
				"dry_run",
				"attempt_count",
				"final_power_state",
				"error_detail",
				"queued_at",
				"started_at",
				"completed_at",
				"created_at",
				"updated_at",
			).
			Values(
				task.ID,
				task.TransitionID,
				task.NodeID,
				task.BMCID,
				task.BMCEndpoint,
				task.Operation,
				task.State,
				task.DryRun,
				task.AttemptCount,
				task.FinalPowerState,
				task.ErrorDetail,
				task.QueuedAt.UTC(),
				optionalTimeValue(task.StartedAt),
				optionalTimeValue(task.CompletedAt),
				task.CreatedAt.UTC(),
				task.UpdatedAt.UTC(),
			)
	} else {
		query = query.
			Columns(
				"transition_id",
				"node_id",
				"bmc_id",
				"bmc_endpoint",
				"operation",
				"state",
				"dry_run",
				"attempt_count",
				"final_power_state",
				"error_detail",
				"queued_at",
				"started_at",
				"completed_at",
				"created_at",
				"updated_at",
			).
			Values(
				task.TransitionID,
				task.NodeID,
				task.BMCID,
				task.BMCEndpoint,
				task.Operation,
				task.State,
				task.DryRun,
				task.AttemptCount,
				task.FinalPowerState,
				task.ErrorDetail,
				task.QueuedAt.UTC(),
				optionalTimeValue(task.StartedAt),
				optionalTimeValue(task.CompletedAt),
				task.CreatedAt.UTC(),
				task.UpdatedAt.UTC(),
			)
	}

	query = query.Suffix(`
RETURNING id,
          transition_id,
          node_id,
          bmc_id,
          bmc_endpoint,
          operation,
          state,
          dry_run,
          attempt_count,
          final_power_state,
          error_detail,
          queued_at,
          started_at,
          completed_at,
          created_at,
          updated_at`)

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return engine.Task{}, fmt.Errorf("building transition task insert query: %w", err)
	}

	row := tx.QueryRowContext(ctx, sqlStr, args...)
	persistedTask, err := scanTransitionTask(row)
	if err != nil {
		return engine.Task{}, fmt.Errorf("inserting transition task for node %q: %w", task.NodeID, err)
	}

	// These execution-only fields are intentionally not persisted in the DB schema
	// yet, but they are required by the in-memory runner after task creation.
	persistedTask.CredentialID = task.CredentialID
	persistedTask.InsecureSkipVerify = task.InsecureSkipVerify

	return persistedTask, nil
}

func scanTransition(scanner interface {
	Scan(dest ...any) error
}) (engine.Transition, error) {
	var out engine.Transition
	var startedAt sql.NullTime
	var completedAt sql.NullTime

	err := scanner.Scan(
		&out.ID,
		&out.RequestID,
		&out.Operation,
		&out.State,
		&out.RequestedBy,
		&out.DryRun,
		&out.TargetCount,
		&out.SuccessCount,
		&out.FailureCount,
		&out.QueuedAt,
		&startedAt,
		&completedAt,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return engine.Transition{}, err
	}

	out.StartedAt = nullTimePtr(startedAt)
	out.CompletedAt = nullTimePtr(completedAt)
	return out, nil
}

func scanTransitionTask(scanner interface {
	Scan(dest ...any) error
}) (engine.Task, error) {
	var out engine.Task
	var startedAt sql.NullTime
	var completedAt sql.NullTime

	err := scanner.Scan(
		&out.ID,
		&out.TransitionID,
		&out.NodeID,
		&out.BMCID,
		&out.BMCEndpoint,
		&out.Operation,
		&out.State,
		&out.DryRun,
		&out.AttemptCount,
		&out.FinalPowerState,
		&out.ErrorDetail,
		&out.QueuedAt,
		&startedAt,
		&completedAt,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return engine.Task{}, err
	}

	out.StartedAt = nullTimePtr(startedAt)
	out.CompletedAt = nullTimePtr(completedAt)
	return out, nil
}

func optionalTimeValue(v *time.Time) any {
	if v == nil {
		return nil
	}
	return v.UTC()
}

func nullTimePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time.UTC()
	return &t
}

func normalizeTransitionPageLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultTransitionPageLimit
	case limit > maxTransitionPageLimit:
		return maxTransitionPageLimit
	default:
		return limit
	}
}
