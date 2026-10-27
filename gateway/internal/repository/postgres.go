// Package repository owns PostgreSQL access for management and runtime paths.
// Keep SQL here: HTTP handlers must not query the database directly.
package repository

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool                    *pgxpool.Pool
	requestID               func() (int64, error)
	heartbeatStaleAfter     time.Duration
	inactiveNodeAfter       time.Duration
	trafficMaterializerLock string
	contributionRetention   time.Duration
	ipProfileEndpoint       string
	ipProfileClient         *http.Client
	analyticsObserver       func(operation, source string, rows int, loadDuration, buildDuration time.Duration)
	systemNodeID            string
	systemVersion           string
	systemBuildID           string
	systemConfigRevision    atomic.Int64
	systemConfigRevisionSet atomic.Bool
}

func New(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool, heartbeatStaleAfter: 45 * time.Second, inactiveNodeAfter: 168 * time.Hour, trafficMaterializerLock: "yaginx:resource-report", contributionRetention: 7 * 24 * time.Hour, ipProfileEndpoint: "https://tools.abc.red", ipProfileClient: &http.Client{Timeout: 3 * time.Second}}
}

// SetRequestIDGenerator activates Snowflake IDs after this process has an
// active node number. Tests and migration-only tools retain the legacy
// sequence fallback until they explicitly opt in.
func (p *Postgres) SetRequestIDGenerator(next func() (int64, error)) { p.requestID = next }

// SetNodeHeartbeatStaleAfter supplies the runtime threshold used when the
// control plane decides whether disabling an active node first drains it.
func (p *Postgres) SetNodeHeartbeatStaleAfter(value time.Duration) {
	if value > 0 {
		p.heartbeatStaleAfter = value
	}
}

func (p *Postgres) SetInactiveNodeAfter(value time.Duration) {
	if value > 0 {
		p.inactiveNodeAfter = value
	}
}

// SetTrafficMaterializerLockNamespace supplies the transaction advisory lock
// namespace shared by report writers on every active node.
func (p *Postgres) SetTrafficMaterializerLockNamespace(value string) {
	if value = strings.TrimSpace(value); value != "" {
		p.trafficMaterializerLock = value
	}
}

// SetContributionRetention aligns the analytics hot contribution source
// with the retention policy enforced by the telemetry pruning task.
func (p *Postgres) SetContributionRetention(value time.Duration) {
	if value > 0 {
		p.contributionRetention = value
	}
}

// SetObservabilityObserver records bounded dashboard load facts without
// exposing Host or request-derived labels to the metrics system.
func (p *Postgres) SetObservabilityObserver(observer func(operation, source string, rows int, loadDuration, buildDuration time.Duration)) {
	p.analyticsObserver = observer
}

// SetIPProfileTimeout adjusts the lookup client timeout. The provider endpoint is
// fixed to the public tools.abc.red service and is not configurable.
func (p *Postgres) SetIPProfileTimeout(timeout time.Duration) {
	if timeout > 0 {
		p.ipProfileClient = &http.Client{Timeout: timeout}
	}
}

func (p *Postgres) databaseNow(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := p.pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return time.Time{}, err
	}
	return now.UTC(), nil
}
