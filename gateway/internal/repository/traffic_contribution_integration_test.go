//go:build integration

package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/management"
	"yaginx-gateway/internal/telemetry"
)

func TestTrafficContributionMaterializesAbsoluteMultiProcessTotals(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	schema := "yaginx_goal_traffic" + uuid.NewString()[:10]
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolation, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolation, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolation)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool)
	store.SetTrafficMaterializerLockNamespace("yaginx:test-resource-report")
	nodeIDs := []string{uuid.NewString(), uuid.NewString()}
	for _, node := range nodeIDs {
		if _, err := store.RegisterNode(ctx, management.NodeRegistrationRequest{NodeID: node, MachineFingerprint: strings.Repeat("b", 64)}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ApproveNode(ctx, node); err != nil {
			t.Fatal(err)
		}
	}
	when := time.Now().UTC().Truncate(time.Hour)
	first, second := telemetry.NewAggregatorWithUserAgent(store, nodeIDs[0], true, 16), telemetry.NewAggregatorWithUserAgent(store, nodeIDs[1], true, 16)
	if err := first.Flush(ctx, []telemetry.Event{{Host: "traffic.example.invalid", Status: 200, At: when, DurationMS: 50, Inbound: 10, Outbound: 20, UserAgent: "Mozilla/5.0 (Windows NT 10.0) Chrome/120.0"}}); err != nil {
		t.Fatal(err)
	}
	if err := second.Flush(ctx, []telemetry.Event{{Host: "traffic.example.invalid", Status: 502, At: when, DurationMS: 700, Inbound: 30, Outbound: 40, UserAgent: "Googlebot/2.1"}}); err != nil {
		t.Fatal(err)
	}
	// Replaying the first process's absolute snapshot must not add a third request.
	if err := first.Flush(ctx, nil); err != nil {
		t.Fatal(err)
	}
	windowBegin, windowEnd := when.Add(-time.Minute), when.Add(time.Minute)
	if err := store.MaterializeTrafficReports(ctx, windowBegin, windowEnd, TrafficMaterializationOptions{Minutely: true, StatementTimeout: time.Minute}); err != nil {
		t.Fatal(err)
	}
	var hourlyBefore int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM resource_report WHERE resource_uuid='traffic.example.invalid' AND cycle_type=10 AND report_ts=$1", when.Unix()).Scan(&hourlyBefore); err != nil {
		t.Fatal(err)
	}
	if hourlyBefore != 0 {
		t.Fatalf("minutely materialization wrote %d hourly reports", hourlyBefore)
	}
	if err := store.MaterializeTrafficReports(ctx, windowBegin, windowEnd, TrafficMaterializationOptions{Hourly: true, StatementTimeout: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if err := store.MaterializeTrafficReports(ctx, windowBegin, windowEnd, TrafficMaterializationOptions{Daily: true, StatementTimeout: time.Minute}); err != nil {
		t.Fatal(err)
	}
	var requests, inbound, outbound int64
	if err := pool.QueryRow(ctx, "SELECT request_counts,inbound_bytes,outbound_bytes FROM host_traffic WHERE host_name='traffic.example.invalid' AND period=$1", when.Unix()).Scan(&requests, &inbound, &outbound); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || inbound != 40 || outbound != 60 {
		t.Fatalf("host traffic=%d/%d/%d", requests, inbound, outbound)
	}
	var reportRequests int64
	if err := pool.QueryRow(ctx, "SELECT request_qty FROM resource_report WHERE resource_uuid='traffic.example.invalid' AND cycle_type=10 AND report_ts=$1", when.Unix()).Scan(&reportRequests); err != nil {
		t.Fatal(err)
	}
	if reportRequests != 2 {
		t.Fatalf("report requests=%d", reportRequests)
	}
	if err := pool.QueryRow(ctx, "SELECT request_qty FROM resource_report WHERE resource_uuid='traffic.example.invalid' AND cycle_type=5 AND report_ts=$1", when.Truncate(time.Minute).Unix()).Scan(&reportRequests); err != nil {
		t.Fatal(err)
	}
	if reportRequests != 2 {
		t.Fatalf("minute report requests=%d", reportRequests)
	}
	var reportInbound, reportOutbound int64
	var reportErrors, reportHistogram []byte
	if err := pool.QueryRow(ctx, "SELECT inbound_bytes,outbound_bytes,error_counts,latency_histogram FROM resource_report WHERE resource_uuid='traffic.example.invalid' AND cycle_type=10 AND report_ts=$1", when.Unix()).Scan(&reportInbound, &reportOutbound, &reportErrors, &reportHistogram); err != nil {
		t.Fatal(err)
	}
	var errorsBySource, histogram map[string]int64
	if err := json.Unmarshal(reportErrors, &errorsBySource); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(reportHistogram, &histogram); err != nil {
		t.Fatal(err)
	}
	if reportInbound != 40 || reportOutbound != 60 || errorsBySource["upstream:server_error"] != 1 || histogram["<=50"] != 1 || histogram["<=1000"] != 1 {
		t.Fatalf("detailed report=%d/%d errors=%#v histogram=%#v", reportInbound, reportOutbound, errorsBySource, histogram)
	}
	if err := pool.QueryRow(ctx, "SELECT request_qty FROM resource_report WHERE resource_uuid='traffic.example.invalid' AND cycle_type=20 AND report_ts=$1", when.Truncate(24*time.Hour).Unix()).Scan(&reportRequests); err != nil {
		t.Fatal(err)
	}
	if reportRequests != 2 {
		t.Fatalf("daily report requests=%d", reportRequests)
	}
	var beforeUpdate time.Time
	if err := pool.QueryRow(ctx, "SELECT update_time FROM resource_report WHERE resource_uuid='traffic.example.invalid' AND cycle_type=10 AND report_ts=$1", when.Unix()).Scan(&beforeUpdate); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := store.MaterializeTraffic(ctx); err != nil {
		t.Fatal(err)
	}
	var afterUpdate time.Time
	if err := pool.QueryRow(ctx, "SELECT update_time FROM resource_report WHERE resource_uuid='traffic.example.invalid' AND cycle_type=10 AND report_ts=$1", when.Unix()).Scan(&afterUpdate); err != nil {
		t.Fatal(err)
	}
	if !afterUpdate.Equal(beforeUpdate) {
		t.Fatalf("no-op materialization updated report row from %s to %s", beforeUpdate, afterUpdate)
	}
	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lockTx, err := blocker.Begin(ctx)
	if err != nil {
		blocker.Release()
		t.Fatal(err)
	}
	if _, err := lockTx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "yaginx:test-resource-report"); err != nil {
		_ = lockTx.Rollback(ctx)
		blocker.Release()
		t.Fatal(err)
	}
	err = store.MaterializeTrafficReports(ctx, windowBegin, windowEnd, TrafficMaterializationOptions{Minutely: true, StatementTimeout: time.Minute})
	if !errors.Is(err, ErrTrafficMaterializerBusy) {
		_ = lockTx.Rollback(ctx)
		blocker.Release()
		t.Fatalf("materializer error=%v, want busy", err)
	}
	if err := lockTx.Rollback(ctx); err != nil {
		blocker.Release()
		t.Fatal(err)
	}
	blocker.Release()
	if _, err := pool.Exec(ctx, "INSERT INTO resource_report(resource_uuid,cycle_type,report_time,report_ts,request_qty,status_code,spider,browser,os,duration,create_time,update_time) VALUES('infinite.example.invalid',10,'infinity',$1,1,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{\"avg\":10,\"max\":10}'::jsonb,clock_timestamp(),clock_timestamp())", when.Unix()); err != nil {
		t.Fatal(err)
	}
	if err := store.BackfillDailyResourceReports(ctx, when.Truncate(24*time.Hour), when.Truncate(24*time.Hour).Add(24*time.Hour), time.Minute); err != nil {
		t.Fatal(err)
	}
	var dailyReportTimeMatches bool
	if err := pool.QueryRow(ctx, "SELECT report_time = to_timestamp(report_ts) FROM resource_report WHERE resource_uuid='infinite.example.invalid' AND cycle_type=20 AND report_ts=$1", when.Truncate(24*time.Hour).Unix()).Scan(&dailyReportTimeMatches); err != nil || !dailyReportTimeMatches {
		t.Fatalf("daily report from infinite hourly mirror matches=%t err=%v", dailyReportTimeMatches, err)
	}
	outsideWindow := when.AddDate(0, 0, -7).Unix()
	if _, err := pool.Exec(ctx, "INSERT INTO gateway_traffic_contribution(node_id,process_id,host_name,cycle_type,period,request_qty,duration_sum,duration_max,inbound_bytes,outbound_bytes,status_code,updated_at) VALUES($1::uuid,$2::uuid,'outside-window.example.invalid',10,$3,1,10,10,0,0,'{}'::jsonb,clock_timestamp())", nodeIDs[0], uuid.NewString(), outsideWindow); err != nil {
		t.Fatal(err)
	}
	fullWindowBegin := when.Truncate(24 * time.Hour)
	if err := store.MaterializeTrafficWindow(ctx, fullWindowBegin, fullWindowBegin.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var outsideCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM resource_report WHERE resource_uuid='outside-window.example.invalid'").Scan(&outsideCount); err != nil {
		t.Fatal(err)
	}
	if outsideCount != 0 {
		t.Fatalf("window materialization wrote %d historical reports", outsideCount)
	}
	var duration, status, spider, browser, osValues []byte
	if err := pool.QueryRow(ctx, "SELECT duration,status_code,spider,browser,os FROM resource_report WHERE resource_uuid='traffic.example.invalid' AND cycle_type=10 AND report_ts=$1", when.Unix()).Scan(&duration, &status, &spider, &browser, &osValues); err != nil {
		t.Fatal(err)
	}
	var durations, statuses, spiders, browsers, oses map[string]int64
	for _, item := range []struct {
		raw []byte
		out *map[string]int64
	}{{duration, &durations}, {status, &statuses}, {spider, &spiders}, {browser, &browsers}, {osValues, &oses}} {
		if err := json.Unmarshal(item.raw, item.out); err != nil {
			t.Fatal(err)
		}
	}
	if durations["avg"] != 375 || durations["max"] != 700 || durations["<=100"] != 1 || durations[">500 & <=1000"] != 1 || statuses["200"] != 1 || statuses["502"] != 1 || spiders["Googlebot"] != 1 || browsers["Chrome"] != 1 || oses["Windows-10"] != 1 {
		t.Fatalf("duration=%#v status=%#v spider=%#v browser=%#v os=%#v", durations, statuses, spiders, browsers, oses)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO website(name,specifications,hosts,proxy_rules,create_time,update_time,proxy_transforms,simple_responses) VALUES('event-retention','{}','[]','[]',clock_timestamp(),clock_timestamp(),'{}','[]')"); err != nil {
		t.Fatal(err)
	}
	var eventID int64
	if err := pool.QueryRow(ctx, "SELECT max(id) FROM gateway_config_event").Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE gateway_config_event SET created_at=$1 WHERE id=$2", time.Now().UTC().Add(-48*time.Hour), eventID); err != nil {
		t.Fatal(err)
	}
	for _, node := range nodeIDs {
		if _, err := pool.Exec(ctx, "UPDATE gateway_node SET applied_revision=$1,last_heartbeat_at=clock_timestamp() WHERE node_id=$2::uuid", eventID, node); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.PruneConfigEvents(ctx, time.Now().UTC(), time.Hour, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM gateway_config_event WHERE id=$1", eventID).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != 0 {
		t.Fatalf("acknowledged expired config event remained: id=%d", eventID)
	}
}

