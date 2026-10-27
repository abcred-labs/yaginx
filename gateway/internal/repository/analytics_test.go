package repository

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"yaginx-gateway/internal/management"
)

func TestAnalyticsDashboardBuildsOnlyRequestedSectionsForEmptyData(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	snapshot := analyticsSnapshot{
		now: now,
		window: analyticsWindowValue{
			begin: now.Add(-time.Hour),
			end:   now,
			grain: "minute",
			step:  time.Minute,
		},
		rows:     []analyticsRow{},
		source:   analyticsSource,
		coverage: analyticsCoverage(analyticsWindowValue{begin: now.Add(-time.Hour), end: now, grain: "minute", step: time.Minute}, nil),
	}
	result := management.AnalyticsOverview{AnalyticsBase: snapshot.baseDTO()}
	for _, section := range []string{"overview", "timeseries"} {
		switch section {
		case "overview":
			overview := snapshot.overviewDTO()
			result.Overview = &overview
		case "timeseries":
			result.Points = denseAnalyticsPointsTyped(snapshot.rows, snapshot.window)
		}
	}
	if result.Source != analyticsSource || result.Overview == nil || result.Overview.HasData {
		t.Fatalf("dashboard=%#v", result)
	}
	if result.Domains != nil {
		t.Fatalf("unrequested domains=%#v", result)
	}
	if points := result.Points; len(points) != 61 {
		t.Fatalf("points=%#v", points)
	}
}

func TestObserveAnalyticsLoadReportsRealLoadAndBuildDurations(t *testing.T) {
	var gotLoad, gotBuild time.Duration
	store := &Postgres{analyticsObserver: func(operation, source string, rows int, loadDuration, buildDuration time.Duration) {
		if operation != "overview" || source != analyticsSource || rows != 1 {
			t.Fatalf("operation=%q source=%q rows=%d", operation, source, rows)
		}
		gotLoad, gotBuild = loadDuration, buildDuration
	}}
	loaded := time.Now().Add(-time.Millisecond)
	store.observeAnalyticsLoad("overview", analyticsSnapshot{source: analyticsSource, rows: []analyticsRow{{}}}, loaded.Add(-2*time.Millisecond), loaded)
	if gotLoad <= 0 || gotBuild <= 0 {
		t.Fatalf("load=%s build=%s", gotLoad, gotBuild)
	}
}

func TestEstimateQuantileUsesMergeableFixedHistogram(t *testing.T) {
	histogram := map[string]int64{"<=10": 40, "<=100": 50, "<=500": 10}
	if value := estimateQuantile(histogram, .50); value != 100 {
		t.Fatalf("p50=%d", value)
	}
	if value := estimateQuantile(histogram, .95); value != 500 {
		t.Fatalf("p95=%d", value)
	}
}

func TestAnalyticsWindowUsesSafeAutomaticGrains(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	window, err := analyticsWindowForRange(now, now.Add(-72*time.Hour).Unix(), now.Unix())
	if err != nil || window.grain != "hour" || window.cycle != reportCycleHourly {
		t.Fatalf("window=%#v err=%v", window, err)
	}
	if _, err := analyticsWindowForRange(now, now.Unix(), now.Unix()); err == nil {
		t.Fatal("expected invalid equal bounds")
	}
}

func TestAnalyticsContributionQueryPinsCycleParametersAsIntegers(t *testing.T) {
	if !strings.Contains(analyticsContributionRowsSQL, "$1::integer=$2::integer") {
		t.Fatalf("contribution query must type cycle parameters: %s", analyticsContributionRowsSQL)
	}
}

func TestAnalyticsFallbackQueryRequiresCompleteReportProjection(t *testing.T) {
	for _, query := range []string{analyticsFallbackContributionRowsSQL, analyticsDetailsPageSQL} {
		if !strings.Contains(query, "NOT r.is_partial") || !strings.Contains(query, "gateway_traffic_contribution") {
			t.Fatalf("fallback query does not guard complete report projections: %s", query)
		}
	}
}

