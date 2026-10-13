package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"
	"yaginx-gateway/internal/management"
)

const (
	databasePerformanceTimeout         = 5 * time.Second
	databasePerformanceComponentLimit  = 750 * time.Millisecond
	databasePerformanceStatementsLimit = 1500 * time.Millisecond
)

// databasePerformance is intentionally a fixed, read-only snapshot. The
// management API never accepts SQL text, table names, or statistic filters.
func (p *Postgres) DatabasePerformance(ctx context.Context) (management.DatabasePerformance, error) {
	ctx, cancel := context.WithTimeout(ctx, databasePerformanceTimeout)
	defer cancel()

	// These components are independent read-only queries against different
	// system views/tables. Running them concurrently removes their serial
	// round-trip latency; each still carries its own per-component timeout.
	var now time.Time
	var database management.DatabaseStats
	var activity management.DatabaseActivity
	var tables []management.DatabaseTableStats
	var statementsCapability management.DatabaseCapability
	var topSQL []management.DatabaseTopSQL
	var ioCapability management.DatabaseCapability
	var ioStats management.DatabaseIO
	var ioTimingCapability management.DatabaseCapability

	group, _ := errgroup.WithContext(ctx)
	group.Go(func() error {
		nowCtx, cancelNow := databasePerformanceComponentContext(ctx)
		defer cancelNow()
		var err error
		now, err = p.databaseNow(nowCtx)
		return err
	})
	group.Go(func() error {
		databaseCtx, cancelDatabase := databasePerformanceComponentContext(ctx)
		defer cancelDatabase()
		var err error
		database, err = p.databasePerformanceDatabase(databaseCtx)
		return err
	})
	group.Go(func() error {
		activityCtx, cancelActivity := databasePerformanceComponentContext(ctx)
		defer cancelActivity()
		var err error
		activity, err = p.databasePerformanceActivity(activityCtx)
		return err
	})
	group.Go(func() error {
		tablesCtx, cancelTables := databasePerformanceComponentContext(ctx)
		defer cancelTables()
		var err error
		tables, err = p.databasePerformanceTables(tablesCtx)
		return err
	})
	group.Go(func() error {
		statementsCtx, cancelStatements := databasePerformanceStatementsContext(ctx)
		defer cancelStatements()
		statementsCapability, topSQL = p.databasePerformanceStatements(statementsCtx)
		return nil
	})
	group.Go(func() error {
		ioCtx, cancelIO := databasePerformanceComponentContext(ctx)
		defer cancelIO()
		ioCapability, ioStats = p.databasePerformanceIO(ioCtx)
		return nil
	})
	group.Go(func() error {
		ioTimingCtx, cancelIOTiming := databasePerformanceComponentContext(ctx)
		defer cancelIOTiming()
		ioTimingCapability = p.databasePerformanceIOTiming(ioTimingCtx)
		return nil
	})
	if err := group.Wait(); err != nil {
		return management.DatabasePerformance{}, err
	}

	poolStat := p.pool.Stat()
	pool := management.DatabasePoolStats{
		Total:        int64(poolStat.TotalConns()),
		Acquired:     int64(poolStat.AcquiredConns()),
		Idle:         int64(poolStat.IdleConns()),
		Constructing: int64(poolStat.ConstructingConns()),
		Max:          int64(poolStat.MaxConns()),
	}
	maxConnections := pool.Max
	if maxConnections > 0 {
		pool.UsageRate = float64(pool.Acquired) / float64(maxConnections)
	}

	capabilities := management.DatabaseCapabilities{
		PGStatStatements: statementsCapability,
		PGStatIO:         ioCapability,
		TrackIOTiming:    ioTimingCapability,
	}
	health := databasePerformanceHealth(pool, activity, tables)

	return management.DatabasePerformance{
		AsOf: now.Unix(), Health: health, Pool: pool, Database: database,
		Activity: activity, Tables: tables, IO: ioStats, Capabilities: capabilities, TopSQL: topSQL,
	}, nil
}

