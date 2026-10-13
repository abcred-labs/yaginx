package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"yaginx-gateway/internal/management"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Gateway node registry: registration, approval, heartbeats and drain.

func (p *Postgres) RegisterNode(ctx context.Context, input management.NodeRegistrationRequest) (management.NodeSummary, error) {
	id := input.NodeID
	fingerprint := strings.ToLower(input.MachineFingerprint)
	if _, err := uuid.Parse(id); err != nil {
		return management.NodeSummary{}, fmt.Errorf("nodeId must be UUID")
	}
	if len(fingerprint) != 64 {
		return management.NodeSummary{}, fmt.Errorf("machineFingerprint must be SHA-256 hex")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return management.NodeSummary{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(6431500123034371918)"); err != nil {
		return management.NodeSummary{}, err
	}
	var status, reason string
	var number *int16
	var restartGeneration, observedRestartGeneration int64
	err = tx.QueryRow(ctx, `INSERT INTO gateway_node(node_id,fingerprint,machine_fingerprint,display_name,software_version,git_sha,status,node_number,last_heartbeat_at,approved_at,state_changed_at)
		VALUES($1::uuid,$2::text,$2::char(64),$3,$4,$5,'active',
			(SELECT n::smallint FROM generate_series(0,1023) AS n WHERE NOT EXISTS (SELECT 1 FROM gateway_node WHERE node_number=n) ORDER BY n LIMIT 1),
			clock_timestamp(),clock_timestamp(),clock_timestamp())
		ON CONFLICT(node_id) DO UPDATE SET display_name=excluded.display_name,software_version=excluded.software_version,
		git_sha=excluded.git_sha,last_heartbeat_at=clock_timestamp(),
		status=CASE WHEN gateway_node.status='draining' AND gateway_node.state_reason LIKE 'restart:%' THEN 'active' ELSE gateway_node.status END,
		state_reason=CASE WHEN gateway_node.status='draining' AND gateway_node.state_reason LIKE 'restart:%' THEN 'restart:starting' ELSE gateway_node.state_reason END,
		restart_completed_at=CASE WHEN gateway_node.status='draining' AND gateway_node.state_reason LIKE 'restart:%' THEN clock_timestamp() ELSE gateway_node.restart_completed_at END,
		observed_restart_generation=CASE WHEN gateway_node.status='draining' AND gateway_node.state_reason LIKE 'restart:%' THEN gateway_node.restart_generation ELSE gateway_node.observed_restart_generation END
		WHERE gateway_node.machine_fingerprint=excluded.machine_fingerprint
		RETURNING status,node_number,state_reason,restart_generation,observed_restart_generation`,
		id, fingerprint, input.DisplayName, input.SoftwareVersion, input.GitSHA).Scan(&status, &number, &reason, &restartGeneration, &observedRestartGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.NodeSummary{}, fmt.Errorf("node ID is already bound to another machine fingerprint")
	}
	if err != nil {
		return management.NodeSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return management.NodeSummary{}, err
	}
	return nodeMap(id, status, number, reason, restartGeneration, observedRestartGeneration), nil
}

// NodeStatus is the process-side gate for data-plane and ACME work. It is
// intentionally not inferred from a stale local snapshot: PostgreSQL remains
// authoritative for approval, draining and disable decisions.
func (p *Postgres) NodeStatus(ctx context.Context, id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("nodeId must be UUID")
	}
	var status string
	err := p.pool.QueryRow(ctx, "SELECT status FROM gateway_node WHERE node_id=$1::uuid", id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", management.ErrNotFound
	}
	return status, err
}

func (p *Postgres) NodeNumber(ctx context.Context, id string) (int, error) {
	if _, err := uuid.Parse(id); err != nil {
		return 0, fmt.Errorf("nodeId must be UUID")
	}
	var number *int16
	var status string
	err := p.pool.QueryRow(ctx, "SELECT status,node_number FROM gateway_node WHERE node_id=$1::uuid", id).Scan(&status, &number)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, management.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if status != "active" || number == nil {
		return 0, fmt.Errorf("node is not active")
	}
	return int(*number), nil
}

