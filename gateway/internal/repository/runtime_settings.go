package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/management"
)

func (p *Postgres) GetRuntimeSetting(ctx context.Context) (*management.RuntimeSetting, error) {
	var payload []byte
	var updatedAt time.Time
	err := p.pool.QueryRow(ctx, `SELECT value::text, updated_at
		FROM gateway_system_setting WHERE setting_key=$1`, "runtime").Scan(&payload, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(payload, &document); err != nil {
		return nil, fmt.Errorf("decode runtime setting: %w", err)
	}
	config, err := config.NormalizeSharedRuntime(document)
	if err != nil {
		return nil, fmt.Errorf("normalize runtime setting: %w", err)
	}
	revision, err := p.currentConfigRevision(ctx)
	if err != nil {
		return nil, err
	}
	updated := updatedAt.Unix()
	return &management.RuntimeSetting{
		Config:    config,
		Revision:  management.ID(revision),
		UpdatedAt: &updated,
		ApplyMode: "restart",
	}, nil
}

func (p *Postgres) currentConfigRevision(ctx context.Context) (int64, error) {
	var revision int64
	if err := p.pool.QueryRow(ctx, "SELECT revision FROM gateway_config_revision WHERE singleton_id=1").Scan(&revision); err != nil {
		return 0, err
	}
	return revision, nil
}

func (p *Postgres) SetRuntimeSetting(ctx context.Context, input management.RuntimeSettingUpsertRequest) (*management.RuntimeSetting, error) {
	if input.Config == nil {
		return nil, fmt.Errorf("runtime config is required")
	}
	plain, err := json.Marshal(input.Config)
	if err != nil {
		return nil, fmt.Errorf("encode runtime setting: %w", err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var current int64
	if err := tx.QueryRow(ctx, "SELECT revision FROM gateway_config_revision WHERE singleton_id=1 FOR UPDATE").Scan(&current); err != nil {
		return nil, err
	}
	if input.ExpectedRevision != nil && int64(*input.ExpectedRevision) != current {
		return nil, fmt.Errorf("runtime config revision conflict: current=%d", current)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO gateway_system_setting(setting_key,value,content_type,updated_at)
		VALUES($1,$2::jsonb,'application/json',clock_timestamp())
		ON CONFLICT(setting_key) DO UPDATE SET value=excluded.value, content_type=excluded.content_type, updated_at=excluded.updated_at`,
		"runtime", string(plain)); err != nil {
		return nil, err
	}
	if input.Restart {
		if _, err := tx.Exec(ctx, `UPDATE gateway_node
			SET restart_generation=restart_generation+1,
			    restart_requested_at=clock_timestamp(),
			    restart_error='',
			state_reason=CASE WHEN status='active' THEN 'restart:runtime-config' ELSE state_reason END
			WHERE status='active'`); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p.GetRuntimeSetting(ctx)
}

func (p *Postgres) RuntimeSettingStatus(ctx context.Context) (management.RuntimeSettingStatus, error) {
	setting, err := p.GetRuntimeSetting(ctx)
	if err != nil {
		return management.RuntimeSettingStatus{}, err
	}
	revision := int64(0)
	if setting != nil {
		revision = int64(setting.Revision)
	}
	nodes, err := p.SearchGatewayNodes(ctx, management.GatewayNodeSearchRequest{PageRequest: management.PageRequest{PageIndex: 1, PageSize: management.MaxPageSize}})
	if err != nil {
		return management.RuntimeSettingStatus{}, err
	}
	pending := false
	for _, node := range nodes.Records {
		if node.Status == "active" && node.AppliedRevision < management.ID(revision) {
			pending = true
		}
		if strings.HasPrefix(node.StateReason, "restart:") {
			pending = true
		}
	}
	return management.RuntimeSettingStatus{
		Revision:       management.ID(revision),
		PendingRestart: pending,
		Nodes:          nodes.Records,
		ApplyMode:      "restart",
	}, nil
}

// ClaimNodeRestart reserves one active node for a rolling restart. The
// advisory lock plus the draining marker prevent multiple nodes from leaving
// the data plane at the same time.
func (p *Postgres) ClaimNodeRestart(ctx context.Context, nodeID string) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('yaginx:rolling-restart', 0))"); err != nil {
		return false, err
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM gateway_node
		WHERE status IN ('active', 'draining') AND state_reason LIKE 'restart:%'
		  AND COALESCE(extract(epoch FROM (clock_timestamp() - last_heartbeat_at)) * 1000000000 < $1, false)
	)`, p.heartbeatStaleAfter.Nanoseconds()).Scan(&active); err != nil {
		return false, err
	}
	if active {
		return false, tx.Commit(ctx)
	}
	var claimed bool
	if err := tx.QueryRow(ctx, `UPDATE gateway_node
		SET status='draining', state_reason='restart:runtime-config',
		    drain_started_at=clock_timestamp(), state_changed_at=clock_timestamp()
		WHERE node_id=$1::uuid AND status='active'
		  AND restart_generation > observed_restart_generation
		RETURNING true`, nodeID).Scan(&claimed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, tx.Commit(ctx)
		}
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return claimed, nil
}

func (p *Postgres) MarkNodeReady(ctx context.Context, nodeID string) error {
	_, err := p.pool.Exec(ctx, `UPDATE gateway_node
		SET state_reason='', restart_completed_at=clock_timestamp(), state_changed_at=clock_timestamp()
		WHERE node_id=$1::uuid AND status='active' AND state_reason='restart:starting'`, nodeID)
	return err
}

func (p *Postgres) RestartGatewayNode(ctx context.Context, nodeID string) (management.GatewayNodeRestartResult, error) {
	var generation int64
	err := p.pool.QueryRow(ctx, `UPDATE gateway_node
		SET restart_generation=restart_generation+1,
		    restart_requested_at=clock_timestamp(),
		    restart_error='',
		    state_reason='restart:manual'
		WHERE node_id=$1::uuid AND status='active'
		RETURNING restart_generation`, nodeID).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.GatewayNodeRestartResult{}, management.ErrNotFound
	}
	if err != nil {
		return management.GatewayNodeRestartResult{}, err
	}
	return management.GatewayNodeRestartResult{NodeID: nodeID, RestartGeneration: management.ID(generation)}, nil
}
