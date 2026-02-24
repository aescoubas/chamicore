// Package store provides mapping persistence for the power service.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"

	"git.cscs.ch/openchami/chamicore-power/internal/model"
)

// ReplaceTopologyMappings reconciles cached mapping rows against the provided desired topology.
func (s *PostgresStore) ReplaceTopologyMappings(
	ctx context.Context,
	endpoints []model.BMCEndpoint,
	links []model.NodeBMCLink,
	syncedAt time.Time,
) (model.MappingApplyCounts, error) {
	cleanEndpoints := normalizeEndpoints(endpoints)
	cleanLinks := normalizeLinks(links)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.MappingApplyCounts{}, fmt.Errorf("starting mapping transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	counts := model.MappingApplyCounts{}

	for _, endpoint := range cleanEndpoints {
		query := s.sb.
			Insert("power.bmc_endpoints").
			Columns(
				"bmc_id",
				"endpoint",
				"credential_id",
				"insecure_skip_verify",
				"source",
				"last_synced_at",
				"created_at",
				"updated_at",
			).
			Values(
				endpoint.BMCID,
				endpoint.Endpoint,
				endpoint.CredentialID,
				endpoint.InsecureSkipVerify,
				endpoint.Source,
				syncedAt,
				syncedAt,
				syncedAt,
			).
			Suffix(`
ON CONFLICT (bmc_id) DO UPDATE SET
  endpoint = EXCLUDED.endpoint,
  credential_id = EXCLUDED.credential_id,
  insecure_skip_verify = EXCLUDED.insecure_skip_verify,
  source = EXCLUDED.source,
  last_synced_at = EXCLUDED.last_synced_at,
  updated_at = EXCLUDED.updated_at`)

		sqlStr, args, sqlErr := query.ToSql()
		if sqlErr != nil {
			return model.MappingApplyCounts{}, fmt.Errorf("building endpoint upsert query: %w", sqlErr)
		}
		if _, execErr := tx.ExecContext(ctx, sqlStr, args...); execErr != nil {
			return model.MappingApplyCounts{}, fmt.Errorf("upserting endpoint %q: %w", endpoint.BMCID, execErr)
		}
		counts.EndpointsUpserted++
	}

	for _, link := range cleanLinks {
		query := s.sb.
			Insert("power.node_bmc_links").
			Columns(
				"node_id",
				"bmc_id",
				"source",
				"last_synced_at",
				"created_at",
				"updated_at",
			).
			Values(
				link.NodeID,
				link.BMCID,
				link.Source,
				syncedAt,
				syncedAt,
				syncedAt,
			).
			Suffix(`
ON CONFLICT (node_id) DO UPDATE SET
  bmc_id = EXCLUDED.bmc_id,
  source = EXCLUDED.source,
  last_synced_at = EXCLUDED.last_synced_at,
  updated_at = EXCLUDED.updated_at`)

		sqlStr, args, sqlErr := query.ToSql()
		if sqlErr != nil {
			return model.MappingApplyCounts{}, fmt.Errorf("building link upsert query: %w", sqlErr)
		}
		if _, execErr := tx.ExecContext(ctx, sqlStr, args...); execErr != nil {
			return model.MappingApplyCounts{}, fmt.Errorf("upserting link for node %q: %w", link.NodeID, execErr)
		}
		counts.LinksUpserted++
	}

	deletedLinks, err := s.deleteStaleLinks(ctx, tx, cleanLinks)
	if err != nil {
		return model.MappingApplyCounts{}, err
	}
	counts.LinksDeleted = deletedLinks

	deletedEndpoints, err := s.deleteStaleEndpoints(ctx, tx, cleanEndpoints)
	if err != nil {
		return model.MappingApplyCounts{}, err
	}
	counts.EndpointsDeleted = deletedEndpoints

	if commitErr := tx.Commit(); commitErr != nil {
		return model.MappingApplyCounts{}, fmt.Errorf("committing mapping transaction: %w", commitErr)
	}

	return counts, nil
}

// ResolveNodeMappings resolves per-node mapping and returns actionable per-node failures.
func (s *PostgresStore) ResolveNodeMappings(
	ctx context.Context,
	nodeIDs []string,
) ([]model.NodePowerMapping, []model.NodeMappingError, error) {
	requestedNodes, queryNodes := normalizeNodeIDs(nodeIDs)
	if len(requestedNodes) == 0 {
		return []model.NodePowerMapping{}, []model.NodeMappingError{}, nil
	}

	query := s.sb.
		Select(
			"l.node_id",
			"l.bmc_id",
			"e.endpoint",
			"e.credential_id",
			"e.insecure_skip_verify",
		).
		From("power.node_bmc_links l").
		Join("power.bmc_endpoints e ON e.bmc_id = l.bmc_id").
		Where(sq.Eq{"l.node_id": queryNodes})

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return nil, nil, fmt.Errorf("building mapping resolve query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("querying node mappings: %w", err)
	}
	defer rows.Close()

	byNode := make(map[string]model.NodePowerMapping, len(queryNodes))
	for rows.Next() {
		var row model.NodePowerMapping
		if scanErr := rows.Scan(
			&row.NodeID,
			&row.BMCID,
			&row.Endpoint,
			&row.CredentialID,
			&row.InsecureSkipVerify,
		); scanErr != nil {
			return nil, nil, fmt.Errorf("scanning mapping row: %w", scanErr)
		}
		byNode[row.NodeID] = row
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, nil, fmt.Errorf("iterating mapping rows: %w", rowsErr)
	}

	resolved := make([]model.NodePowerMapping, 0, len(requestedNodes))
	missing := make([]model.NodeMappingError, 0)
	for _, nodeID := range requestedNodes {
		row, ok := byNode[nodeID]
		if !ok {
			missing = append(missing, model.MissingNodeMappingError(nodeID))
			continue
		}
		if strings.TrimSpace(row.Endpoint) == "" {
			missing = append(missing, model.MissingEndpointError(nodeID, row.BMCID))
			continue
		}
		if strings.TrimSpace(row.CredentialID) == "" {
			missing = append(missing, model.MissingCredentialError(nodeID, row.BMCID))
			continue
		}
		resolved = append(resolved, row)
	}

	return resolved, missing, nil
}

// ListBMCEndpoints returns all cached BMC endpoint rows.
func (s *PostgresStore) ListBMCEndpoints(ctx context.Context) ([]model.BMCEndpoint, error) {
	query := s.sb.
		Select(
			"bmc_id",
			"endpoint",
			"credential_id",
			"insecure_skip_verify",
			"source",
			"last_synced_at",
			"created_at",
			"updated_at",
		).
		From("power.bmc_endpoints").
		OrderBy("bmc_id")

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list BMC endpoints query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("listing BMC endpoints: %w", err)
	}
	defer rows.Close()

	items := make([]model.BMCEndpoint, 0)
	for rows.Next() {
		var item model.BMCEndpoint
		if scanErr := rows.Scan(
			&item.BMCID,
			&item.Endpoint,
			&item.CredentialID,
			&item.InsecureSkipVerify,
			&item.Source,
			&item.LastSyncedAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("scanning BMC endpoint row: %w", scanErr)
		}
		items = append(items, item)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterating BMC endpoint rows: %w", rowsErr)
	}

	return items, nil
}

