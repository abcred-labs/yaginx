package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"yaginx-gateway/internal/telemetry"
)

const (
	trafficCutoverSetting  = "traffic.cutover_ts"
	trafficBackfillSetting = "traffic.legacy_monitor_backfill_complete"
)

// The report_cycle column encoding. Written here, read back by the analytics
// queries in analytics.go.
const (
	reportCycleMinutely = 5
	reportCycleHourly   = 10
	reportCycleDaily    = 20
)

var ErrTrafficMaterializerBusy = errors.New("traffic materializer busy")

type trafficCutoverPayload struct {
	UnixMilli int64 `json:"unixMilli"`
}

// GetOrCreateTrafficCutover atomically freezes the boundary between legacy
// raw monitor rows and Go contributions.
func (p *Postgres) GetOrCreateTrafficCutover(ctx context.Context, now time.Time) (time.Time, error) {
	now = now.UTC()
	plain, err := json.Marshal(trafficCutoverPayload{UnixMilli: now.UnixMilli()})
	if err != nil {
		return time.Time{}, err
	}
	var payload []byte
	err = p.pool.QueryRow(ctx, "INSERT INTO gateway_system_setting(setting_key,value,content_type,updated_at) VALUES($1,$2::jsonb,'application/json',clock_timestamp()) ON CONFLICT(setting_key) DO NOTHING RETURNING value::text", trafficCutoverSetting, string(plain)).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		err = p.pool.QueryRow(ctx, "SELECT value::text FROM gateway_system_setting WHERE setting_key=$1", trafficCutoverSetting).Scan(&payload)
	}
	if err != nil {
		return time.Time{}, err
	}
	var value trafficCutoverPayload
	if err := json.Unmarshal(payload, &value); err != nil || value.UnixMilli <= 0 {
		if err == nil {
			err = fmt.Errorf("missing unixMilli")
		}
		return time.Time{}, fmt.Errorf("decode traffic cutover: %w", err)
	}
	return time.UnixMilli(value.UnixMilli).UTC(), nil
}

// BackfillLegacyMonitorReports applies the .NET monitor report grouping to
// raw rows strictly before cutover. PostgreSQL expands JSONB and aggregates in
// place, avoiding an unbounded Go heap during a long-lived deployment. Go v1
// does not create new raw monitor rows; afterwards normal contribution jobs
// own all periods at or after cutover.
func (p *Postgres) BackfillLegacyMonitorReports(ctx context.Context, cutover time.Time) error {
	if cutover.IsZero() {
		return fmt.Errorf("traffic cutover is required")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('yaginx:legacy-monitor-backfill',0))"); err != nil {
		return err
	}
	complete, err := p.legacyMonitorBackfillComplete(ctx, tx, cutover)
	if err != nil {
		return err
	}
	if complete {
		return tx.Commit(ctx)
	}
	for _, cycle := range []struct {
		kind  string
		value int
	}{{"minute", 5}, {"hour", 10}} {
		_, err := tx.Exec(ctx, legacyMonitorReportSQL, cycle.kind, cycle.value, cutover.Unix())
		if err != nil {
			return fmt.Errorf("backfill legacy %s report: %w", cycle.kind, err)
		}
	}
	if err := p.markLegacyMonitorBackfillComplete(ctx, tx, cutover); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) legacyMonitorBackfillComplete(ctx context.Context, tx pgx.Tx, cutover time.Time) (bool, error) {
	var payload []byte
	err := tx.QueryRow(ctx, "SELECT value::text FROM gateway_system_setting WHERE setting_key=$1 FOR UPDATE", trafficBackfillSetting).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var marker trafficCutoverPayload
	if err := json.Unmarshal(payload, &marker); err != nil {
		return false, fmt.Errorf("decode legacy monitor backfill marker: %w", err)
	}
	if marker.UnixMilli != cutover.UnixMilli() {
		return false, fmt.Errorf("legacy monitor backfill marker cutover mismatch")
	}
	return true, nil
}

func (p *Postgres) markLegacyMonitorBackfillComplete(ctx context.Context, tx pgx.Tx, cutover time.Time) error {
	plain, err := json.Marshal(trafficCutoverPayload{UnixMilli: cutover.UnixMilli()})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO gateway_system_setting(setting_key,value,content_type,updated_at) VALUES($1,$2::jsonb,'application/json',clock_timestamp())", trafficBackfillSetting, string(plain))
	return err
}

const legacyMonitorReportSQL = `
WITH raw AS (
    SELECT date_trunc($1, to_timestamp(m.create_ts)) AS report_time,
           NULLIF(COALESCE(item->>'host', item->>'Host'), '') AS host_name,
           CASE WHEN COALESCE(item->>'statusCode', item->>'StatusCode', '') ~ '^-?[0-9]+$'
                THEN COALESCE(item->>'statusCode', item->>'StatusCode')::integer ELSE 0 END AS status_code,
           CASE WHEN COALESCE(item->>'duration', item->>'Duration', '') ~ '^-?[0-9]+$'
                THEN COALESCE(item->>'duration', item->>'Duration')::bigint ELSE 0 END AS duration_ms,
           COALESCE(item#>>'{ua,family}', item#>>'{Ua,Family}', '') AS ua_family,
           CASE WHEN lower(COALESCE(item#>>'{device,isSpider}', item#>>'{Device,IsSpider}', 'false')) = 'true'
                THEN true ELSE false END AS is_spider,
           COALESCE(item#>>'{os,family}', item#>>'{Os,Family}', '') AS os_family,
           COALESCE(item#>>'{os,major}', item#>>'{Os,Major}', '') AS os_major
      FROM resource_monitor_info m
      CROSS JOIN LATERAL jsonb_array_elements(COALESCE(m.data, '[]'::jsonb)) item
     WHERE m.create_ts < $3
),
totals AS (
    SELECT host_name, report_time, count(*)::bigint AS request_qty,
           COALESCE(avg(duration_ms)::bigint, 0) AS duration_avg,
           COALESCE(max(duration_ms), 0) AS duration_max,
           count(*) FILTER (WHERE duration_ms <= 100)::bigint AS duration_100,
           count(*) FILTER (WHERE duration_ms > 100 AND duration_ms <= 500)::bigint AS duration_500,
           count(*) FILTER (WHERE duration_ms > 500 AND duration_ms <= 1000)::bigint AS duration_1000,
           count(*) FILTER (WHERE duration_ms > 1000)::bigint AS duration_over_1000
      FROM raw WHERE host_name IS NOT NULL
     GROUP BY host_name, report_time
),
status_values AS (
    SELECT host_name, report_time, status_code::text AS status_code, count(*)::bigint AS quantity
      FROM raw WHERE host_name IS NOT NULL
     GROUP BY host_name, report_time, status_code
),
statuses AS (
    SELECT host_name, report_time, jsonb_object_agg(status_code, quantity) AS status_code
      FROM status_values GROUP BY host_name, report_time
),
spider_values AS (
    SELECT host_name, report_time, ua_family, count(*)::bigint AS quantity
      FROM raw WHERE host_name IS NOT NULL AND is_spider AND ua_family <> ''
     GROUP BY host_name, report_time, ua_family
),
spiders AS (
    SELECT host_name, report_time, jsonb_object_agg(ua_family, quantity) || jsonb_build_object('total', sum(quantity)) AS spider
      FROM spider_values GROUP BY host_name, report_time
),
browser_values AS (
    SELECT host_name, report_time, ua_family, count(*)::bigint AS quantity
      FROM raw WHERE host_name IS NOT NULL AND NOT is_spider AND ua_family <> ''
     GROUP BY host_name, report_time, ua_family
),
browsers AS (
    SELECT host_name, report_time, jsonb_object_agg(ua_family, quantity) || jsonb_build_object('total', sum(quantity)) AS browser
      FROM browser_values GROUP BY host_name, report_time
),
os_values AS (
    SELECT host_name, report_time, os_family || '-' || os_major AS os_name, count(*)::bigint AS quantity
      FROM raw WHERE host_name IS NOT NULL AND os_family <> ''
     GROUP BY host_name, report_time, os_family, os_major
),
oses AS (
    SELECT host_name, report_time, jsonb_object_agg(os_name, quantity) AS os
      FROM os_values GROUP BY host_name, report_time
)
INSERT INTO resource_report(resource_uuid,cycle_type,report_time,report_ts,request_qty,status_code,spider,browser,os,duration,create_time,update_time)
SELECT t.host_name,$2,t.report_time,extract(epoch FROM t.report_time)::bigint,t.request_qty,
       COALESCE(s.status_code,'{}'::jsonb),COALESCE(sp.spider,'{}'::jsonb),COALESCE(b.browser,'{}'::jsonb),COALESCE(o.os,'{}'::jsonb),
       jsonb_build_object('avg',t.duration_avg,'max',t.duration_max,'<=100',t.duration_100,'>100 & <=500',t.duration_500,'>500 & <=1000',t.duration_1000,'>1000',t.duration_over_1000),
       clock_timestamp(),clock_timestamp()
  FROM totals t LEFT JOIN statuses s USING(host_name,report_time)
       LEFT JOIN spiders sp USING(host_name,report_time)
       LEFT JOIN browsers b USING(host_name,report_time)
       LEFT JOIN oses o USING(host_name,report_time)
ON CONFLICT(resource_uuid,cycle_type,report_ts) DO UPDATE
    SET request_qty=excluded.request_qty,status_code=excluded.status_code,duration=excluded.duration,update_time=clock_timestamp()`

