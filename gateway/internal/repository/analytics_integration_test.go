//go:build integration

package repository

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/management"
)

func TestAnalyticsReadsDurableDomainReports(t *testing.T) {
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
	schema := "yaginx_goal_analytics" + uuid.NewString()[:10]
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
	if _, err := store.RegisterNode(ctx, management.NodeRegistrationRequest{NodeID: nodeID, MachineFingerprint: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Minute)
	insert := func(domain string, requests, inbound, outbound int64, status, errors, histogram string) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO gateway_traffic_contribution(node_id,process_id,host_name,cycle_type,period,request_qty,duration_sum,duration_max,inbound_bytes,outbound_bytes,status_code,spider,browser,os,duration_buckets,error_counts,latency_histogram,is_final,updated_at)
			VALUES($1::uuid,$2::uuid,$3,5,$4,$5,$6,50,$7,$8,$9::jsonb,'{}'::jsonb,jsonb_build_object('Browser',$5::bigint),jsonb_build_object('Linux',$5::bigint),'{"<=100":1}'::jsonb,$10::jsonb,$11::jsonb,true,clock_timestamp())`,
			nodeID, uuid.NewString(), domain, at.Unix(), requests, requests*20, inbound, outbound, status, errors, histogram)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("a.example", 3, 30, 60, `{"200":2,"502":1}`, `{"upstream:server_error":1}`, `{"<=10":2,"<=50":1}`)
	insert("b.example", 1, 10, 20, `{"200":1}`, `{}`, `{"<=100":1}`)
	input := management.AnalyticsQuery{From: at.Add(-time.Minute).Unix(), To: at.Unix()}
	overview, err := store.AnalyticsSummary(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Source != analyticsSource || !overview.HasData || overview.Kpis.Requests != 4 || overview.Kpis.InboundBytes != 40 || overview.Kpis.OutboundBytes != 80 || overview.Kpis.ErrorRequests != 1 || overview.Kpis.P95Ms == nil || *overview.Kpis.P95Ms != 100 {
		t.Fatalf("overview=%#v", overview)
	}
	domainsResult, err := store.AnalyticsDomains(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	domains := domainsResult.Domains
	if len(domains) != 2 || domains[0].Domain != "a.example" || domains[0].Requests != 3 {
		t.Fatalf("domains=%#v", domains)
	}
	series, err := store.AnalyticsTimeseries(ctx, management.AnalyticsQuery{From: at.Add(-time.Minute).Unix(), To: at.Unix(), Domain: "a.example"})
	if err != nil {
		t.Fatal(err)
	}
	points := series.Points
	if len(points) != 2 || points[1].Requests != 3 || points[1].ErrorRequests != 1 {
		t.Fatalf("points=%#v", points)
	}
	contributions, err := store.AnalyticsContributions(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if summary := contributions.Summary; summary.TotalRequests != 4 || summary.ActiveHostCount != 2 || summary.TopHostName != "a.example" {
		t.Fatalf("contributions=%#v", contributions)
	}
	// A second process contributes to the same Host and minute. The details
	// endpoint must group both source rows before applying its page limit.
	insert("a.example", 2, 20, 40, `{"200":1,"500":1}`, `{}`, `{"<=10":1,"<=100":1}`)
	if _, err := pool.Exec(ctx, `INSERT INTO gateway_traffic_contribution(node_id,process_id,host_name,cycle_type,period,request_qty,duration_sum,duration_max,inbound_bytes,outbound_bytes,status_code,latency_histogram,is_final,updated_at)
		SELECT $1::uuid,$2::uuid,'bulk-'||hosts.host_no::text||'.example.invalid',5,$3::bigint-buckets.minute_offset*60,1,10,10,1,2,'{"200":1}'::jsonb,'{"<=10":1}'::jsonb,true,clock_timestamp()
		FROM generate_series(0,49) AS hosts(host_no) CROSS JOIN generate_series(0,1440) AS buckets(minute_offset)`, nodeID, uuid.NewString(), at.Unix()); err != nil {
		t.Fatal(err)
	}
	detailInput := management.AnalyticsQuery{From: at.Add(-24 * time.Hour).Unix(), To: at.Unix()}
	details, err := store.AnalyticsDetails(ctx, management.AnalyticsPagedQuery{AnalyticsQuery: detailInput, PageRequest: management.PageRequest{PageIndex: 1, PageSize: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if paging := details.Paging; paging.Total != 72052 || paging.PageSize != 1 {
		t.Fatalf("details paging=%#v", paging)
	}
	if records := details.Records; len(records) != 1 || records[0].HostName != "a.example" || records[0].RequestCounts != 5 || records[0].ErrorRequests != 2 || records[0].ErrorRate != 0.4 || records[0].P95Ms == nil || *records[0].P95Ms != 100 {
		t.Fatalf("details records=%#v", records)
	}
	if details.Coverage.State != "complete" || details.Coverage.MissingBuckets != 0 {
		t.Fatalf("large-window coverage=%#v", details.Coverage)
	}
	emptyDetails, err := store.AnalyticsDetails(ctx, management.AnalyticsPagedQuery{
		AnalyticsQuery: management.AnalyticsQuery{From: detailInput.From, To: detailInput.To, Domain: "missing.example.invalid"},
		PageRequest:    management.PageRequest{PageIndex: 1, PageSize: 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	if emptyDetails.Paging.Total != 0 || emptyDetails.Paging.PageIndex != 1 || len(emptyDetails.Records) != 0 || emptyDetails.Coverage.State != "collecting" {
		t.Fatalf("empty-window details=%#v", emptyDetails)
	}
	var observerCalls int
	store.SetObservabilityObserver(func(operation, source string, rows int, loadDuration, buildDuration time.Duration) {
		observerCalls++
		if operation != "dashboard" || source != analyticsSource || rows != 2 || loadDuration <= 0 || buildDuration < 0 {
			t.Fatalf("observer operation=%q source=%q rows=%d load=%s build=%s", operation, source, rows, loadDuration, buildDuration)
		}
	})
	filteredInput := management.AnalyticsQuery{From: at.Add(-time.Minute).Unix(), To: at.Unix(), Domain: "a.example"}
	dashboard, err := store.AnalyticsOverview(ctx, management.AnalyticsOverviewQuery{AnalyticsQuery: filteredInput, Sections: []string{"overview", "domains", "timeseries"}})
	if err != nil {
		t.Fatal(err)
	}
	if observerCalls != 1 {
		t.Fatalf("dashboard should load once; observer calls=%d", observerCalls)
	}
	store.SetObservabilityObserver(nil)
	store.SetObservabilityObserver(func(operation, source string, rows int, loadDuration, buildDuration time.Duration) {
		observerCalls++
		if operation != "details" || source != analyticsSource || rows != 1 || loadDuration <= 0 || buildDuration < 0 {
			t.Fatalf("details observer operation=%q source=%q rows=%d load=%s build=%s", operation, source, rows, loadDuration, buildDuration)
		}
	})
	if _, err := store.AnalyticsDetails(ctx, management.AnalyticsPagedQuery{AnalyticsQuery: detailInput, PageRequest: management.PageRequest{PageIndex: 1, PageSize: 1}}); err != nil {
		t.Fatal(err)
	}
	if observerCalls != 2 {
		t.Fatalf("details should be observed once; observer calls=%d", observerCalls-1)
	}
	store.SetObservabilityObserver(nil)
	filteredOverview, err := store.AnalyticsSummary(ctx, filteredInput)
	if err != nil {
		t.Fatal(err)
	}
	filteredDomains, err := store.AnalyticsDomains(ctx, filteredInput)
	if err != nil {
		t.Fatal(err)
	}
	filteredSeries, err := store.AnalyticsTimeseries(ctx, filteredInput)
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Overview != nil {
		// These values are sampled from wall clock time by separate queries and
		// can legitimately differ by a second between the dashboard and the
		// direct summary request.
		dashboardOverview := *dashboard.Overview
		dashboardOverview.AsOf = 0
		dashboardOverview.DataLagSeconds = 0
		filteredOverview.AsOf = 0
		filteredOverview.DataLagSeconds = 0
		if !reflect.DeepEqual(dashboardOverview, filteredOverview) {
			t.Fatalf("dashboard overview=%#v want=%#v", dashboard.Overview, filteredOverview)
		}
	} else {
		t.Fatalf("dashboard overview=%#v want=%#v", dashboard.Overview, filteredOverview)
	}
	if got, want := dashboard.Domains, filteredDomains.Domains; !reflect.DeepEqual(got, want) {
		t.Fatalf("dashboard domains=%#v want=%#v", got, want)
	}
	if got, want := dashboard.Points, filteredSeries.Points; !reflect.DeepEqual(got, want) {
		t.Fatalf("dashboard points=%#v want=%#v", got, want)
	}
}

func TestAnalyticsCombinesColdReportsAndHotContributionsWithoutUAFalsePositives(t *testing.T) {
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
	schema := "yaginx_goal_analytics_hot_cold" + uuid.NewString()[:10]
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
	store.SetContributionRetention(2 * time.Hour)
	nodeID := uuid.NewString()
	if _, err := store.RegisterNode(ctx, management.NodeRegistrationRequest{NodeID: nodeID, MachineFingerprint: strings.Repeat("b", 64)}); err != nil {
		t.Fatal(err)
	}
	now, err := store.databaseNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cutover := now.Add(-2 * time.Hour).UTC().Truncate(time.Minute)
	coldAt := cutover.Add(-10 * time.Minute)
	hotAt := cutover.Add(10 * time.Minute)
	if _, err := pool.Exec(ctx, `INSERT INTO resource_report(resource_uuid,cycle_type,report_time,report_ts,request_qty,inbound_bytes,outbound_bytes,status_code,spider,browser,os,duration,error_counts,latency_histogram,is_partial,create_time,update_time)
		VALUES('split.example.invalid',5,$1,$2,2,20,30,'{"200":2}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{"<=100":2,"avg":50,"max":50}'::jsonb,'{}'::jsonb,'{"<=100":2}'::jsonb,false,$1,$1)`,
		coldAt, coldAt.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO gateway_traffic_contribution(node_id,process_id,host_name,cycle_type,period,request_qty,duration_sum,duration_max,inbound_bytes,outbound_bytes,status_code,spider,browser,os,duration_buckets,error_counts,latency_histogram,is_final,updated_at)
		VALUES($1::uuid,$2::uuid,'split.example.invalid',5,$3,3,180,80,40,50,'{"200":3}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'{"<=100":3}'::jsonb,'{}'::jsonb,'{"<=100":3}'::jsonb,true,clock_timestamp())`,
		nodeID, uuid.NewString(), hotAt.Unix()); err != nil {
		t.Fatal(err)
	}
	input := management.AnalyticsQuery{From: coldAt.Unix(), To: hotAt.Unix(), Domain: "split.example.invalid"}
	overview, err := store.AnalyticsSummary(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Source != analyticsMixedSource || overview.Kpis.Requests != 5 {
		t.Fatalf("overview=%#v", overview)
	}
	series, err := store.AnalyticsTimeseries(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	points := series.Points
	if points[0].Requests != 2 || points[len(points)-1].Requests != 3 {
		t.Fatalf("timeseries=%#v", points)
	}
	detailInput := management.AnalyticsQuery{From: coldAt.Add(-24 * time.Hour).Unix(), To: hotAt.Unix()}
	details, err := store.AnalyticsDetails(ctx, management.AnalyticsPagedQuery{AnalyticsQuery: detailInput, PageRequest: management.PageRequest{PageIndex: 99, PageSize: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if details.Source != analyticsMixedSource || details.Paging.Total != 2 || details.Paging.PageIndex != 2 || len(details.Records) != 1 || details.Records[0].Period != coldAt.Unix() || details.Records[0].RequestCounts != 2 {
		t.Fatalf("mixed-source details=%#v", details)
	}
	detailSummary, err := store.AnalyticsSummary(ctx, detailInput)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(details.Coverage, detailSummary.Coverage) {
		t.Fatalf("details coverage=%#v want overview coverage=%#v", details.Coverage, detailSummary.Coverage)
	}
	disabledHealth, err := store.TrafficStatisticsHealth(ctx, coldAt, coldAt.Add(time.Minute), false)
	if err != nil {
		t.Fatal(err)
	}
	if disabledHealth["ua_incomplete"] != 0 {
		t.Fatalf("disabled UA health=%#v", disabledHealth)
	}
	enabledHealth, err := store.TrafficStatisticsHealth(ctx, coldAt, coldAt.Add(time.Minute), true)
	if err != nil {
		t.Fatal(err)
	}
	if enabledHealth["ua_incomplete"] != 1 {
		t.Fatalf("enabled UA health=%#v", enabledHealth)
	}
}