// ListNodeBMCLinks returns all cached node->BMC links.
func (s *PostgresStore) ListNodeBMCLinks(ctx context.Context) ([]model.NodeBMCLink, error) {
	query := s.sb.
		Select(
			"node_id",
			"bmc_id",
			"source",
			"last_synced_at",
			"created_at",
			"updated_at",
		).
		From("power.node_bmc_links").
		OrderBy("node_id")

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list node links query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("listing node links: %w", err)
	}
	defer rows.Close()

	items := make([]model.NodeBMCLink, 0)
	for rows.Next() {
		var item model.NodeBMCLink
		if scanErr := rows.Scan(
			&item.NodeID,
			&item.BMCID,
			&item.Source,
			&item.LastSyncedAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("scanning node link row: %w", scanErr)
		}
		items = append(items, item)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterating node link rows: %w", rowsErr)
	}

	return items, nil
}

func (s *PostgresStore) deleteStaleLinks(
	ctx context.Context,
	tx *sql.Tx,
	desiredLinks []model.NodeBMCLink,
) (int, error) {
	query := s.sb.Delete("power.node_bmc_links")
	if len(desiredLinks) > 0 {
		nodeIDs := make([]string, 0, len(desiredLinks))
		for _, link := range desiredLinks {
			nodeIDs = append(nodeIDs, link.NodeID)
		}
		query = query.Where(sq.NotEq{"node_id": nodeIDs})
	}

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return 0, fmt.Errorf("building stale-link delete query: %w", err)
	}
	res, err := tx.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return 0, fmt.Errorf("deleting stale node links: %w", err)
	}
	return rowsAffectedAsInt(res, "stale node links")
}