func TestPruneTelemetryRemovesExpiredRowsInBoundedBatches(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	schema := "yaginx_goal_prune" + uuid.NewString()[:10]
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolation, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolation, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolation)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool)
	nodeID := uuid.NewString()
	if _, err := store.RegisterNode(ctx, management.NodeRegistrationRequest{NodeID: nodeID, MachineFingerprint: strings.Repeat("c", 64)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApproveNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	old := now.Add(-400 * 24 * time.Hour)
	fresh := now.Add(-time.Hour)
	for i := 0; i < 3; i++ {
		if _, err := pool.Exec(ctx, "INSERT INTO gateway_traffic_contribution(node_id,process_id,host_name,cycle_type,period,request_qty,duration_sum,duration_max,inbound_bytes,outbound_bytes,status_code,updated_at) VALUES($1::uuid,$2::uuid,$3,10,$4,1,0,0,0,0,'{}'::jsonb,$5)", nodeID, uuid.NewString(), fmt.Sprintf("old-contribution-%d", i), old.Unix()+int64(i), old); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "INSERT INTO resource_monitor_info(resource_uuid,create_ts,data) VALUES($1,$2,'{}'::jsonb)", fmt.Sprintf("old-monitor-%d", i), old.Unix()+int64(i)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "INSERT INTO request_history(id,request_time,request_ts,duration_ms,status_code) VALUES($1,$2,$3,0,200)", int64(i+1), old, old.UnixMilli()+int64(i)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "INSERT INTO host_traffic(host_name,period,request_counts,inbound_bytes,outbound_bytes) VALUES($1,$2,1,0,0)", fmt.Sprintf("old-traffic-%d", i), old.Unix()+int64(i)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "INSERT INTO resource_report(resource_uuid,cycle_type,report_time,report_ts,request_qty,status_code,spider,browser,os,duration,create_time,update_time) VALUES($1,10,$2,$3,1,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,$4,$4)", fmt.Sprintf("old-report-%d", i), old, old.Unix()+int64(i), old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "INSERT INTO gateway_traffic_contribution(node_id,process_id,host_name,cycle_type,period,request_qty,duration_sum,duration_max,inbound_bytes,outbound_bytes,status_code,updated_at) VALUES($1::uuid,$2::uuid,'fresh-contribution',10,$3,1,0,0,0,0,'{}'::jsonb,$4)", nodeID, uuid.NewString(), fresh.Unix(), fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO resource_monitor_info(resource_uuid,create_ts,data) VALUES('fresh-monitor',$1,'{}'::jsonb)", fresh.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO request_history(id,request_time,request_ts,duration_ms,status_code) VALUES(100,$1,$2,0,200)", fresh, fresh.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO host_traffic(host_name,period,request_counts,inbound_bytes,outbound_bytes) VALUES('fresh-traffic',$1,1,0,0)", fresh.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO resource_report(resource_uuid,cycle_type,report_time,report_ts,request_qty,status_code,spider,browser,os,duration,create_time,update_time) VALUES('fresh-report',10,$1,$2,1,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,$1,$1)", fresh, fresh.Unix()); err != nil {
		t.Fatal(err)
	}
	if err := store.PruneTelemetry(ctx, now, 48*time.Hour, 48*time.Hour, 90, 120, 48, 2); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"gateway_traffic_contribution", "resource_monitor_info", "request_history", "host_traffic", "resource_report"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s count=%d, want one retained row", table, count)
		}
	}
}