func (p *Postgres) databasePerformanceDatabase(ctx context.Context) (management.DatabaseStats, error) {
	var name string
	var connections, commits, rollbacks, blocksRead, blocksHit, tempFiles, tempBytes, deadlocks int64
	var blockReadMS, blockWriteMS float64
	var statsReset *time.Time
	err := p.pool.QueryRow(ctx, `SELECT datname,numbackends,xact_commit,xact_rollback,blks_read,blks_hit,
		temp_files,temp_bytes,deadlocks,blk_read_time,blk_write_time,stats_reset
		FROM pg_stat_database WHERE datname=current_database()`).Scan(
		&name, &connections, &commits, &rollbacks, &blocksRead, &blocksHit,
		&tempFiles, &tempBytes, &deadlocks, &blockReadMS, &blockWriteMS, &statsReset,
	)
	if err != nil {
		return management.DatabaseStats{}, err
	}
	resetAt := int64(0)
	if statsReset != nil {
		resetAt = statsReset.UTC().Unix()
	}
	cacheDenominator := blocksRead + blocksHit
	cacheHitRate := float64(0)
	if cacheDenominator > 0 {
		cacheHitRate = float64(blocksHit) / float64(cacheDenominator)
	}
	return management.DatabaseStats{
		Name: name, Connections: connections, Commits: commits, Rollbacks: rollbacks,
		BlocksRead: blocksRead, BlocksHit: blocksHit, CacheHitRate: cacheHitRate,
		TempFiles: tempFiles, TempBytes: tempBytes, Deadlocks: deadlocks,
		BlockReadMs: blockReadMS, BlockWriteMs: blockWriteMS, StatsResetAt: resetAt,
	}, nil
}

func (p *Postgres) databasePerformanceActivity(ctx context.Context) (management.DatabaseActivity, error) {
	var active, idle, idleInTransaction, lockWaits int64
	var maxLockWaitSeconds float64
	err := p.pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE state='active'),
		count(*) FILTER (WHERE state='idle'),
		count(*) FILTER (WHERE state='idle in transaction'),
		count(*) FILTER (WHERE wait_event_type='Lock'),
		COALESCE(max(extract(epoch FROM clock_timestamp()-query_start)) FILTER (WHERE wait_event_type='Lock'), 0)
		FROM pg_stat_activity WHERE datname=current_database()`).Scan(
		&active, &idle, &idleInTransaction, &lockWaits, &maxLockWaitSeconds,
	)
	if err != nil {
		return management.DatabaseActivity{}, err
	}
	var ungrantedLocks int64
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE NOT granted`).Scan(&ungrantedLocks); err != nil {
		return management.DatabaseActivity{}, err
	}
	return management.DatabaseActivity{
		Active: active, Idle: idle, IdleInTransaction: idleInTransaction,
		LockWaits: lockWaits, UngrantedLocks: ungrantedLocks, MaxLockWaitSeconds: maxLockWaitSeconds,
	}, nil
}