func (p *Postgres) SaveTrafficContributions(ctx context.Context, values []telemetry.Contribution) error {
	if len(values) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	batch := &pgx.Batch{}
	const upsert = "INSERT INTO gateway_traffic_contribution(node_id,process_id,host_name,cycle_type,period,request_qty,duration_sum,duration_max,inbound_bytes,outbound_bytes,status_code,spider,browser,os,duration_buckets,error_counts,latency_histogram,response_mode_metrics,is_final,updated_at) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12::jsonb,$13::jsonb,$14::jsonb,$15::jsonb,$16::jsonb,$17::jsonb,$18::jsonb,$19,clock_timestamp()) ON CONFLICT(node_id,process_id,host_name,cycle_type,period) DO UPDATE SET request_qty=excluded.request_qty,duration_sum=excluded.duration_sum,duration_max=excluded.duration_max,inbound_bytes=excluded.inbound_bytes,outbound_bytes=excluded.outbound_bytes,status_code=excluded.status_code,spider=excluded.spider,browser=excluded.browser,os=excluded.os,duration_buckets=excluded.duration_buckets,error_counts=excluded.error_counts,latency_histogram=excluded.latency_histogram,response_mode_metrics=excluded.response_mode_metrics,is_final=gateway_traffic_contribution.is_final OR excluded.is_final,updated_at=clock_timestamp()"
	for _, value := range values {
		status, err := marshalCounts(value.StatusCounts)
		if err != nil {
			return err
		}
		spider, err := marshalCounts(value.SpiderCounts)
		if err != nil {
			return err
		}
		browser, err := marshalCounts(value.BrowserCounts)
		if err != nil {
			return err
		}
		osValues, err := marshalCounts(value.OSCounts)
		if err != nil {
			return err
		}
		durationBuckets, err := marshalCounts(value.DurationBuckets)
		if err != nil {
			return err
		}
		errors, err := marshalCounts(value.ErrorCounts)
		if err != nil {
			return err
		}
		histogram, err := marshalCounts(value.LatencyHistogram)
		if err != nil {
			return err
		}
		modeMetrics, err := json.Marshal(value.ResponseModeMetrics)
		if err != nil {
			return err
		}
		batch.Queue(upsert, value.NodeID, value.ProcessID, value.Host, value.Cycle, value.Period, value.Requests, value.DurationSum, value.DurationMax, value.Inbound, value.Outbound, status, spider, browser, osValues, durationBuckets, errors, histogram, modeMetrics, value.IsFinal)
	}
	results := tx.SendBatch(ctx, batch)
	for range values {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return err
		}
	}
	if err := results.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func marshalCounts(values map[string]int64) ([]byte, error) {
	if len(values) == 0 {
		return []byte(`{}`), nil
	}
	return json.Marshal(values)
}

// RetentionStats describes one telemetry retention pass.
type RetentionStats struct {
	Deleted                 map[string]int64
	BlockedContributionRows int64
}

// PruneTelemetry deletes bounded batches so retention never turns into an
// unbounded transaction. Contributions are deleted only after their complete
// report projections exist, so a delayed materializer never creates a gap in
// the summarized data.
func (p *Postgres) PruneTelemetry(ctx context.Context, now time.Time, contributionRetention, monitorInfoRetention time.Duration, trafficDays, reportDays, historyHours, batchSize int) error {
	_, err := p.PruneTelemetryWithStats(ctx, now, contributionRetention, monitorInfoRetention, trafficDays, reportDays, historyHours, batchSize)
	return err
}

// PruneTelemetryWithStats is PruneTelemetry with deletion and blocked-row
// counts for retention observability.
func (p *Postgres) PruneTelemetryWithStats(ctx context.Context, now time.Time, contributionRetention, monitorInfoRetention time.Duration, trafficDays, reportDays, historyHours, batchSize int) (RetentionStats, error) {
	stats := RetentionStats{Deleted: map[string]int64{}}
	if contributionRetention <= 0 {
		contributionRetention = 7 * 24 * time.Hour
	}
	if monitorInfoRetention <= 0 {
		monitorInfoRetention = 48 * time.Hour
	}
	if trafficDays < 1 {
		trafficDays = 90
	}
	if reportDays < trafficDays {
		reportDays = trafficDays
	}
	if historyHours < 1 {
		historyHours = 48
	}
	if batchSize < 1 {
		batchSize = 5000
	}
	contributionCutoff := now.Add(-contributionRetention).Unix()
	for _, task := range []struct {
		table, predicate string
		arg              any
	}{
		{"gateway_traffic_contribution", "period < $1 AND NOT EXISTS (SELECT 1 FROM gateway_traffic_contribution pending WHERE pending.host_name=gateway_traffic_contribution.host_name AND pending.cycle_type=gateway_traffic_contribution.cycle_type AND pending.period=gateway_traffic_contribution.period AND NOT pending.is_final) AND EXISTS (SELECT 1 FROM resource_report report WHERE report.resource_uuid=gateway_traffic_contribution.host_name AND report.cycle_type=gateway_traffic_contribution.cycle_type AND report.report_ts=gateway_traffic_contribution.period AND NOT report.is_partial) AND (gateway_traffic_contribution.cycle_type <> 10 OR EXISTS (SELECT 1 FROM host_traffic traffic WHERE traffic.host_name=gateway_traffic_contribution.host_name AND traffic.period=gateway_traffic_contribution.period))", contributionCutoff},
		{"resource_monitor_info", "create_ts < $1 AND EXISTS (SELECT 1 FROM gateway_system_setting done JOIN gateway_system_setting cutover ON cutover.setting_key='traffic.cutover_ts' WHERE done.setting_key='traffic.legacy_monitor_backfill_complete' AND done.value->>'unixMilli'=cutover.value->>'unixMilli')", now.Add(-monitorInfoRetention).Unix()},
		{"request_history", "request_ts < $1", now.Add(-time.Duration(historyHours) * time.Hour).UnixMilli()},
		{"host_traffic", "period < $1", now.AddDate(0, 0, -trafficDays).UTC().Truncate(time.Hour).Unix()},
		{"resource_report", "report_ts < $1", now.AddDate(0, 0, -reportDays).UTC().Truncate(24 * time.Hour).Unix()},
	} {
		for {
			query := fmt.Sprintf("DELETE FROM %s WHERE ctid IN (SELECT ctid FROM %s WHERE %s LIMIT $2)", task.table, task.table, task.predicate)
			tag, err := p.pool.Exec(ctx, query, task.arg, batchSize)
			if err != nil {
				return stats, fmt.Errorf("prune %s: %w", task.table, err)
			}
			stats.Deleted[task.table] += tag.RowsAffected()
			if tag.RowsAffected() < int64(batchSize) {
				break
			}
		}
	}
	if err := p.vacuumRetentionTables(ctx, stats.Deleted); err != nil {
		return stats, err
	}
	if err := p.pool.QueryRow(ctx, `SELECT count(*)
		FROM gateway_traffic_contribution c
		WHERE c.period < $1
		  AND (EXISTS (
				SELECT 1 FROM gateway_traffic_contribution pending
				WHERE pending.host_name=c.host_name AND pending.cycle_type=c.cycle_type AND pending.period=c.period AND NOT pending.is_final
			) OR NOT EXISTS (
				SELECT 1 FROM resource_report report
				WHERE report.resource_uuid=c.host_name AND report.cycle_type=c.cycle_type AND report.report_ts=c.period AND NOT report.is_partial
			) OR (c.cycle_type=10 AND NOT EXISTS (
				SELECT 1 FROM host_traffic traffic WHERE traffic.host_name=c.host_name AND traffic.period=c.period
			)))`, contributionCutoff).Scan(&stats.BlockedContributionRows); err != nil {
		return stats, fmt.Errorf("count blocked contribution retention rows: %w", err)
	}
	return stats, nil
}