// ApproveNode is retained as an idempotent compatibility shim for older
// operators. Nodes are automatically activated during registration.
func (p *Postgres) ApproveNode(ctx context.Context, id string) (management.NodeSummary, error) {
	var status string
	var number *int16
	var reason string
	var restartGeneration, observedRestartGeneration int64
	err := p.pool.QueryRow(ctx, "SELECT status,node_number,state_reason,restart_generation,observed_restart_generation FROM gateway_node WHERE node_id=$1::uuid", id).Scan(&status, &number, &reason, &restartGeneration, &observedRestartGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.NodeSummary{}, management.ErrNotFound
	}
	if err != nil {
		return management.NodeSummary{}, err
	}
	return nodeMap(id, status, number, reason, restartGeneration, observedRestartGeneration), nil
}

func (p *Postgres) HeartbeatNode(ctx context.Context, input management.NodeHeartbeatRequest) (management.NodeSummary, error) {
	id := input.NodeID
	if _, err := uuid.Parse(id); err != nil {
		return management.NodeSummary{}, fmt.Errorf("nodeId must be UUID")
	}
	var status string
	var number *int16
	var reason string
	var restartGeneration, observedRestartGeneration int64
	err := p.pool.QueryRow(ctx, `UPDATE gateway_node SET applied_revision=$2,reload_generation=$3,observed_reload_generation=$4,
		restart_generation=$5,observed_restart_generation=$6,last_snapshot_error=$7,last_snapshot_at=clock_timestamp(),
		last_heartbeat_at=clock_timestamp() WHERE node_id=$1::uuid
		RETURNING status,node_number,state_reason,restart_generation,observed_restart_generation`,
		id, input.AppliedRevision, input.ReloadGeneration, input.ObservedReloadGeneration,
		input.RestartGeneration, input.ObservedRestartGeneration, input.LastSnapshotError).Scan(&status, &number, &reason, &restartGeneration, &observedRestartGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.NodeSummary{}, management.ErrNotFound
	}
	if err != nil {
		return management.NodeSummary{}, err
	}
	return nodeMap(id, status, number, reason, restartGeneration, observedRestartGeneration), nil
}

func (p *Postgres) ListNodes(ctx context.Context) ([]management.NodeSummary, error) {
	rows, err := p.pool.Query(ctx, "SELECT node_id::text,status,node_number,state_reason,restart_generation,observed_restart_generation FROM gateway_node ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []management.NodeSummary{}
	for rows.Next() {
		var id, status, reason string
		var number *int16
		var restartGeneration, observedRestartGeneration int64
		if err := rows.Scan(&id, &status, &number, &reason, &restartGeneration, &observedRestartGeneration); err != nil {
			return nil, err
		}
		out = append(out, nodeMap(id, status, number, reason, restartGeneration, observedRestartGeneration))
	}
	return out, rows.Err()
}

func (p *Postgres) SearchGatewayNodes(ctx context.Context, input management.GatewayNodeSearchRequest) (management.GatewayNodePage, error) {
	page, err := management.NormalizePageRequest(input.PageRequest)
	if err != nil {
		return management.GatewayNodePage{}, err
	}
	status := strings.TrimSpace(input.Status)
	if status != "" && status != "active" && status != "draining" && status != "disabled" {
		return management.GatewayNodePage{}, fmt.Errorf("node status is invalid")
	}
	keyword := strings.TrimSpace(input.Keyword)
	var total int64
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM gateway_node WHERE ($1='' OR status=$1) AND ($2='' OR lower(display_name) LIKE '%' || lower($2) || '%' OR machine_fingerprint LIKE lower($2) || '%')", status, keyword).Scan(&total); err != nil {
		return management.GatewayNodePage{}, err
	}
	paging, offset, err := management.PageWindow(page, total)
	if err != nil {
		return management.GatewayNodePage{}, err
	}
	rows, err := p.pool.Query(ctx, "SELECT node_id::text,machine_fingerprint,display_name,status,node_number,state_reason,software_version,git_sha,applied_revision,reload_generation,observed_reload_generation,restart_generation,observed_restart_generation,last_snapshot_at,last_snapshot_error,last_heartbeat_at,approved_at,drain_started_at,disabled_at,created_at,updated_at FROM gateway_node WHERE ($1='' OR status=$1) AND ($2='' OR lower(display_name) LIKE '%' || lower($2) || '%' OR machine_fingerprint LIKE lower($2) || '%') ORDER BY last_heartbeat_at DESC NULLS LAST,node_id LIMIT $3 OFFSET $4", status, keyword, paging.PageSize, offset)
	if err != nil {
		return management.GatewayNodePage{}, err
	}
	defer rows.Close()
	records := []management.GatewayNode{}
	for rows.Next() {
		node, err := scanGatewayNode(rows)
		if err != nil {
			return management.GatewayNodePage{}, err
		}
		node.Stale = node.LastHeartbeatAt == nil || time.Since(time.Unix(*node.LastHeartbeatAt, 0)) > p.inactiveNodeAfter
		records = append(records, node)
	}
	if err := rows.Err(); err != nil {
		return management.GatewayNodePage{}, err
	}
	return management.GatewayNodePage{Records: records, Paging: paging}, nil
}