func TestStatusFamilyCountsConservesRequestsAndClassifiesOther(t *testing.T) {
	families := statusFamilyCounts(map[string]int64{"200": 2, "302": 3, "404": 4, "503": 5, "604": 6, "101": 7, "invalid": 8}, 40)
	if families["1xx"] != 7 || families["2xx"] != 2 || families["3xx"] != 3 || families["4xx"] != 4 || families["5xx"] != 5 || families["6xx"] != 6 || families["other"] != 13 {
		t.Fatalf("families=%#v", families)
	}
	if got := sumCounts(families); got != 40 {
		t.Fatalf("status families=%d, want requests=40", got)
	}
}

func TestAnalyticsKpisHidesPercentilesWithoutFullHistogramCoverage(t *testing.T) {
	partial := analyticsMetrics{requests: 10, histogram: map[string]int64{"<=100": 9}}
	if kpis := analyticsKpis(partial, time.Minute); kpis.P95Ms != nil || kpis.LatencyCoverageRate != .9 {
		t.Fatalf("partial kpis=%#v", kpis)
	}
	full := analyticsMetrics{requests: 10, histogram: map[string]int64{"<=100": 10}}
	if kpis := analyticsKpis(full, time.Minute); kpis.P95Ms == nil || *kpis.P95Ms != 100 || kpis.LatencyCoverageRate != 1 {
		t.Fatalf("full kpis=%#v", kpis)
	}
}

func TestAnalyticsModeCoverageUsesRequestsAsDenominator(t *testing.T) {
	m := analyticsMetrics{responseModeMetrics: map[string]map[string]int64{
		"stream": {"requests": 10, "ttfb_count": 1, "ttfb_sum": 20, "ttfb_hist:<=25": 1},
	}}
	mode := analyticsModeMetrics(m, "stream")
	if mode.FirstByteCoverageRate != .1 {
		t.Fatalf("first byte coverage=%v, want 0.1", mode.FirstByteCoverageRate)
	}
}

func TestAnalyticsPublicBreakdownsExcludeModeMetadata(t *testing.T) {
	families := statusFamilyCounts(map[string]int64{"200": 2, "__response_mode:stream": 2}, 2)
	if families["2xx"] != 2 || families["other"] != 0 {
		t.Fatalf("families=%#v", families)
	}
	buckets := publicDurationBuckets(map[string]int64{"<=100": 2, "__response_mode:stream:duration_sum": 50})
	if len(buckets) != 1 || buckets["<=100"] != 2 {
		t.Fatalf("buckets=%#v", buckets)
	}
}

func TestAnalyticsCoverageTreatsFinalReportBackedBucketsAsComplete(t *testing.T) {
	begin := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	window := analyticsWindowValue{begin: begin, end: begin.Add(time.Hour), step: time.Hour}
	coverage := analyticsCoverage(window, []analyticsRow{
		{timestamp: begin.Unix(), metrics: analyticsMetrics{}},
		{timestamp: begin.Add(time.Hour).Unix(), metrics: analyticsMetrics{}},
	})
	if coverage.State != "complete" || !coverage.IsComplete || coverage.MissingBuckets != 0 {
		t.Fatalf("coverage=%#v", coverage)
	}
}

func TestAnalyticsWindowKeepsShortRangesMinuteGranular(t *testing.T) {
	now := time.Date(2026, 8, 4, 19, 0, 30, 0, time.UTC)
	window, err := analyticsWindowForRange(now, now.Add(-59*time.Minute).Unix(), now.Truncate(time.Minute).Unix())
	if err != nil || window.grain != "minute" || window.step != time.Minute {
		t.Fatalf("window=%#v err=%v", window, err)
	}
	if got := int(window.end.Sub(window.begin)/window.step) + 1; got != 60 {
		t.Fatalf("minute bucket count=%d", got)
	}
}