// vacuumRetentionTables refreshes planner statistics and makes deleted pages
// reusable without requiring a long-lived VACUUM FULL lock. The table names
// are fixed so the maintenance query cannot be influenced by configuration.
func (p *Postgres) vacuumRetentionTables(ctx context.Context, deleted map[string]int64) error {
	for _, table := range []string{
		"gateway_traffic_contribution", "resource_monitor_info", "request_history", "host_traffic", "resource_report",
	} {
		if deleted[table] <= 0 {
			continue
		}
		conn, err := p.pool.Acquire(ctx)
		if err != nil {
			return fmt.Errorf("acquire connection to vacuum %s after retention: %w", table, err)
		}
		var previousTimeout string
		if err := conn.QueryRow(ctx, "SHOW statement_timeout").Scan(&previousTimeout); err != nil {
			conn.Release()
			return fmt.Errorf("read statement timeout before vacuum %s: %w", table, err)
		}
		if _, err := conn.Exec(ctx, "SELECT set_config('statement_timeout','0',false)"); err != nil {
			conn.Release()
			return fmt.Errorf("disable statement timeout for vacuum %s: %w", table, err)
		}
		_, vacuumErr := conn.Exec(ctx, "VACUUM (ANALYZE) "+table)
		resetCtx := context.Background()
		_, resetErr := conn.Exec(resetCtx, "SELECT set_config('statement_timeout',$1,false)", previousTimeout)
		if resetErr != nil {
			_ = conn.Conn().Close(resetCtx)
			conn.Release()
			if vacuumErr != nil {
				return fmt.Errorf("vacuum %s after retention: %w (also failed to restore statement timeout: %v)", table, vacuumErr, resetErr)
			}
			return fmt.Errorf("restore statement timeout after vacuum %s: %w", table, resetErr)
		}
		conn.Release()
		if vacuumErr != nil {
			return fmt.Errorf("vacuum %s after retention: %w", table, vacuumErr)
		}
	}
	return nil
}

// ResetTrafficStatistics clears only derived and raw traffic statistics for a
// deliberate zero-based rebuild. Request history is intentionally excluded.
func (p *Postgres) ResetTrafficStatistics(ctx context.Context) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := p.lockTrafficMaterializer(ctx, tx); err != nil {
		return err
	}
	for _, table := range []string{"resource_report", "host_traffic", "gateway_traffic_contribution", "resource_monitor_info"} {
		if _, err := tx.Exec(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("reset traffic statistics %s: %w", table, err)
		}
	}
	return tx.Commit(ctx)
}