func (p *Postgres) databasePerformanceTables(ctx context.Context) ([]management.DatabaseTableStats, error) {
	rows, err := p.pool.Query(ctx, `SELECT relname,n_live_tup,n_dead_tup,last_autovacuum,last_autoanalyze,
		COALESCE(pg_total_relation_size(relid), 0)
		FROM pg_stat_user_tables
		ORDER BY CASE WHEN n_live_tup+n_dead_tup > 0 THEN n_dead_tup::double precision/(n_live_tup+n_dead_tup) ELSE 0 END DESC,
		n_dead_tup DESC, relname
		LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]management.DatabaseTableStats, 0)
	for rows.Next() {
		var name string
		var liveTuples, deadTuples, sizeBytes int64
		var lastAutovacuum, lastAutoanalyze *time.Time
		if err := rows.Scan(&name, &liveTuples, &deadTuples, &lastAutovacuum, &lastAutoanalyze, &sizeBytes); err != nil {
			return nil, err
		}
		totalTuples := liveTuples + deadTuples
		deadRatio := float64(0)
		if totalTuples > 0 {
			deadRatio = float64(deadTuples) / float64(totalTuples)
		}
		result = append(result, management.DatabaseTableStats{
			Name: name, LiveTuples: liveTuples, DeadTuples: deadTuples, DeadTupleRatio: deadRatio,
			SizeBytes: sizeBytes, LastAutovacuumAt: unixTime(lastAutovacuum), LastAutoanalyzeAt: unixTime(lastAutoanalyze),
		})
	}
	return result, rows.Err()
}

func (p *Postgres) databasePerformanceStatements(ctx context.Context) (management.DatabaseCapability, []management.DatabaseTopSQL) {
	capability := management.DatabaseCapability{
		Enabled: false, Installed: false, Queryable: false,
		Reason: "pg_stat_statements extension is not installed",
	}
	var installed bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='pg_stat_statements')`).Scan(&installed); err != nil {
		capability.Reason = databaseCapabilityReason(err, "unable to inspect pg_stat_statements")
		return capability, nil
	}
	if !installed {
		return capability, nil
	}
	capability.Installed = true
	rows, err := p.pool.Query(ctx, `SELECT queryid::text,query,calls,total_plan_time,total_exec_time,
		mean_plan_time,mean_exec_time,shared_blks_read,shared_blks_hit,temp_blks_read,temp_blks_written
		FROM pg_stat_statements
		ORDER BY total_exec_time DESC, queryid
		LIMIT 20`)
	if err != nil {
		capability.Reason = databaseCapabilityReason(err, "Top SQL query is unavailable")
		return capability, nil
	}
	defer rows.Close()
	result := make([]management.DatabaseTopSQL, 0)
	for rows.Next() {
		var queryID pgtype.Text
		var query string
		var calls, sharedRead, sharedHit, tempRead, tempWritten int64
		var totalPlan, totalExec, meanPlan, meanExec float64
		if err := rows.Scan(&queryID, &query, &calls, &totalPlan, &totalExec, &meanPlan, &meanExec, &sharedRead, &sharedHit, &tempRead, &tempWritten); err != nil {
			capability.Reason = databaseCapabilityReason(err, "Top SQL result could not be read")
			return capability, nil
		}
		result = append(result, management.DatabaseTopSQL{
			QueryID: databaseTopSQLQueryID(queryID), Query: query, Calls: calls, TotalPlanMs: totalPlan, TotalExecMs: totalExec,
			MeanPlanMs: meanPlan, MeanExecMs: meanExec, SharedBlocksRead: sharedRead, SharedBlocksHit: sharedHit,
			TempBlocksRead: tempRead, TempBlocksWritten: tempWritten,
		})
	}
	if err := rows.Err(); err != nil {
		capability.Reason = databaseCapabilityReason(err, "Top SQL result stream failed")
		return capability, nil
	}
	capability.Enabled, capability.Queryable, capability.Reason = true, true, ""
	return capability, result
}