func TestAnalyticsContributionsGroupsTopHostsAndOthers(t *testing.T) {
	begin := time.Date(2026, 8, 4, 19, 0, 0, 0, time.UTC)
	window := analyticsWindowValue{begin: begin, end: begin.Add(time.Minute), step: time.Minute}
	rows := []analyticsRow{
		{domain: "a.example", timestamp: begin.Unix(), metrics: analyticsMetrics{requests: 5}},
		{domain: "b.example", timestamp: begin.Unix(), metrics: analyticsMetrics{requests: 3}},
		{domain: "c.example", timestamp: begin.Unix(), metrics: analyticsMetrics{requests: 2}},
		{domain: "d.example", timestamp: begin.Unix(), metrics: analyticsMetrics{requests: 1}},
		{domain: "e.example", timestamp: begin.Unix(), metrics: analyticsMetrics{requests: 1}},
		{domain: "f.example", timestamp: begin.Unix(), metrics: analyticsMetrics{requests: 4, partial: true}},
	}
	summary, rankings, timeline := analyticsContributionsTyped(rows, window)
	if summary.TotalRequests != 16 || summary.ActiveHostCount != 6 || summary.TopHostName != "a.example" {
		t.Fatalf("summary=%#v", summary)
	}
	if len(rankings) != 6 || rankings[0].HostName != "a.example" || rankings[1].HostName != "f.example" {
		t.Fatalf("rankings=%#v", rankings)
	}
	if len(timeline) != 12 {
		t.Fatalf("timeline=%#v", timeline)
	}
	var other *management.AnalyticsContributionPoint
	for _, point := range timeline {
		if point.Period == begin.Unix() && point.HostName == "其他" {
			other = &point
			break
		}
	}
	if other == nil || other.RequestCounts != 1 || other.IsPartial {
		t.Fatalf("other point=%#v", other)
	}
}

func TestAnalyticsDetailsPaginatesByPeriodThenHost(t *testing.T) {
	begin := time.Date(2026, 8, 4, 19, 0, 0, 0, time.UTC)
	window := analyticsWindowValue{begin: begin, end: begin.Add(time.Minute), step: time.Minute}
	rows := []analyticsRow{
		{domain: "b.example", timestamp: begin.Unix(), metrics: analyticsMetrics{requests: 1, inbound: 10, outbound: 20}},
		{domain: "a.example", timestamp: begin.Unix(), metrics: analyticsMetrics{requests: 2}},
		{domain: "c.example", timestamp: begin.Add(time.Minute).Unix(), metrics: analyticsMetrics{requests: 3, partial: true}},
	}
	records, paging := analyticsDetailsTyped(rows, window, 1, 2)
	if paging.Total != 3 || len(records) != 2 || records[0].HostName != "c.example" || records[1].HostName != "a.example" {
		t.Fatalf("records=%#v paging=%#v", records, paging)
	}
	if !records[0].IsPartial || records[1].TotalBytes != 0 {
		t.Fatalf("record data=%#v", records)
	}
	records, paging = analyticsDetailsTyped(rows, window, 9, 2)
	if paging.PageIndex != 2 || len(records) != 1 || records[0].HostName != "b.example" || records[0].TotalBytes != 30 {
		t.Fatalf("last page records=%#v paging=%#v", records, paging)
	}
	_, paging = analyticsDetailsTyped(rows, window, 1, 201)
	if paging.PageSize != management.DefaultPageSize {
		t.Fatalf("pageSize=%d, want %d for invalid direct helper input", paging.PageSize, management.DefaultPageSize)
	}
}

func TestAuditRedactionRemovesSensitiveHeadersQueryAndJSON(t *testing.T) {
	headers := headerMap(map[string][]string{"Authorization": {"Bearer secret"}, "X-Trace": {"safe"}})
	if headers["Authorization"][0] != "[REDACTED]" || headers["X-Trace"][0] != "safe" {
		t.Fatalf("headers=%#v", headers)
	}
	if query := redactQuery("safe=one&api_key=secret"); query != "api_key=%5BREDACTED%5D&safe=one" {
		t.Fatalf("query=%q", query)
	}
	body := base64.StdEncoding.EncodeToString([]byte(`{"nested":{"password":"secret"},"safe":"one"}`))
	decoded, err := base64.StdEncoding.DecodeString(redactJSONBody(body, "application/json"))
	if err != nil || string(decoded) != `{"nested":{"password":"[REDACTED]"},"safe":"one"}` {
		t.Fatalf("body=%q err=%v", decoded, err)
	}
}