// TrafficStatisticsHealth compares Go's raw contribution source with its
// compatibility projections. UA completeness is meaningful only when parsing
// was enabled for the contribution writer.
func (p *Postgres) TrafficStatisticsHealth(ctx context.Context, begin, end time.Time, uaParsingEnabled bool) (map[string]int64, error) {
	beginUnix, endUnix, err := reportMaterializationBounds(begin, end)
	if err != nil {
		return nil, err
	}
	health := map[string]int64{
		"contribution_report_mismatch": 0, "missing_report_bucket": 0, "status_nonconservation": 0,
		"latency_incomplete": 0, "ua_incomplete": 0, "materialization_lag_seconds": 0,
	}
	where := "($1::bigint IS NULL OR period >= $1) AND ($2::bigint IS NULL OR period < $2)"
	var mismatch, missingReports int64
	if err := p.pool.QueryRow(ctx, `WITH source AS (
		SELECT host_name,cycle_type,period,sum(request_qty) AS requests,sum(inbound_bytes) AS inbound,sum(outbound_bytes) AS outbound
		FROM gateway_traffic_contribution WHERE cycle_type IN (5,10) AND `+where+`
		GROUP BY host_name,cycle_type,period
	), reports AS (
		SELECT resource_uuid,cycle_type,report_ts,request_qty,inbound_bytes,outbound_bytes
		FROM resource_report WHERE cycle_type IN (5,10)
			AND ($1::bigint IS NULL OR report_ts >= $1) AND ($2::bigint IS NULL OR report_ts < $2)
	), comparison AS (
		SELECT COALESCE(s.host_name,r.resource_uuid) AS host_name,COALESCE(s.cycle_type,r.cycle_type) AS cycle_type,
			COALESCE(s.period,r.report_ts) AS period,s.requests,s.inbound,s.outbound,r.request_qty,r.inbound_bytes,r.outbound_bytes
		FROM source s FULL OUTER JOIN reports r
			ON r.resource_uuid=s.host_name AND r.cycle_type=s.cycle_type AND r.report_ts=s.period
	)
	SELECT count(*) FILTER (WHERE requests IS DISTINCT FROM request_qty OR inbound IS DISTINCT FROM inbound_bytes OR outbound IS DISTINCT FROM outbound_bytes),
		count(*) FILTER (WHERE requests IS NOT NULL AND request_qty IS NULL)
	FROM comparison`, beginUnix, endUnix).Scan(&mismatch, &missingReports); err != nil {
		return nil, fmt.Errorf("traffic contribution/report reconciliation: %w", err)
	}
	health["contribution_report_mismatch"] = mismatch
	health["missing_report_bucket"] = missingReports
	var statusNonconservation, latencyIncomplete int64
	if err := p.pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE request_qty <> COALESCE((SELECT sum((value)::bigint) FROM jsonb_each_text(COALESCE(status_code,'{}'::jsonb))),0)),
		count(*) FILTER (WHERE request_qty <> COALESCE((SELECT sum((value)::bigint) FROM jsonb_each_text(COALESCE(latency_histogram,'{}'::jsonb))),0))
		FROM resource_report WHERE cycle_type IN (5,10) AND ($1::bigint IS NULL OR report_ts >= $1) AND ($2::bigint IS NULL OR report_ts < $2)`,
		beginUnix, endUnix).Scan(&statusNonconservation, &latencyIncomplete); err != nil {
		return nil, fmt.Errorf("traffic report completeness: %w", err)
	}
	health["status_nonconservation"] = statusNonconservation
	health["latency_incomplete"] = latencyIncomplete
	if uaParsingEnabled {
		var uaIncomplete int64
		if err := p.pool.QueryRow(ctx, `SELECT count(*) FILTER (
			WHERE request_qty <> COALESCE((SELECT sum((value)::bigint) FROM jsonb_each_text(COALESCE(browser,'{}'::jsonb))),0)
				+ COALESCE((SELECT sum((value)::bigint) FROM jsonb_each_text(COALESCE(spider,'{}'::jsonb))),0)
			   OR request_qty <> COALESCE((SELECT sum((value)::bigint) FROM jsonb_each_text(COALESCE(os,'{}'::jsonb))),0)
		) FROM resource_report
		WHERE cycle_type IN (5,10) AND ($1::bigint IS NULL OR report_ts >= $1) AND ($2::bigint IS NULL OR report_ts < $2)`,
			beginUnix, endUnix).Scan(&uaIncomplete); err != nil {
			return nil, fmt.Errorf("traffic report user-agent completeness: %w", err)
		}
		health["ua_incomplete"] = uaIncomplete
	}
	var latest *int64
	if err := p.pool.QueryRow(ctx, `SELECT max(period) FROM gateway_traffic_contribution WHERE cycle_type=5`).Scan(&latest); err != nil {
		return nil, fmt.Errorf("traffic contribution lag: %w", err)
	}
	if latest != nil {
		health["materialization_lag_seconds"] = max(time.Now().UTC().Unix()-*latest, 0)
	}
	return health, nil
}

// PruneReportCycles applies the shorter minute/hour windows without reducing
// daily reports. It uses bounded deletes for installations upgraded before
// time partitioning is available.
func (p *Postgres) PruneReportCycles(ctx context.Context, now time.Time, minuteDays, hourlyDays, batchSize int) error {
	_, err := p.PruneReportCyclesWithStats(ctx, now, minuteDays, hourlyDays, batchSize)
	return err
}

// PruneReportCyclesWithStats is PruneReportCycles with per-table deletion
// counts for retention observability.
func (p *Postgres) PruneReportCyclesWithStats(ctx context.Context, now time.Time, minuteDays, hourlyDays, batchSize int) (map[string]int64, error) {
	deleted := map[string]int64{}
	if minuteDays < 1 {
		minuteDays = 30
	}
	if hourlyDays < 1 {
		hourlyDays = 365
	}
	if batchSize < 1 {
		batchSize = 5000
	}
	for _, task := range []struct {
		cycle  int
		before int64
	}{
		{reportCycleMinutely, now.AddDate(0, 0, -minuteDays).UTC().Truncate(time.Minute).Unix()},
		{reportCycleHourly, now.AddDate(0, 0, -hourlyDays).UTC().Truncate(time.Hour).Unix()},
	} {
		for {
			tag, err := p.pool.Exec(ctx, "DELETE FROM resource_report WHERE ctid IN (SELECT ctid FROM resource_report WHERE cycle_type=$1 AND report_ts < $2 LIMIT $3)", task.cycle, task.before, batchSize)
			if err != nil {
				return deleted, fmt.Errorf("prune report cycle %d: %w", task.cycle, err)
			}
			deleted["resource_report"] += tag.RowsAffected()
			if tag.RowsAffected() < int64(batchSize) {
				break
			}
		}
	}
	if err := p.vacuumRetentionTables(ctx, deleted); err != nil {
		return deleted, err
	}
	return deleted, nil
}

// MaterializeTraffic converts all process snapshots into absolute legacy
// tables. Re-running it never adds a second copy of any node contribution.
func (p *Postgres) MaterializeTraffic(ctx context.Context) error {
	return p.MaterializeTrafficWindow(ctx, time.Time{}, time.Time{})
}

// TrafficMaterializationOptions selects which report cycles to reconcile.
// Runtime callers split their frequent minute refresh from broader hourly and
// daily recovery windows, while maintenance callers retain the all-cycle path.
type TrafficMaterializationOptions struct {
	Minutely         bool
	Hourly           bool
	Daily            bool
	StatementTimeout time.Duration
}

// MaterializeTrafficWindow reconciles contribution-backed reports inside the
// supplied UTC range. A zero bound retains the legacy full-history behavior
// used by maintenance tests; runtime callers always provide bounded dates.
func (p *Postgres) MaterializeTrafficWindow(ctx context.Context, begin, end time.Time) error {
	return p.MaterializeTrafficReports(ctx, begin, end, TrafficMaterializationOptions{
		Minutely: true,
		Hourly:   true,
		Daily:    true,
	})
}

// MaterializeTrafficReports reconciles the selected report cycles. The
// transaction advisory lock is intentionally non-blocking so another gateway
// can keep serving instead of timing out while a peer owns the report job.
func (p *Postgres) MaterializeTrafficReports(ctx context.Context, begin, end time.Time, options TrafficMaterializationOptions) error {
	if !options.Minutely && !options.Hourly && !options.Daily {
		return fmt.Errorf("traffic materialization requires at least one cycle")
	}
	beginUnix, endUnix, err := reportMaterializationBounds(begin, end)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	locked, err := p.tryLockTrafficMaterializer(ctx, tx)
	if err != nil {
		return err
	}
	if !locked {
		return ErrTrafficMaterializerBusy
	}
	if options.StatementTimeout > 0 {
		if _, err := tx.Exec(ctx, "SELECT set_config('statement_timeout',$1,true)", fmt.Sprintf("%dms", options.StatementTimeout.Milliseconds())); err != nil {
			return fmt.Errorf("set traffic materialization statement timeout: %w", err)
		}
	}
	if options.Minutely {
		if err := p.materializeContributionReportsTx(ctx, tx, beginUnix, endUnix, reportCycleMinutely); err != nil {
			return err
		}
	}
	if options.Hourly {
		if _, err := tx.Exec(ctx, materializeHostTrafficSQL, beginUnix, endUnix); err != nil {
			return fmt.Errorf("materialize host traffic: %w", err)
		}
		if err := p.materializeContributionReportsTx(ctx, tx, beginUnix, endUnix, reportCycleHourly); err != nil {
			return err
		}
	}
	if options.Daily {
		if err := p.materializeDailyResourceReportsTx(ctx, tx, beginUnix, endUnix); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// BackfillDailyResourceReports materializes daily reports for a finite UTC
// range. Each caller owns its transaction, allowing CLI backfills to commit one
// date at a time and resume safely after an interruption.
func (p *Postgres) BackfillDailyResourceReports(ctx context.Context, begin, end time.Time, statementTimeout time.Duration) error {
	beginUnix, endUnix, err := reportMaterializationBounds(begin, end)
	if err != nil {
		return err
	}
	if beginUnix == nil || endUnix == nil {
		return fmt.Errorf("daily report backfill requires begin and end")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if statementTimeout > 0 {
		if _, err := tx.Exec(ctx, "SELECT set_config('statement_timeout',$1,true)", fmt.Sprintf("%dms", statementTimeout.Milliseconds())); err != nil {
			return fmt.Errorf("set daily report statement timeout: %w", err)
		}
	}
	if err := p.lockTrafficMaterializer(ctx, tx); err != nil {
		return err
	}
	if err := p.backfillDailyResourceReportsTx(ctx, tx, beginUnix, endUnix); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) lockTrafficMaterializer(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", p.trafficMaterializerLock)
	return err
}

func (p *Postgres) tryLockTrafficMaterializer(ctx context.Context, tx pgx.Tx) (bool, error) {
	var locked bool
	err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))", p.trafficMaterializerLock).Scan(&locked)
	return locked, err
}

func (p *Postgres) materializeContributionReportsTx(ctx context.Context, tx pgx.Tx, beginUnix, endUnix any, cycle int) error {
	_, err := tx.Exec(ctx, materializeContributionReportsSQL, beginUnix, endUnix, cycle)
	if err != nil {
		return fmt.Errorf("materialize resource report cycle %d: %w", cycle, err)
	}
	return nil
}

func (p *Postgres) materializeDailyResourceReportsTx(ctx context.Context, tx pgx.Tx, beginUnix, endUnix any) error {
	_, err := tx.Exec(ctx, materializeDailyResourceReportsSQL, beginUnix, endUnix)
	if err != nil {
		return fmt.Errorf("materialize daily resource report: %w", err)
	}
	return nil
}

func (p *Postgres) backfillDailyResourceReportsTx(ctx context.Context, tx pgx.Tx, beginUnix, endUnix any) error {
	_, err := tx.Exec(ctx, backfillDailyResourceReportsSQL, beginUnix, endUnix)
	if err != nil {
		return fmt.Errorf("backfill daily resource report: %w", err)
	}
	return nil
}

func reportMaterializationBounds(begin, end time.Time) (any, any, error) {
	if !begin.IsZero() && !end.IsZero() && !end.After(begin) {
		return nil, nil, fmt.Errorf("report materialization end must be after begin")
	}
	var beginUnix, endUnix any
	if !begin.IsZero() {
		beginUnix = begin.UTC().Unix()
	}
	if !end.IsZero() {
		endUnix = end.UTC().Unix()
	}
	return beginUnix, endUnix, nil
}

const materializeHostTrafficSQL = `
WITH totals AS (
    SELECT host_name, period, sum(request_qty) AS request_qty,
           sum(inbound_bytes) AS inbound_bytes, sum(outbound_bytes) AS outbound_bytes
      FROM gateway_traffic_contribution
     WHERE cycle_type = 10
       AND ($1::bigint IS NULL OR period >= $1)
       AND ($2::bigint IS NULL OR period < $2)
     GROUP BY host_name, period
)
INSERT INTO host_traffic(host_name,period,request_counts,inbound_bytes,outbound_bytes)
SELECT host_name,period,request_qty,inbound_bytes,outbound_bytes FROM totals
ON CONFLICT(host_name,period) DO UPDATE
    SET request_counts=excluded.request_counts,inbound_bytes=excluded.inbound_bytes,outbound_bytes=excluded.outbound_bytes
  WHERE (host_traffic.request_counts,host_traffic.inbound_bytes,host_traffic.outbound_bytes)
        IS DISTINCT FROM (excluded.request_counts,excluded.inbound_bytes,excluded.outbound_bytes)`

const materializeContributionReportsSQL = `
WITH contributions AS MATERIALIZED (
    SELECT host_name,cycle_type,period,request_qty,duration_sum,duration_max,
           inbound_bytes,outbound_bytes,status_code,spider,browser,os,duration_buckets,
           error_counts,latency_histogram,response_mode_metrics,is_final
	      FROM gateway_traffic_contribution
	     WHERE cycle_type = $3
	       AND ($1::bigint IS NULL OR period >= $1)
	       AND ($2::bigint IS NULL OR period < $2)
),
totals AS (
    SELECT host_name,cycle_type,period,sum(request_qty) AS request_qty,
           sum(duration_sum) AS duration_sum,max(duration_max) AS duration_max,
           sum(inbound_bytes) AS inbound_bytes,sum(outbound_bytes) AS outbound_bytes,
           bool_or(NOT is_final) AS is_partial
      FROM contributions GROUP BY host_name,cycle_type,period
),
status_values AS (
    SELECT c.host_name,c.cycle_type,c.period,e.key,sum((e.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.status_code) e
     GROUP BY c.host_name,c.cycle_type,c.period,e.key
),
spider_values AS (
    SELECT c.host_name,c.cycle_type,c.period,e.key,sum((e.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.spider) e
     GROUP BY c.host_name,c.cycle_type,c.period,e.key
),
browser_values AS (
    SELECT c.host_name,c.cycle_type,c.period,e.key,sum((e.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.browser) e
     GROUP BY c.host_name,c.cycle_type,c.period,e.key
),
os_values AS (
    SELECT c.host_name,c.cycle_type,c.period,e.key,sum((e.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.os) e
     GROUP BY c.host_name,c.cycle_type,c.period,e.key
),
bucket_values AS (
    SELECT c.host_name,c.cycle_type,c.period,e.key,sum((e.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.duration_buckets) e
     GROUP BY c.host_name,c.cycle_type,c.period,e.key
),
error_values AS (
    SELECT c.host_name,c.cycle_type,c.period,e.key,sum((e.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.error_counts) e
     GROUP BY c.host_name,c.cycle_type,c.period,e.key
),
histogram_values AS (
    SELECT c.host_name,c.cycle_type,c.period,e.key,sum((e.value)::bigint) AS count
	      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.latency_histogram) e
	     GROUP BY c.host_name,c.cycle_type,c.period,e.key
),
mode_values AS (
    SELECT c.host_name,c.cycle_type,c.period,m.key AS mode,k.key AS metric,sum((k.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each(c.response_mode_metrics) m
      CROSS JOIN LATERAL jsonb_each_text(m.value) k
     GROUP BY c.host_name,c.cycle_type,c.period,m.key,k.key
),
status_maps AS (
    SELECT host_name,cycle_type,period,jsonb_object_agg(key,count) AS value
      FROM status_values GROUP BY host_name,cycle_type,period
),
spider_maps AS (
    SELECT host_name,cycle_type,period,jsonb_object_agg(key,count) AS value
      FROM spider_values GROUP BY host_name,cycle_type,period
),
browser_maps AS (
    SELECT host_name,cycle_type,period,jsonb_object_agg(key,count) AS value
      FROM browser_values GROUP BY host_name,cycle_type,period
),
os_maps AS (
    SELECT host_name,cycle_type,period,jsonb_object_agg(key,count) AS value
      FROM os_values GROUP BY host_name,cycle_type,period
),
bucket_maps AS (
    SELECT host_name,cycle_type,period,jsonb_object_agg(key,count) AS value
      FROM bucket_values GROUP BY host_name,cycle_type,period
),
error_maps AS (
    SELECT host_name,cycle_type,period,jsonb_object_agg(key,count) AS value
      FROM error_values GROUP BY host_name,cycle_type,period
),
histogram_maps AS (
    SELECT host_name,cycle_type,period,jsonb_object_agg(key,count) AS value
      FROM histogram_values GROUP BY host_name,cycle_type,period
),
mode_metric_maps AS (
    SELECT host_name,cycle_type,period,mode,jsonb_object_agg(metric,count) AS value
      FROM mode_values GROUP BY host_name,cycle_type,period,mode
),
mode_maps AS (
    SELECT host_name,cycle_type,period,jsonb_object_agg(mode,value) AS value
      FROM mode_metric_maps GROUP BY host_name,cycle_type,period
)
INSERT INTO resource_report(resource_uuid,cycle_type,report_time,report_ts,request_qty,inbound_bytes,outbound_bytes,status_code,spider,browser,os,duration,error_counts,latency_histogram,response_mode_metrics,is_partial,create_time,update_time)
SELECT t.host_name,t.cycle_type,to_timestamp(t.period),t.period,t.request_qty,
	       t.inbound_bytes,t.outbound_bytes,
	       COALESCE(s.value,'{}'::jsonb),
	       COALESCE(sp.value,'{}'::jsonb),
	       COALESCE(b.value,'{}'::jsonb),
	       COALESCE(o.value,'{}'::jsonb),
	       COALESCE(d.value,'{}'::jsonb)
	           || jsonb_build_object('avg',CASE WHEN t.request_qty=0 THEN 0 ELSE (t.duration_sum/t.request_qty)::bigint END,'max',t.duration_max),
	       COALESCE(e.value,'{}'::jsonb),
	       COALESCE(h.value,'{}'::jsonb),
	       COALESCE(mm.value,'{}'::jsonb),
	       t.is_partial,
	       clock_timestamp(),clock_timestamp()
  FROM totals t
  LEFT JOIN status_maps s USING(host_name,cycle_type,period)
  LEFT JOIN spider_maps sp USING(host_name,cycle_type,period)
  LEFT JOIN browser_maps b USING(host_name,cycle_type,period)
  LEFT JOIN os_maps o USING(host_name,cycle_type,period)
  LEFT JOIN bucket_maps d USING(host_name,cycle_type,period)
  LEFT JOIN error_maps e USING(host_name,cycle_type,period)
  LEFT JOIN histogram_maps h USING(host_name,cycle_type,period)
  LEFT JOIN mode_maps mm USING(host_name,cycle_type,period)
ON CONFLICT(resource_uuid,cycle_type,report_ts) DO UPDATE
    SET request_qty=excluded.request_qty,status_code=excluded.status_code,spider=excluded.spider,
        browser=excluded.browser,os=excluded.os,duration=excluded.duration,
        inbound_bytes=excluded.inbound_bytes,outbound_bytes=excluded.outbound_bytes,
        error_counts=excluded.error_counts,latency_histogram=excluded.latency_histogram,response_mode_metrics=excluded.response_mode_metrics,
        is_partial=excluded.is_partial,report_time=excluded.report_time,update_time=clock_timestamp()
  WHERE (resource_report.request_qty,resource_report.status_code,resource_report.spider,
         resource_report.browser,resource_report.os,resource_report.duration,
         resource_report.inbound_bytes,resource_report.outbound_bytes,
         resource_report.error_counts,resource_report.latency_histogram,resource_report.response_mode_metrics,
         resource_report.is_partial,resource_report.report_time)
        IS DISTINCT FROM
        (excluded.request_qty,excluded.status_code,excluded.spider,excluded.browser,
         excluded.os,excluded.duration,excluded.inbound_bytes,excluded.outbound_bytes,
         excluded.error_counts,excluded.latency_histogram,excluded.response_mode_metrics,excluded.is_partial,excluded.report_time)`

const materializeDailyResourceReportsSQL = `
WITH contributions AS MATERIALIZED (
    SELECT host_name,
           (period / 86400) * 86400 AS period,
           request_qty,duration_sum,duration_max,inbound_bytes,outbound_bytes,
           status_code,spider,browser,os,duration_buckets,error_counts,latency_histogram,response_mode_metrics,is_final
      FROM gateway_traffic_contribution
     WHERE cycle_type = 10
       AND ($1::bigint IS NULL OR period >= $1)
       AND ($2::bigint IS NULL OR period < $2)
),
totals AS (
    SELECT host_name,period,sum(request_qty) AS request_qty,sum(duration_sum) AS duration_sum,
           max(duration_max) AS duration_max,sum(inbound_bytes) AS inbound_bytes,
           sum(outbound_bytes) AS outbound_bytes,bool_or(NOT is_final) AS is_partial
      FROM contributions GROUP BY host_name,period
),
status_values AS (
    SELECT c.host_name,c.period,e.key,sum((e.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.status_code) e
     GROUP BY c.host_name,c.period,e.key
),
spider_values AS (
    SELECT c.host_name,c.period,e.key,sum((e.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.spider) e
     GROUP BY c.host_name,c.period,e.key
),
browser_values AS (
    SELECT c.host_name,c.period,e.key,sum((e.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.browser) e
     GROUP BY c.host_name,c.period,e.key
),
os_values AS (
    SELECT c.host_name,c.period,e.key,sum((e.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.os) e
     GROUP BY c.host_name,c.period,e.key
),
bucket_values AS (
    SELECT c.host_name,c.period,e.key,sum((e.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.duration_buckets) e
     GROUP BY c.host_name,c.period,e.key
),
error_values AS (
    SELECT c.host_name,c.period,e.key,sum((e.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.error_counts) e
     GROUP BY c.host_name,c.period,e.key
),
histogram_values AS (
    SELECT c.host_name,c.period,e.key,sum((e.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each_text(c.latency_histogram) e
     GROUP BY c.host_name,c.period,e.key
),
mode_values AS (
    SELECT c.host_name,c.period,m.key AS mode,k.key AS metric,sum((k.value)::bigint) AS count
      FROM contributions c CROSS JOIN LATERAL jsonb_each(c.response_mode_metrics) m
      CROSS JOIN LATERAL jsonb_each_text(m.value) k
     GROUP BY c.host_name,c.period,m.key,k.key
),
status_maps AS (
    SELECT host_name,period,jsonb_object_agg(key,count) AS value
      FROM status_values GROUP BY host_name,period
),
spider_maps AS (
    SELECT host_name,period,jsonb_object_agg(key,count) AS value
      FROM spider_values GROUP BY host_name,period
),
browser_maps AS (
    SELECT host_name,period,jsonb_object_agg(key,count) AS value
      FROM browser_values GROUP BY host_name,period
),
os_maps AS (
    SELECT host_name,period,jsonb_object_agg(key,count) AS value
      FROM os_values GROUP BY host_name,period
),
bucket_maps AS (
    SELECT host_name,period,jsonb_object_agg(key,count) AS value
      FROM bucket_values GROUP BY host_name,period
),
error_maps AS (
    SELECT host_name,period,jsonb_object_agg(key,count) AS value
      FROM error_values GROUP BY host_name,period
),
histogram_maps AS (
    SELECT host_name,period,jsonb_object_agg(key,count) AS value
      FROM histogram_values GROUP BY host_name,period
),
mode_metric_maps AS (
    SELECT host_name,period,mode,jsonb_object_agg(metric,count) AS value
      FROM mode_values GROUP BY host_name,period,mode
),
mode_maps AS (
    SELECT host_name,period,jsonb_object_agg(mode,value) AS value
      FROM mode_metric_maps GROUP BY host_name,period
)
INSERT INTO resource_report(resource_uuid,cycle_type,report_time,report_ts,request_qty,inbound_bytes,outbound_bytes,status_code,spider,browser,os,duration,error_counts,latency_histogram,response_mode_metrics,is_partial,create_time,update_time)
SELECT t.host_name,20,to_timestamp(t.period),t.period,t.request_qty,t.inbound_bytes,t.outbound_bytes,
       COALESCE(s.value,'{}'::jsonb),
       COALESCE(sp.value,'{}'::jsonb),
       COALESCE(b.value,'{}'::jsonb),
       COALESCE(o.value,'{}'::jsonb),
       COALESCE(d.value,'{}'::jsonb)
           || jsonb_build_object('avg',CASE WHEN t.request_qty=0 THEN 0 ELSE (t.duration_sum/t.request_qty)::bigint END,'max',t.duration_max),
       COALESCE(e.value,'{}'::jsonb),
       COALESCE(h.value,'{}'::jsonb),
       COALESCE(mm.value,'{}'::jsonb),
       t.is_partial,
       clock_timestamp(),clock_timestamp()
  FROM totals t
  LEFT JOIN status_maps s USING(host_name,period)
  LEFT JOIN spider_maps sp USING(host_name,period)
  LEFT JOIN browser_maps b USING(host_name,period)
  LEFT JOIN os_maps o USING(host_name,period)
  LEFT JOIN bucket_maps d USING(host_name,period)
  LEFT JOIN error_maps e USING(host_name,period)
  LEFT JOIN histogram_maps h USING(host_name,period)
  LEFT JOIN mode_maps mm USING(host_name,period)
ON CONFLICT(resource_uuid,cycle_type,report_ts) DO UPDATE
    SET request_qty=excluded.request_qty,status_code=excluded.status_code,spider=excluded.spider,
        browser=excluded.browser,os=excluded.os,duration=excluded.duration,
        inbound_bytes=excluded.inbound_bytes,outbound_bytes=excluded.outbound_bytes,
        error_counts=excluded.error_counts,latency_histogram=excluded.latency_histogram,response_mode_metrics=excluded.response_mode_metrics,
        is_partial=excluded.is_partial,report_time=excluded.report_time,update_time=clock_timestamp()
  WHERE (resource_report.request_qty,resource_report.status_code,resource_report.spider,
         resource_report.browser,resource_report.os,resource_report.duration,
         resource_report.inbound_bytes,resource_report.outbound_bytes,
         resource_report.error_counts,resource_report.latency_histogram,resource_report.response_mode_metrics,
         resource_report.is_partial,resource_report.report_time)
        IS DISTINCT FROM
        (excluded.request_qty,excluded.status_code,excluded.spider,excluded.browser,
         excluded.os,excluded.duration,excluded.inbound_bytes,excluded.outbound_bytes,
         excluded.error_counts,excluded.latency_histogram,excluded.response_mode_metrics,excluded.is_partial,excluded.report_time)`

// backfillDailyResourceReportsSQL folds persisted hourly reports into daily
// reports. report_ts is authoritative here because historical hourly rows may
// carry an invalid legacy report_time such as PostgreSQL's infinity.
const backfillDailyResourceReportsSQL = `
WITH hourly AS MATERIALIZED (
    SELECT resource_uuid,
           (report_ts / 86400) * 86400 AS period,
           request_qty,inbound_bytes,outbound_bytes,
           COALESCE(status_code, '{}'::jsonb) AS status_code,
           COALESCE(spider, '{}'::jsonb) AS spider,
           COALESCE(browser, '{}'::jsonb) AS browser,
           COALESCE(os, '{}'::jsonb) AS os,
           COALESCE(duration, '{}'::jsonb) AS duration,
           COALESCE(error_counts, '{}'::jsonb) AS error_counts,
           COALESCE(latency_histogram, '{}'::jsonb) AS latency_histogram,
           COALESCE(response_mode_metrics, '{}'::jsonb) AS response_mode_metrics,
           is_partial
      FROM resource_report
     WHERE cycle_type = 10
       AND report_ts >= $1::bigint
       AND report_ts < $2::bigint
),
totals AS (
    SELECT resource_uuid,period,sum(request_qty) AS request_qty,
           sum(inbound_bytes) AS inbound_bytes,sum(outbound_bytes) AS outbound_bytes,
           bool_or(is_partial) AS is_partial
      FROM hourly GROUP BY resource_uuid,period
),
status_values AS (
    SELECT h.resource_uuid,h.period,e.key,sum((e.value)::bigint) AS count
      FROM hourly h CROSS JOIN LATERAL jsonb_each_text(h.status_code) e
     GROUP BY h.resource_uuid,h.period,e.key
),
spider_values AS (
    SELECT h.resource_uuid,h.period,e.key,sum((e.value)::bigint) AS count
      FROM hourly h CROSS JOIN LATERAL jsonb_each_text(h.spider) e
     GROUP BY h.resource_uuid,h.period,e.key
),
browser_values AS (
    SELECT h.resource_uuid,h.period,e.key,sum((e.value)::bigint) AS count
      FROM hourly h CROSS JOIN LATERAL jsonb_each_text(h.browser) e
     GROUP BY h.resource_uuid,h.period,e.key
),
os_values AS (
    SELECT h.resource_uuid,h.period,e.key,sum((e.value)::bigint) AS count
      FROM hourly h CROSS JOIN LATERAL jsonb_each_text(h.os) e
     GROUP BY h.resource_uuid,h.period,e.key
),
bucket_values AS (
    SELECT h.resource_uuid,h.period,e.key,sum((e.value)::bigint) AS count
      FROM hourly h CROSS JOIN LATERAL jsonb_each_text(h.duration - 'avg' - 'max') e
     GROUP BY h.resource_uuid,h.period,e.key
),
error_values AS (
    SELECT h.resource_uuid,h.period,e.key,sum((e.value)::bigint) AS count
      FROM hourly h CROSS JOIN LATERAL jsonb_each_text(h.error_counts) e
     GROUP BY h.resource_uuid,h.period,e.key
),
histogram_values AS (
    SELECT h.resource_uuid,h.period,e.key,sum((e.value)::bigint) AS count
      FROM hourly h CROSS JOIN LATERAL jsonb_each_text(h.latency_histogram) e
     GROUP BY h.resource_uuid,h.period,e.key
),
mode_values AS (
    SELECT h.resource_uuid,h.period,m.key AS mode,k.key AS metric,sum((k.value)::bigint) AS count
      FROM hourly h CROSS JOIN LATERAL jsonb_each(h.response_mode_metrics) m
      CROSS JOIN LATERAL jsonb_each_text(m.value) k
     GROUP BY h.resource_uuid,h.period,m.key,k.key
),
duration_stats AS (
    SELECT resource_uuid,period,
           COALESCE(floor(sum(COALESCE((duration->>'avg')::numeric, 0) * request_qty) / NULLIF(sum(request_qty), 0))::bigint, 0) AS avg_duration,
           max(COALESCE((duration->>'max')::bigint, 0)) AS max_duration
      FROM hourly GROUP BY resource_uuid,period
),
status_maps AS (
    SELECT resource_uuid,period,jsonb_object_agg(key,count) AS value
      FROM status_values GROUP BY resource_uuid,period
),
spider_maps AS (
    SELECT resource_uuid,period,jsonb_object_agg(key,count) AS value
      FROM spider_values GROUP BY resource_uuid,period
),
browser_maps AS (
    SELECT resource_uuid,period,jsonb_object_agg(key,count) AS value
      FROM browser_values GROUP BY resource_uuid,period
),
os_maps AS (
    SELECT resource_uuid,period,jsonb_object_agg(key,count) AS value
      FROM os_values GROUP BY resource_uuid,period
),
bucket_maps AS (
    SELECT resource_uuid,period,jsonb_object_agg(key,count) AS value
      FROM bucket_values GROUP BY resource_uuid,period
),
error_maps AS (
    SELECT resource_uuid,period,jsonb_object_agg(key,count) AS value
      FROM error_values GROUP BY resource_uuid,period
),
histogram_maps AS (
    SELECT resource_uuid,period,jsonb_object_agg(key,count) AS value
      FROM histogram_values GROUP BY resource_uuid,period
),
mode_metric_maps AS (
    SELECT resource_uuid,period,mode,jsonb_object_agg(metric,count) AS value
      FROM mode_values GROUP BY resource_uuid,period,mode
),
mode_maps AS (
    SELECT resource_uuid,period,jsonb_object_agg(mode,value) AS value
      FROM mode_metric_maps GROUP BY resource_uuid,period
)
INSERT INTO resource_report(resource_uuid,cycle_type,report_time,report_ts,request_qty,inbound_bytes,outbound_bytes,status_code,spider,browser,os,duration,error_counts,latency_histogram,response_mode_metrics,is_partial,create_time,update_time)
SELECT t.resource_uuid,20,to_timestamp(t.period),t.period,t.request_qty,t.inbound_bytes,t.outbound_bytes,
       COALESCE(s.value,'{}'::jsonb),
       COALESCE(sp.value,'{}'::jsonb),
       COALESCE(b.value,'{}'::jsonb),
       COALESCE(o.value,'{}'::jsonb),
       COALESCE(d.value,'{}'::jsonb) || jsonb_build_object('avg',ds.avg_duration,'max',ds.max_duration),
       COALESCE(e.value,'{}'::jsonb),
       COALESCE(h.value,'{}'::jsonb),
       COALESCE(mm.value,'{}'::jsonb),
       t.is_partial,
       clock_timestamp(),clock_timestamp()
  FROM totals t
  JOIN duration_stats ds USING(resource_uuid,period)
  LEFT JOIN status_maps s USING(resource_uuid,period)
  LEFT JOIN spider_maps sp USING(resource_uuid,period)
  LEFT JOIN browser_maps b USING(resource_uuid,period)
  LEFT JOIN os_maps o USING(resource_uuid,period)
  LEFT JOIN bucket_maps d USING(resource_uuid,period)
  LEFT JOIN error_maps e USING(resource_uuid,period)
  LEFT JOIN histogram_maps h USING(resource_uuid,period)
  LEFT JOIN mode_maps mm USING(resource_uuid,period)
ON CONFLICT(resource_uuid,cycle_type,report_ts) DO UPDATE
    SET request_qty=excluded.request_qty,status_code=excluded.status_code,spider=excluded.spider,
        browser=excluded.browser,os=excluded.os,duration=excluded.duration,
        inbound_bytes=excluded.inbound_bytes,outbound_bytes=excluded.outbound_bytes,
        error_counts=excluded.error_counts,latency_histogram=excluded.latency_histogram,response_mode_metrics=excluded.response_mode_metrics,
        is_partial=excluded.is_partial,report_time=excluded.report_time,update_time=clock_timestamp()
  WHERE (resource_report.request_qty,resource_report.status_code,resource_report.spider,
         resource_report.browser,resource_report.os,resource_report.duration,
         resource_report.inbound_bytes,resource_report.outbound_bytes,
         resource_report.error_counts,resource_report.latency_histogram,resource_report.response_mode_metrics,
         resource_report.is_partial,resource_report.report_time)
        IS DISTINCT FROM
        (excluded.request_qty,excluded.status_code,excluded.spider,excluded.browser,
         excluded.os,excluded.duration,excluded.inbound_bytes,excluded.outbound_bytes,
         excluded.error_counts,excluded.latency_histogram,excluded.response_mode_metrics,excluded.is_partial,excluded.report_time)`

// materializeAnalyticsColumns enriches the legacy report rows after their
// existing materialization. Keeping this separate makes the compatibility
// insert readable and lets an upgrade start with zero-valued new columns.
func materializeAnalyticsColumns(ctx context.Context, tx pgx.Tx, beginUnix, endUnix any) error {
	_, err := tx.Exec(ctx, `WITH totals AS (
		SELECT host_name,cycle_type,period,sum(inbound_bytes) AS inbound_bytes,sum(outbound_bytes) AS outbound_bytes,bool_or(NOT is_final) AS is_partial
		FROM gateway_traffic_contribution WHERE cycle_type IN (5,10)
			AND ($1::bigint IS NULL OR period >= $1) AND ($2::bigint IS NULL OR period < $2)
		GROUP BY host_name,cycle_type,period
	), errors AS (
		SELECT c.host_name,c.cycle_type,c.period,e.key,sum((e.value)::bigint) AS value
		FROM gateway_traffic_contribution c CROSS JOIN LATERAL jsonb_each_text(c.error_counts) e
		WHERE c.cycle_type IN (5,10) AND ($1::bigint IS NULL OR c.period >= $1) AND ($2::bigint IS NULL OR c.period < $2)
		GROUP BY c.host_name,c.cycle_type,c.period,e.key
	), histograms AS (
		SELECT c.host_name,c.cycle_type,c.period,e.key,sum((e.value)::bigint) AS value
		FROM gateway_traffic_contribution c CROSS JOIN LATERAL jsonb_each_text(c.latency_histogram) e
		WHERE c.cycle_type IN (5,10) AND ($1::bigint IS NULL OR c.period >= $1) AND ($2::bigint IS NULL OR c.period < $2)
		GROUP BY c.host_name,c.cycle_type,c.period,e.key
	), mode_metrics AS (
		SELECT c.host_name,c.cycle_type,c.period,m.key AS mode,k.key AS metric,sum((k.value)::bigint) AS value
		FROM gateway_traffic_contribution c CROSS JOIN LATERAL jsonb_each(c.response_mode_metrics) m CROSS JOIN LATERAL jsonb_each_text(m.value) k
		WHERE c.cycle_type IN (5,10) AND ($1::bigint IS NULL OR c.period >= $1) AND ($2::bigint IS NULL OR c.period < $2)
		GROUP BY c.host_name,c.cycle_type,c.period,m.key,k.key
	)
	UPDATE resource_report r SET inbound_bytes=t.inbound_bytes,outbound_bytes=t.outbound_bytes,is_partial=t.is_partial,
		error_counts=COALESCE((SELECT jsonb_object_agg(e.key,e.value) FROM errors e WHERE e.host_name=t.host_name AND e.cycle_type=t.cycle_type AND e.period=t.period),'{}'::jsonb),
		latency_histogram=COALESCE((SELECT jsonb_object_agg(h.key,h.value) FROM histograms h WHERE h.host_name=t.host_name AND h.cycle_type=t.cycle_type AND h.period=t.period),'{}'::jsonb)
		,response_mode_metrics=COALESCE((SELECT jsonb_object_agg(mm.mode,mm.value) FROM (SELECT mode,jsonb_object_agg(metric,value) AS value FROM mode_metrics WHERE host_name=t.host_name AND cycle_type=t.cycle_type AND period=t.period GROUP BY mode) mm),'{}'::jsonb)
	FROM totals t WHERE r.resource_uuid=t.host_name AND r.cycle_type=t.cycle_type AND r.report_ts=t.period`, beginUnix, endUnix)
	if err != nil {
		return fmt.Errorf("materialize detailed minute/hour report: %w", err)
	}
	_, err = tx.Exec(ctx, `WITH totals AS (
		SELECT resource_uuid,date_trunc('day',report_time) AS report_time,sum(inbound_bytes) AS inbound_bytes,sum(outbound_bytes) AS outbound_bytes,bool_or(is_partial) AS is_partial
		FROM resource_report WHERE cycle_type=10
			AND ($1::bigint IS NULL OR report_ts >= $1) AND ($2::bigint IS NULL OR report_ts < $2)
		GROUP BY resource_uuid,date_trunc('day',report_time)
	), errors AS (
		SELECT r.resource_uuid,date_trunc('day',r.report_time) AS report_time,e.key,sum((e.value)::bigint) AS value
		FROM resource_report r CROSS JOIN LATERAL jsonb_each_text(r.error_counts) e WHERE r.cycle_type=10
			AND ($1::bigint IS NULL OR r.report_ts >= $1) AND ($2::bigint IS NULL OR r.report_ts < $2)
		GROUP BY r.resource_uuid,date_trunc('day',r.report_time),e.key
	), histograms AS (
		SELECT r.resource_uuid,date_trunc('day',r.report_time) AS report_time,e.key,sum((e.value)::bigint) AS value
		FROM resource_report r CROSS JOIN LATERAL jsonb_each_text(r.latency_histogram) e WHERE r.cycle_type=10
			AND ($1::bigint IS NULL OR r.report_ts >= $1) AND ($2::bigint IS NULL OR r.report_ts < $2)
		GROUP BY r.resource_uuid,date_trunc('day',r.report_time),e.key
	), mode_metrics AS (
		SELECT r.resource_uuid,date_trunc('day',r.report_time) AS report_time,m.key AS mode,k.key AS metric,sum((k.value)::bigint) AS value
		FROM resource_report r CROSS JOIN LATERAL jsonb_each(r.response_mode_metrics) m CROSS JOIN LATERAL jsonb_each_text(m.value) k WHERE r.cycle_type=10
			AND ($1::bigint IS NULL OR r.report_ts >= $1) AND ($2::bigint IS NULL OR r.report_ts < $2)
		GROUP BY r.resource_uuid,date_trunc('day',r.report_time),m.key,k.key
	)
	UPDATE resource_report r SET inbound_bytes=t.inbound_bytes,outbound_bytes=t.outbound_bytes,is_partial=t.is_partial,
		error_counts=COALESCE((SELECT jsonb_object_agg(e.key,e.value) FROM errors e WHERE e.resource_uuid=t.resource_uuid AND e.report_time=t.report_time),'{}'::jsonb),
		latency_histogram=COALESCE((SELECT jsonb_object_agg(h.key,h.value) FROM histograms h WHERE h.resource_uuid=t.resource_uuid AND h.report_time=t.report_time),'{}'::jsonb)
		,response_mode_metrics=COALESCE((SELECT jsonb_object_agg(mm.mode,mm.value) FROM (SELECT mode,jsonb_object_agg(metric,value) AS value FROM mode_metrics WHERE resource_uuid=t.resource_uuid AND report_time=t.report_time GROUP BY mode) mm),'{}'::jsonb)
	FROM totals t WHERE r.resource_uuid=t.resource_uuid AND r.cycle_type=20 AND r.report_time=t.report_time`, beginUnix, endUnix)
	if err != nil {
		return fmt.Errorf("materialize detailed daily report: %w", err)
	}
	return nil
}

// FlushRequestHistory is used by telemetry.Aggregator after its contribution
// snapshot is durable. It intentionally has no host_traffic writes.
func (p *Postgres) FlushRequestHistory(ctx context.Context, events []telemetry.Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, event := range events {
		host := strings.TrimSpace(strings.Split(event.Host, ":")[0])
		if host == "" {
			continue
		}
		var requestID int64
		if p.requestID != nil {
			requestID, err = p.requestID()
		} else {
			err = tx.QueryRow(ctx, "SELECT nextval('gateway_request_history_id_seq')").Scan(&requestID)
		}
		if err != nil {
			return err
		}
		if err := insertRequestHistory(ctx, tx, requestID, event, host); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func insertRequestHistory(ctx context.Context, tx pgx.Tx, requestID int64, event telemetry.Event, host string) error {
	requestHeaders, err := json.Marshal(headerMap(event.RequestHeaders))
	if err != nil {
		return fmt.Errorf("encode request headers: %w", err)
	}
	responseHeaders, err := json.Marshal(headerMap(event.ResponseHeaders))
	if err != nil {
		return fmt.Errorf("encode response headers: %w", err)
	}
	historyStatus := event.HistoryStatus
	if historyStatus == 0 {
		historyStatus = event.Status
	}
	statusSource := event.StatusSource
	if statusSource == "" {
		statusSource = "gateway"
	}
	proxyStage := event.ProxyStage
	if proxyStage == "" {
		proxyStage = "completed"
	}
	var upstream any
	if event.UpstreamStatus != nil {
		upstream = *event.UpstreamStatus
	}
	var blacklistRule any
	if event.BlacklistRuleID != 0 {
		blacklistRule = event.BlacklistRuleID
	}
	requestBody := redactJSONBody(event.RequestBodyBase64, event.RequestContentType)
	responseBody := redactJSONBody(event.ResponseBodyBase64, event.ResponseContentType)
	requestTruncated, responseTruncated := event.RequestBodyTruncated, event.ResponseBodyTruncated
	// AllMetadata is useful for sampled success metadata, but raw bodies stay
	// error-only even when that mode is enabled. This keeps the audit setting
	// from silently becoming a full traffic payload recorder.
	if historyStatus < 400 && event.ErrorCategory == "" {
		requestBody, responseBody = "", ""
		requestTruncated, responseTruncated = false, false
	}
	redactedQuery := redactQuery(event.Query)
	redactedEvent := event
	redactedEvent.Query = redactedQuery
	info := event.UserAgentInfo
	var isBot any
	if info.ParseStatus == "parsed" {
		isBot = info.IsBot
	}
	_, err = tx.Exec(ctx, "INSERT INTO request_history(id,request_time,request_ts,duration_ms,client_ip,method,scheme,host,path,query_string,url,protocol,status_code,user_agent,user_agent_browser,user_agent_browser_version,user_agent_browser_major,user_agent_os,user_agent_os_version,user_agent_os_major,user_agent_device_type,user_agent_device_brand,user_agent_device_model,user_agent_is_bot,user_agent_bot_name,user_agent_parse_status,proxy_stage,exception_stage,forwarder_error,request_headers,response_headers,request_body_base64,response_body_base64,request_content_type,response_content_type,request_body_truncated,response_body_truncated,exception_type,exception_message,exception_stack_trace,upstream_status_code,status_source,error_category,blacklist_scope,blacklist_rule_id,blacklist_cidr,trace_id) VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),$13,NULLIF($14,''),NULLIF($15,''),NULLIF($16,''),NULLIF($17,''),NULLIF($18,''),NULLIF($19,''),NULLIF($20,''),NULLIF($21,''),NULLIF($22,''),NULLIF($23,''),$24,$25,NULLIF($26,''),$27,NULLIF($28,''),NULLIF($29,''),$30::jsonb,$31::jsonb,NULLIF($32,''),NULLIF($33,''),NULLIF($34,''),NULLIF($35,''),$36,$37,NULLIF($38,''),NULLIF($39,''),NULLIF($40,''),$41,NULLIF($42,''),NULLIF($43,''),NULLIF($44,''),NULLIF($45,''),$46,NULLIF($47,''),NULLIF($48,''))", requestID, event.At, event.At.UTC().UnixMilli(), event.DurationMS, event.ClientIP, event.Method, event.Scheme, host, event.Path, redactedQuery, requestURL(redactedEvent), event.Protocol, historyStatus, event.UserAgent, info.BrowserName, info.BrowserVersion, info.BrowserMajor, info.OSName, info.OSVersion, info.OSMajor, info.DeviceType, info.DeviceBrand, info.DeviceModel, isBot, info.BotName, info.ParseStatus, proxyStage, event.ExceptionStage, event.ForwarderError, requestHeaders, responseHeaders, requestBody, responseBody, event.RequestContentType, event.ResponseContentType, requestTruncated, responseTruncated, event.ExceptionType, event.ExceptionMessage, event.ExceptionStackTrace, upstream, statusSource, event.ErrorCategory, event.BlacklistScope, blacklistRule, event.BlacklistCIDR, event.TraceID)
	return err
}

func headerMap(value map[string][]string) map[string][]string {
	out := make(map[string][]string, len(value))
	for key, values := range value {
		if sensitiveAuditKey(key) {
			out[key] = []string{"[REDACTED]"}
			continue
		}
		out[key] = append([]string(nil), values...)
	}
	return out
}

func sensitiveAuditKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", ""))
	return strings.Contains(key, "authorization") || strings.Contains(key, "cookie") || strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "password") || strings.Contains(key, "apikey") || strings.Contains(key, "api_key")
}

func redactQuery(raw string) string {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return ""
	}
	for key := range values {
		if sensitiveAuditKey(key) {
			values[key] = []string{"[REDACTED]"}
		}
	}
	return values.Encode()
}

func redactJSONBody(encoded, contentType string) string {
	if encoded == "" || !strings.Contains(strings.ToLower(contentType), "json") {
		return encoded
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return encoded
	}
	redactJSONValue(value)
	clean, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(clean)
}

func redactJSONValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveAuditKey(key) {
				typed[key] = "[REDACTED]"
			} else {
				redactJSONValue(child)
			}
		}
	case []any:
		for _, child := range typed {
			redactJSONValue(child)
		}
	}
}