func TestResetTrafficStatisticsPreservesRequestHistory(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	schema := "yaginx_goal_reset" + uuid.NewString()[:10]
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolation, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolation, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolation)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool)
	nodeID := uuid.NewString()
	if _, err := store.RegisterNode(ctx, management.NodeRegistrationRequest{NodeID: nodeID, MachineFingerprint: strings.Repeat("d", 64)}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO gateway_traffic_contribution(node_id,process_id,host_name,cycle_type,period,request_qty,duration_sum,duration_max,inbound_bytes,outbound_bytes,status_code,updated_at)
		VALUES($1::uuid,$2::uuid,'reset.example.invalid',10,$3,1,1,1,1,1,'{"200":1}'::jsonb,clock_timestamp())`, nodeID, uuid.NewString(), now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO resource_report(resource_uuid,cycle_type,report_time,report_ts,request_qty,status_code,spider,browser,os,duration,create_time,update_time)
		VALUES('reset.example.invalid',10,$1,$2,1,'{"200":1}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,$1,$1)`, now, now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO host_traffic(host_name,period,request_counts,inbound_bytes,outbound_bytes) VALUES('reset.example.invalid',$1,1,1,1)", now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO resource_monitor_info(resource_uuid,create_ts,data) VALUES('reset.example.invalid',$1,'{}'::jsonb)", now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO request_history(id,request_time,request_ts,duration_ms,status_code) VALUES(9001,$1,$2,1,200)", now, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetTrafficStatistics(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"gateway_traffic_contribution", "resource_report", "host_traffic", "resource_monitor_info"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	var history int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM request_history").Scan(&history); err != nil || history != 1 {
		t.Fatalf("request_history=%d err=%v", history, err)
	}
}