func (p *Postgres) GatewayNodeHealthSummary(ctx context.Context, cutoff int64) (management.GatewayHealthSummary, error) {
	var out management.GatewayHealthSummary
	err := p.pool.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE status = 'active' AND last_heartbeat_at >= to_timestamp($1))::int,
		       count(*) FILTER (WHERE status = 'active' AND (last_heartbeat_at IS NULL OR last_heartbeat_at < to_timestamp($1)))::int,
		       count(*) FILTER (WHERE status = 'draining')::int,
		       COALESCE(EXTRACT(EPOCH FROM max(last_heartbeat_at) FILTER (WHERE status = 'active' AND last_heartbeat_at >= to_timestamp($1))), 0)::bigint
		FROM gateway_node`, cutoff).Scan(&out.TotalNodes, &out.ActiveNodes, &out.StaleNodes, &out.DrainingNodes, &out.LatestHeartbeat)
	return out, err
}

func (p *Postgres) GetGatewayNode(ctx context.Context, id string) (*management.GatewayNode, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("nodeId must be UUID")
	}
	row := p.pool.QueryRow(ctx, "SELECT node_id::text,machine_fingerprint,display_name,status,node_number,state_reason,software_version,git_sha,applied_revision,reload_generation,observed_reload_generation,restart_generation,observed_restart_generation,last_snapshot_at,last_snapshot_error,last_heartbeat_at,approved_at,drain_started_at,disabled_at,created_at,updated_at FROM gateway_node WHERE node_id=$1::uuid", id)
	node, err := scanGatewayNode(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	node.Stale = node.LastHeartbeatAt == nil || time.Since(time.Unix(*node.LastHeartbeatAt, 0)) > p.inactiveNodeAfter
	return &node, nil
}

func (p *Postgres) DisableGatewayNode(ctx context.Context, id, reason string) (management.GatewayNode, error) {
	if _, err := uuid.Parse(id); err != nil {
		return management.GatewayNode{}, fmt.Errorf("nodeId must be UUID")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 500 {
		return management.GatewayNode{}, fmt.Errorf("disable reason is invalid")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return management.GatewayNode{}, err
	}
	defer tx.Rollback(ctx)
	var status string
	var heartbeatActive bool
	err = tx.QueryRow(ctx, `SELECT status,
	COALESCE(extract(epoch FROM (clock_timestamp() - last_heartbeat_at)) * 1000000000 < $2, false)
	FROM gateway_node WHERE node_id=$1::uuid FOR UPDATE`, id, p.heartbeatStaleAfter.Nanoseconds()).Scan(&status, &heartbeatActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.GatewayNode{}, management.ErrNotFound
	}
	if err != nil {
		return management.GatewayNode{}, err
	}
	next := "disabled"
	if status == "active" && heartbeatActive {
		next = "draining"
	}
	row := tx.QueryRow(ctx, "UPDATE gateway_node SET status=$2,state_reason=$3,state_changed_at=clock_timestamp(),drain_started_at=CASE WHEN $2='draining' THEN clock_timestamp() ELSE drain_started_at END,disabled_at=CASE WHEN $2='disabled' THEN clock_timestamp() ELSE NULL END WHERE node_id=$1::uuid RETURNING node_id::text,machine_fingerprint,display_name,status,node_number,state_reason,software_version,git_sha,applied_revision,reload_generation,observed_reload_generation,restart_generation,observed_restart_generation,last_snapshot_at,last_snapshot_error,last_heartbeat_at,approved_at,drain_started_at,disabled_at,created_at,updated_at", id, next, "disable:"+reason)
	out, err := scanGatewayNode(row)
	if err != nil {
		return management.GatewayNode{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return management.GatewayNode{}, err
	}
	return out, nil
}

func (p *Postgres) DeleteGatewayNode(ctx context.Context, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("nodeId must be UUID")
	}
	result, err := p.pool.Exec(ctx, `DELETE FROM gateway_node
		WHERE node_id=$1::uuid AND (status='disabled' OR
		(status='active' AND (last_heartbeat_at IS NULL OR last_heartbeat_at < clock_timestamp()-$2 * interval '1 second')))`, id, p.inactiveNodeAfter.Seconds())
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("node must be disabled or stale before deletion")
	}
	return nil
}

func (p *Postgres) CompleteNodeDrain(ctx context.Context, id string) error {
	_, err := p.pool.Exec(ctx, "UPDATE gateway_node SET status='disabled',disabled_at=clock_timestamp(),state_changed_at=clock_timestamp() WHERE node_id=$1::uuid AND status='draining'", id)
	return err
}

func (p *Postgres) ReloadGatewayNode(ctx context.Context, id string) (management.GatewayNodeReloadResult, error) {
	if _, err := uuid.Parse(id); err != nil {
		return management.GatewayNodeReloadResult{}, fmt.Errorf("nodeId must be UUID")
	}
	var generation int64
	err := p.pool.QueryRow(ctx, "UPDATE gateway_node SET reload_generation=reload_generation+1 WHERE node_id=$1::uuid AND status='active' RETURNING reload_generation", id).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.GatewayNodeReloadResult{}, management.ErrNotFound
	}
	if err != nil {
		return management.GatewayNodeReloadResult{}, err
	}
	return management.GatewayNodeReloadResult{NodeID: id, ReloadGeneration: management.ID(generation)}, nil
}

type gatewayNodeScanner interface {
	Scan(...any) error
}

func scanGatewayNode(row gatewayNodeScanner) (management.GatewayNode, error) {
	var id, fingerprint, displayName, status, reason, version, gitSHA, snapshotError string
	var number *int16
	var applied, generation, observed, restartGeneration, observedRestartGeneration int64
	var lastSnapshot, heartbeat, approved, drain, disabled *time.Time
	var created, updated time.Time
	if err := row.Scan(&id, &fingerprint, &displayName, &status, &number, &reason, &version, &gitSHA, &applied, &generation, &observed, &restartGeneration, &observedRestartGeneration, &lastSnapshot, &snapshotError, &heartbeat, &approved, &drain, &disabled, &created, &updated); err != nil {
		return management.GatewayNode{}, err
	}
	return management.GatewayNode{
		NodeID:                    id,
		MachineFingerprint:        fingerprint,
		DisplayName:               displayName,
		Status:                    status,
		NodeNumber:                number,
		StateReason:               reason,
		SoftwareVersion:           version,
		GitSHA:                    gitSHA,
		AppliedRevision:           management.ID(applied),
		ReloadGeneration:          management.ID(generation),
		ObservedReloadGeneration:  management.ID(observed),
		RestartGeneration:         management.ID(restartGeneration),
		ObservedRestartGeneration: management.ID(observedRestartGeneration),
		LastSnapshotAt:            epochSeconds(lastSnapshot),
		LastSnapshotError:         snapshotError,
		LastHeartbeatAt:           epochSeconds(heartbeat),
		DrainStartedAt:            epochSeconds(drain),
		DisabledAt:                epochSeconds(disabled),
		CreatedAt:                 created.Unix(),
		UpdatedAt:                 updated.Unix(),
	}, nil
}

func epochSeconds(value *time.Time) *int64 {
	if value == nil {
		return nil
	}
	result := value.Unix()
	return &result
}

func nodeMap(id, status string, number *int16, reason string, restartGeneration, observedRestartGeneration int64) management.NodeSummary {
	return management.NodeSummary{
		NodeID: id, Status: status, NodeNumber: number, StateReason: reason,
		RestartGeneration: restartGeneration, ObservedRestartGeneration: observedRestartGeneration,
	}
}