func databaseTopSQLQueryID(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func (p *Postgres) databasePerformanceIO(ctx context.Context) (management.DatabaseCapability, management.DatabaseIO) {
	capability := management.DatabaseCapability{Enabled: false, Reason: "pg_stat_io is unavailable"}
	var reads, writes, extends, fsyncs, evictions, reuses int64
	var readMS, writeMS float64
	err := p.pool.QueryRow(ctx, `SELECT COALESCE(sum(reads),0),COALESCE(sum(writes),0),COALESCE(sum(extends),0),
		COALESCE(sum(fsyncs),0),COALESCE(sum(evictions),0),COALESCE(sum(reuses),0),
		COALESCE(sum(read_time),0),COALESCE(sum(write_time),0)
		FROM pg_stat_io WHERE backend_type='client backend'`).Scan(
		&reads, &writes, &extends, &fsyncs, &evictions, &reuses, &readMS, &writeMS,
	)
	if err != nil {
		capability.Reason = databaseIOCapabilityReason(err)
		return capability, management.DatabaseIO{Available: false}
	}
	capability.Enabled, capability.Installed, capability.Queryable, capability.Reason = true, true, true, ""
	return capability, management.DatabaseIO{
		Available: true, Reads: int64Pointer(reads), Writes: int64Pointer(writes), Extends: int64Pointer(extends),
		Fsyncs: int64Pointer(fsyncs), Evictions: int64Pointer(evictions), Reuses: int64Pointer(reuses),
		ReadMs: float64Pointer(readMS), WriteMs: float64Pointer(writeMS),
	}
}

func (p *Postgres) databasePerformanceIOTiming(ctx context.Context) management.DatabaseCapability {
	var value string
	if err := p.pool.QueryRow(ctx, "SHOW track_io_timing").Scan(&value); err != nil {
		return management.DatabaseCapability{Reason: databaseCapabilityReason(err, "unable to inspect track_io_timing")}
	}
	if strings.EqualFold(value, "on") {
		return management.DatabaseCapability{Enabled: true, Installed: true, Queryable: true}
	}
	return management.DatabaseCapability{Installed: true, Queryable: true, Reason: "track_io_timing is disabled"}
}

func databasePerformanceHealth(pool management.DatabasePoolStats, activity management.DatabaseActivity, tables []management.DatabaseTableStats) management.DatabaseHealth {
	usage := pool.UsageRate
	lockWait := activity.MaxLockWaitSeconds
	warnings := make([]string, 0)
	critical := make([]string, 0)
	switch {
	case usage >= .95:
		critical = append(critical, "连接池使用率达到 95%")
	case usage >= .80:
		warnings = append(warnings, "连接池使用率达到 80%")
	}
	switch {
	case lockWait >= 30:
		critical = append(critical, "锁等待持续超过 30 秒")
	case activity.LockWaits > 0 || activity.UngrantedLocks > 0:
		warnings = append(warnings, "存在锁等待")
	}
	if activity.IdleInTransaction > 0 {
		warnings = append(warnings, "存在 idle in transaction 会话")
	}
	for _, table := range tables {
		if table.LiveTuples+table.DeadTuples >= 1000 && table.DeadTupleRatio >= .20 {
			warnings = append(warnings, fmt.Sprintf("%s 死元组比例达到 20%%", table.Name))
		}
	}
	level := "healthy"
	reasons := warnings
	if len(critical) > 0 {
		level, reasons = "critical", critical
	} else if len(warnings) > 0 {
		level = "warning"
	}
	return management.DatabaseHealth{Level: level, Reasons: reasons}
}

func int64Pointer(value int64) *int64 { return &value }

func float64Pointer(value float64) *float64 { return &value }

func databaseCapabilityReason(err error, fallback string) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "database performance query timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "database performance query canceled"
	}
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) {
		return fallback
	}
	switch pgError.Code {
	case "42P01":
		return "extension is not installed"
	case "42501":
		return "permission denied"
	case "55000":
		return "extension is not loaded through shared_preload_libraries"
	case "57014":
		return "database performance query timed out"
	default:
		return fallback
	}
}

func databasePerformanceComponentContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, databasePerformanceComponentLimit)
}

func databasePerformanceStatementsContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, databasePerformanceStatementsLimit)
}

func databaseIOCapabilityReason(err error) string {
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) {
		return "pg_stat_io is unavailable"
	}
	switch pgError.Code {
	case "42P01":
		return "PostgreSQL 15 does not provide pg_stat_io"
	case "42501":
		return "permission denied"
	default:
		return "pg_stat_io is unavailable"
	}
}

func unixTime(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return value.UTC().Unix()
}