func TestPruneReportCyclesUsesIndependentRetentionWindows(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	schema := "yaginx_goal_report_cycles" + uuid.NewString()[:10]
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolation, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolation, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolation)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	insert := func(resource string, cycle int, at time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx, "INSERT INTO resource_report(resource_uuid,cycle_type,report_time,report_ts,request_qty,status_code,spider,browser,os,duration,create_time,update_time) VALUES($1,$2,$3,$4,1,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,$3,$3)", resource, cycle, at, at.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	insert("expired-minute", reportCycleMinutely, now.AddDate(0, 0, -31))
	insert("retained-minute", reportCycleMinutely, now.AddDate(0, 0, -29))
	insert("expired-hour", reportCycleHourly, now.AddDate(0, 0, -366))
	insert("retained-hour", reportCycleHourly, now.AddDate(0, 0, -364))
	insert("retained-daily", reportCycleDaily, now.AddDate(0, 0, -500))
	if err := store.PruneReportCycles(ctx, now, 30, 365, 2); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{"retained-minute", "retained-hour", "retained-daily"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM resource_report WHERE resource_uuid=$1", resource).Scan(&count); err != nil || count != 1 {
			t.Fatalf("retained report %s count=%d err=%v", resource, count, err)
		}
	}
	for _, resource := range []string{"expired-minute", "expired-hour"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM resource_report WHERE resource_uuid=$1", resource).Scan(&count); err != nil || count != 0 {
			t.Fatalf("expired report %s count=%d err=%v", resource, count, err)
		}
	}
}