func (s *PostgresStore) deleteStaleEndpoints(
	ctx context.Context,
	tx *sql.Tx,
	desiredEndpoints []model.BMCEndpoint,
) (int, error) {
	query := s.sb.Delete("power.bmc_endpoints")
	if len(desiredEndpoints) > 0 {
		bmcIDs := make([]string, 0, len(desiredEndpoints))
		for _, endpoint := range desiredEndpoints {
			bmcIDs = append(bmcIDs, endpoint.BMCID)
		}
		query = query.Where(sq.NotEq{"bmc_id": bmcIDs})
	}

	sqlStr, args, err := query.ToSql()
	if err != nil {
		return 0, fmt.Errorf("building stale-endpoint delete query: %w", err)
	}
	res, err := tx.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return 0, fmt.Errorf("deleting stale BMC endpoints: %w", err)
	}
	return rowsAffectedAsInt(res, "stale BMC endpoints")
}

func rowsAffectedAsInt(res sql.Result, label string) (int, error) {
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reading rows affected for %s: %w", label, err)
	}
	if affected < 0 {
		return 0, nil
	}
	return int(affected), nil
}

func normalizeEndpoints(items []model.BMCEndpoint) []model.BMCEndpoint {
	byBMC := make(map[string]model.BMCEndpoint, len(items))
	for _, item := range items {
		bmcID := strings.TrimSpace(item.BMCID)
		if bmcID == "" {
			continue
		}
		item.BMCID = bmcID
		item.Endpoint = strings.TrimSpace(item.Endpoint)
		item.CredentialID = strings.TrimSpace(item.CredentialID)
		item.Source = strings.TrimSpace(item.Source)
		if item.Source == "" {
			item.Source = "smd"
		}
		byBMC[bmcID] = item
	}

	keys := make([]string, 0, len(byBMC))
	for k := range byBMC {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]model.BMCEndpoint, 0, len(keys))
	for _, key := range keys {
		result = append(result, byBMC[key])
	}
	return result
}

func normalizeLinks(items []model.NodeBMCLink) []model.NodeBMCLink {
	byNode := make(map[string]model.NodeBMCLink, len(items))
	for _, item := range items {
		nodeID := strings.TrimSpace(item.NodeID)
		bmcID := strings.TrimSpace(item.BMCID)
		if nodeID == "" || bmcID == "" {
			continue
		}
		item.NodeID = nodeID
		item.BMCID = bmcID
		item.Source = strings.TrimSpace(item.Source)
		if item.Source == "" {
			item.Source = "smd"
		}
		byNode[nodeID] = item
	}

	keys := make([]string, 0, len(byNode))
	for k := range byNode {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]model.NodeBMCLink, 0, len(keys))
	for _, key := range keys {
		result = append(result, byNode[key])
	}
	return result
}

func normalizeNodeIDs(nodeIDs []string) ([]string, []string) {
	requested := make([]string, 0, len(nodeIDs))
	unique := make([]string, 0, len(nodeIDs))
	seen := make(map[string]struct{}, len(nodeIDs))

	for _, raw := range nodeIDs {
		nodeID := strings.TrimSpace(raw)
		if nodeID == "" {
			continue
		}
		requested = append(requested, nodeID)
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		unique = append(unique, nodeID)
	}

	return requested, unique
}