func TestApplyWebsiteEventsUsesBoundedIncrementalSnapshot(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	schema := "yaginx_goal_incremental" + uuid.NewString()[:10]
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolation, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolation, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolation)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool)
	if _, err := pool.Exec(ctx, "INSERT INTO website(name,specifications,hosts,proxy_rules,create_time,update_time,proxy_transforms,simple_responses) VALUES('incremental-site',$1::jsonb,'[\"incremental.example.invalid\"]','[]',clock_timestamp(),clock_timestamp(),'{}','[]')", `{"defaultDestination":"http://first.example.invalid"}`); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.LoadProxySnapshot(ctx)
	if err != nil || snapshot.Revision == 0 || len(snapshot.Sites) != 1 || snapshot.Sites[0].MaintenanceMode {
		t.Fatalf("initial snapshot=%#v err=%v", snapshot, err)
	}
	initialPool, ok := snapshot.Sites[0].UpstreamPools["legacy-default"]
	if !ok || len(initialPool.Endpoints) != 1 || initialPool.Endpoints[0].Address != "http://first.example.invalid" {
		t.Fatalf("initial legacy default upstream pool=%#v", snapshot.Sites[0].UpstreamPools)
	}
	if len(snapshot.Sites[0].Rules) != 1 || !snapshot.Sites[0].Rules[0].Enabled || !snapshot.Sites[0].Rules[0].IsFallback || snapshot.Sites[0].Rules[0].UpstreamPoolID != "legacy-default" {
		t.Fatalf("initial legacy default fallback rule=%#v", snapshot.Sites[0].Rules)
	}
	if _, err := pool.Exec(ctx, "UPDATE website SET specifications=$1::jsonb,update_time=clock_timestamp() WHERE name='incremental-site'", `{"defaultDestination":"http://second.example.invalid","isMaintenanceMode":true}`); err != nil {
		t.Fatal(err)
	}
	next, incremental, reloadCertificates, err := store.ApplyConfigEvents(ctx, snapshot, 500)
	if err != nil || !incremental || reloadCertificates || next.Revision <= snapshot.Revision || len(next.Sites) != 1 || next.Sites[0].Destination != "" || next.Sites[0].DestinationHost != "" || !next.Sites[0].MaintenanceMode || next.Sites[0].ID == 0 {
		t.Fatalf("website update next=%#v incremental=%t reloadCertificates=%t err=%v", next, incremental, reloadCertificates, err)
	}
	updatedPool, ok := next.Sites[0].UpstreamPools["legacy-default"]
	if !ok || len(updatedPool.Endpoints) != 1 || updatedPool.Endpoints[0].Address != "http://second.example.invalid" {
		t.Fatalf("updated legacy default upstream pool=%#v", next.Sites[0].UpstreamPools)
	}
	if len(next.Sites[0].Rules) != 1 || !next.Sites[0].Rules[0].Enabled || !next.Sites[0].Rules[0].IsFallback || next.Sites[0].Rules[0].UpstreamPoolID != "legacy-default" {
		t.Fatalf("updated legacy default fallback rule=%#v", next.Sites[0].Rules)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM website WHERE name='incremental-site'"); err != nil {
		t.Fatal(err)
	}
	next, incremental, reloadCertificates, err = store.ApplyConfigEvents(ctx, next, 500)
	if err != nil || !incremental || reloadCertificates || len(next.Sites) != 0 {
		t.Fatalf("website delete next=%#v incremental=%t reloadCertificates=%t err=%v", next, incremental, reloadCertificates, err)
	}
	var fallbackEvent int64
	if err := pool.QueryRow(ctx, "INSERT INTO gateway_config_event(resource_type,resource_id,operation) VALUES('gateway_system_setting','test','update') RETURNING id").Scan(&fallbackEvent); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE gateway_config_revision SET revision=$1 WHERE singleton_id=1", fallbackEvent); err != nil {
		t.Fatal(err)
	}
	if candidate, incremental, reloadCertificates, err := store.ApplyConfigEvents(ctx, next, 500); err != nil || !incremental || reloadCertificates || candidate == nil || candidate.Revision != fallbackEvent {
		t.Fatalf("system setting event candidate=%#v incremental=%t reloadCertificates=%t err=%v", candidate, incremental, reloadCertificates, err)
	}
	next, err = store.LoadProxySnapshot(ctx)
	if err != nil || next.Revision != fallbackEvent {
		t.Fatalf("full fallback snapshot=%#v err=%v", next, err)
	}
	var certificateEvent int64
	if err := pool.QueryRow(ctx, "INSERT INTO gateway_config_event(resource_type,resource_id,operation) VALUES('gateway_certificate','1','update') RETURNING id").Scan(&certificateEvent); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE gateway_config_revision SET revision=$1 WHERE singleton_id=1", certificateEvent); err != nil {
		t.Fatal(err)
	}
	next, incremental, reloadCertificates, err = store.ApplyConfigEvents(ctx, next, 500)
	if err != nil || !incremental || !reloadCertificates || next.Revision != certificateEvent {
		t.Fatalf("certificate event next=%#v incremental=%t reloadCertificates=%t err=%v", next, incremental, reloadCertificates, err)
	}
	var domainEvent int64
	if err := pool.QueryRow(ctx, "INSERT INTO gateway_config_event(resource_type,resource_id,operation) VALUES('web_domain','1','update') RETURNING id").Scan(&domainEvent); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE gateway_config_revision SET revision=$1 WHERE singleton_id=1", domainEvent); err != nil {
		t.Fatal(err)
	}
	next, incremental, reloadCertificates, err = store.ApplyConfigEvents(ctx, next, 500)
	if err != nil || !incremental || reloadCertificates || next.Revision != domainEvent {
		t.Fatalf("domain event next=%#v incremental=%t reloadCertificates=%t err=%v", next, incremental, reloadCertificates, err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO website(name,specifications,hosts,proxy_rules,create_time,update_time,proxy_transforms,simple_responses) VALUES('gap-site','{}','[]','[]',clock_timestamp(),clock_timestamp(),'{}','[]')"); err != nil {
		t.Fatal(err)
	}
	var gapEvent int64
	if err := pool.QueryRow(ctx, "SELECT revision FROM gateway_config_revision WHERE singleton_id=1").Scan(&gapEvent); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE gateway_config_event SET created_at=$1 WHERE id=$2", time.Now().UTC().Add(-48*time.Hour), gapEvent); err != nil {
		t.Fatal(err)
	}
	if err := store.PruneConfigEvents(ctx, time.Now().UTC(), time.Hour, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if candidate, incremental, reloadCertificates, err := store.ApplyConfigEvents(ctx, next, 500); err != nil || incremental || reloadCertificates || candidate != nil {
		t.Fatalf("expired event must require full rebuild candidate=%#v incremental=%t reloadCertificates=%t err=%v", candidate, incremental, reloadCertificates, err)
	}
	next, err = store.LoadProxySnapshot(ctx)
	if err != nil || next.Revision != gapEvent {
		t.Fatalf("expired event full fallback snapshot=%#v err=%v", next, err)
	}
	for _, name := range []string{"overflow-one", "overflow-two"} {
		if _, err := pool.Exec(ctx, "INSERT INTO website(name,specifications,hosts,proxy_rules,create_time,update_time,proxy_transforms,simple_responses) VALUES($1,'{}','[]','[]',clock_timestamp(),clock_timestamp(),'{}','[]')", name); err != nil {
			t.Fatal(err)
		}
	}
	if candidate, incremental, reloadCertificates, err := store.ApplyConfigEvents(ctx, next, 1); err != nil || incremental || reloadCertificates || candidate != nil {
		t.Fatalf("batch overflow must require full rebuild candidate=%#v incremental=%t reloadCertificates=%t err=%v", candidate, incremental, reloadCertificates, err)
	}
}

func TestLegacyMonitorBackfillHonorsTrafficCutover(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	schema := "yaginx_goal_backfill" + uuid.NewString()[:10]
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolation, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolation, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolation)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	cutover, err := store.GetOrCreateTrafficCutover(ctx, now)
	if err != nil || !cutover.Equal(now) {
		t.Fatalf("cutover=%s err=%v", cutover, err)
	}
	second, err := store.GetOrCreateTrafficCutover(ctx, now.Add(time.Hour))
	if err != nil || !second.Equal(cutover) {
		t.Fatalf("second cutover=%s first=%s err=%v", second, cutover, err)
	}
	data := `[{"host":"legacy.example.invalid","statusCode":200,"duration":10,"ua":{"family":"Googlebot"},"device":{"isSpider":true},"os":{"family":"Linux","major":"6"}},{"Host":"legacy.example.invalid","StatusCode":502,"Duration":600,"Ua":{"Family":"Chrome"},"Device":{"IsSpider":false},"Os":{"Family":"Windows","Major":"11"}}]`
	if _, err := pool.Exec(ctx, "INSERT INTO resource_monitor_info(resource_uuid,create_ts,data) VALUES('yaginx',$1,$2::jsonb)", cutover.Add(-time.Minute).Unix(), data); err != nil {
		t.Fatal(err)
	}
	// A raw row at the exact boundary belongs to the Go contribution side and
	// must never be folded into the legacy report.
	if _, err := pool.Exec(ctx, "INSERT INTO resource_monitor_info(resource_uuid,create_ts,data) VALUES('yaginx',$1,$2::jsonb)", cutover.Unix(), `[{"host":"new.example.invalid","statusCode":200,"duration":1}]`); err != nil {
		t.Fatal(err)
	}
	if err := store.BackfillLegacyMonitorReports(ctx, cutover); err != nil {
		t.Fatal(err)
	}
	var requests int64
	var status, duration, spider, browser, osValues []byte
	period := cutover.Add(-time.Minute).Truncate(time.Minute).Unix()
	if err := pool.QueryRow(ctx, "SELECT request_qty,status_code,duration,spider,browser,os FROM resource_report WHERE resource_uuid='legacy.example.invalid' AND cycle_type=5 AND report_ts=$1", period).Scan(&requests, &status, &duration, &spider, &browser, &osValues); err != nil {
		t.Fatal(err)
	}
	var statuses, durations, spiders, browsers, oses map[string]int64
	if err := json.Unmarshal(status, &statuses); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(duration, &durations); err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		raw  []byte
		into *map[string]int64
	}{{spider, &spiders}, {browser, &browsers}, {osValues, &oses}} {
		if err := json.Unmarshal(pair.raw, pair.into); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 2 || statuses["200"] != 1 || statuses["502"] != 1 || durations["avg"] != 305 || durations["max"] != 600 || durations["<=100"] != 1 || durations[">500 & <=1000"] != 1 || spiders["Googlebot"] != 1 || spiders["total"] != 1 || browsers["Chrome"] != 1 || browsers["total"] != 1 || oses["Linux-6"] != 1 || oses["Windows-11"] != 1 {
		t.Fatalf("requests=%d statuses=%#v durations=%#v spiders=%#v browsers=%#v os=%#v", requests, statuses, durations, spiders, browsers, oses)
	}
	var unexpected int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM resource_report WHERE resource_uuid='new.example.invalid'").Scan(&unexpected); err != nil {
		t.Fatal(err)
	}
	if unexpected != 0 {
		t.Fatalf("post-cutover raw report count=%d", unexpected)
	}
}
