package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"yaginx-gateway/internal/management"
)

const (
	analyticsSource       = "gateway_traffic_contribution"
	analyticsReportSource = "resource_report"
	analyticsMixedSource  = "gateway_traffic_contribution+resource_report"
)

const analyticsContributionRowsSQL = `SELECT host_name,
	CASE WHEN $1::integer=$2::integer THEN (period / 86400) * 86400 ELSE period END AS report_ts,
	request_qty,inbound_bytes,outbound_bytes,duration_sum,duration_max,
	status_code,spider,browser,os,duration_buckets,error_counts,latency_histogram,response_mode_metrics,NOT is_final
	FROM gateway_traffic_contribution
	WHERE cycle_type=$3 AND period >= $4 AND period <= $5 AND (cardinality($6::text[])=0 OR host_name = ANY($6))
	ORDER BY report_ts,host_name`

type analyticsSnapshot struct {
	now      time.Time
	window   analyticsWindowValue
	domain   string
	rows     []analyticsRow
	source   string
	coverage management.AnalyticsCoverage
}

func (p *Postgres) loadAnalyticsQuery(ctx context.Context, input management.AnalyticsQuery) (analyticsSnapshot, time.Time, error) {
	domains := input.Domains
	if len(domains) == 0 {
		if domain := strings.TrimSpace(input.Domain); domain != "" {
			domains = []string{domain}
		}
	}
	now, err := p.databaseNow(ctx)
	if err != nil {
		return analyticsSnapshot{}, time.Time{}, err
	}
	window, err := analyticsWindowForRange(now, input.From, input.To)
	if err != nil {
		return analyticsSnapshot{}, time.Time{}, err
	}
	rows, source, err := p.analyticsRows(ctx, now, window, domains)
	if err != nil {
		return analyticsSnapshot{}, time.Time{}, err
	}
	snapshotDomain := ""
	if len(domains) == 1 {
		snapshotDomain = domains[0]
	}
	return analyticsSnapshot{now: now, window: window, domain: snapshotDomain, rows: rows, source: source, coverage: analyticsCoverage(window, rows)}, time.Now(), nil
}

func (value analyticsSnapshot) baseDTO() management.AnalyticsBase {
	return management.AnalyticsBase{AsOf: value.now.Unix(), From: value.window.begin.Unix(), To: value.window.end.Unix(), Grain: value.window.grain, Source: value.source, Coverage: value.coverage}
}

func (p *Postgres) AnalyticsSummary(ctx context.Context, input management.AnalyticsQuery) (management.AnalyticsSummary, error) {
	started := time.Now()
	snapshot, loaded, err := p.loadAnalyticsQuery(ctx, input)
	if err != nil {
		return management.AnalyticsSummary{}, err
	}
	result := snapshot.overviewDTO()
	p.observeAnalyticsLoad("overview", snapshot, started, loaded)
	return result, nil
}

func (p *Postgres) AnalyticsDomains(ctx context.Context, input management.AnalyticsQuery) (management.AnalyticsDomains, error) {
	started := time.Now()
	snapshot, loaded, err := p.loadAnalyticsQuery(ctx, input)
	if err != nil {
		return management.AnalyticsDomains{}, err
	}
	result := management.AnalyticsDomains{AnalyticsBase: snapshot.baseDTO(), Domains: topDomainsTyped(snapshot.rows, snapshot.window.end.Sub(snapshot.window.begin), 200)}
	p.observeAnalyticsLoad("domains", snapshot, started, loaded)
	return result, nil
}

func (p *Postgres) AnalyticsTimeseries(ctx context.Context, input management.AnalyticsQuery) (management.AnalyticsTimeseries, error) {
	started := time.Now()
	snapshot, loaded, err := p.loadAnalyticsQuery(ctx, input)
	if err != nil {
		return management.AnalyticsTimeseries{}, err
	}
	result := management.AnalyticsTimeseries{AnalyticsBase: snapshot.baseDTO(), Domain: snapshot.domain, Points: denseAnalyticsPointsTyped(snapshot.rows, snapshot.window)}
	p.observeAnalyticsLoad("timeseries", snapshot, started, loaded)
	return result, nil
}

func (p *Postgres) AnalyticsContributions(ctx context.Context, input management.AnalyticsQuery) (management.AnalyticsContributions, error) {
	started := time.Now()
	snapshot, loaded, err := p.loadAnalyticsQuery(ctx, input)
	if err != nil {
		return management.AnalyticsContributions{}, err
	}
	summary, rankings, timeline := analyticsContributionsTyped(snapshot.rows, snapshot.window)
	result := management.AnalyticsContributions{AnalyticsBase: snapshot.baseDTO(), Summary: summary, Rankings: rankings, Timeline: timeline}
	p.observeAnalyticsLoad("contributions", snapshot, started, loaded)
	return result, nil
}

func (p *Postgres) AnalyticsDetails(ctx context.Context, input management.AnalyticsPagedQuery) (management.AnalyticsDetails, error) {
	started := time.Now()
	now, err := p.databaseNow(ctx)
	if err != nil {
		return management.AnalyticsDetails{}, err
	}
	window, err := analyticsWindowForRange(now, input.From, input.To)
	if err != nil {
		return management.AnalyticsDetails{}, err
	}
	pageRequest, err := management.NormalizePageRequest(input.PageRequest)
	if err != nil {
		return management.AnalyticsDetails{}, err
	}
	domains := input.Domains
	if len(domains) == 0 {
		if domain := strings.TrimSpace(input.Domain); domain != "" {
			domains = []string{domain}
		}
	}
	if domains == nil {
		domains = []string{}
	}
	if window.end.Sub(window.begin) < 24*time.Hour {
		rows, source, err := p.analyticsRows(ctx, now, window, domains)
		if err != nil {
			return management.AnalyticsDetails{}, err
		}
		snapshot := analyticsSnapshot{now: now, window: window, source: source, coverage: analyticsCoverage(window, rows)}
		if len(domains) == 1 {
			snapshot.domain = domains[0]
		}
		records, paging := analyticsDetailsTyped(rows, window, pageRequest.PageIndex, pageRequest.PageSize)
		result := management.AnalyticsDetails{AnalyticsBase: snapshot.baseDTO(), Records: records, Paging: paging}
		p.observeAnalyticsLoad("details", snapshot, started, time.Now())
		return result, nil
	}

	cutover := now.Add(-p.contributionRetention).UTC().Truncate(window.step)
	var reportFrom, reportTo, contributionFrom, contributionTo int64
	includeReports, includeContributions := false, false
	source := analyticsSource
	switch {
	case !cutover.After(window.begin):
		includeContributions = true
		contributionFrom, contributionTo = window.begin.Unix(), window.end.Unix()
	case cutover.After(window.end):
		includeReports = true
		reportFrom, reportTo = window.begin.Unix(), window.end.Unix()
		source = analyticsReportSource
	default:
		includeReports, includeContributions = true, true
		reportFrom, reportTo = window.begin.Unix(), cutover.Add(-window.step).Unix()
		contributionFrom, contributionTo = cutover.Unix(), window.end.Unix()
		source = analyticsMixedSource
	}
	contributionCycle := window.cycle
	if contributionCycle == reportCycleDaily {
		contributionCycle = reportCycleHourly
	}

	var pageIndex, pageSize int
	var total int64
	var firstBucket, lastFinalBucket int64
	var finalizedBuckets int64
	var rawRecords []byte
	err = p.pool.QueryRow(ctx, analyticsDetailsPageSQL,
		window.cycle, reportCycleDaily, contributionCycle,
		includeReports, reportFrom, reportTo,
		includeContributions, contributionFrom, contributionTo,
		domains, pageRequest.PageIndex, pageRequest.PageSize,
	).Scan(&pageIndex, &pageSize, &total, &firstBucket, &lastFinalBucket, &finalizedBuckets, &rawRecords)
	if err != nil {
		return management.AnalyticsDetails{}, err
	}
	loaded := time.Now()
	var pageRows []analyticsDetailPageRow
	if err := json.Unmarshal(rawRecords, &pageRows); err != nil {
		return management.AnalyticsDetails{}, fmt.Errorf("decode analytics detail page: %w", err)
	}
	windowBuckets := int(window.end.Sub(window.begin)/window.step) + 1
	missingBuckets := max(windowBuckets-int(finalizedBuckets), 0)
	coverage := management.AnalyticsCoverage{
		From: firstBucket, IsComplete: firstBucket > 0 && missingBuckets == 0,
		MissingBuckets: missingBuckets, WindowBuckets: windowBuckets, State: "collecting",
	}
	if lastFinalBucket > 0 {
		coverage.To = lastFinalBucket + int64(window.step.Seconds())
	}
	if coverage.IsComplete {
		coverage.State = "complete"
	} else if firstBucket > 0 {
		coverage.State = "incomplete"
	}
	snapshotDomain := ""
	if len(domains) == 1 {
		snapshotDomain = domains[0]
	}
	snapshot := analyticsSnapshot{now: now, window: window, domain: snapshotDomain, source: source, coverage: coverage}
	records := make([]management.AnalyticsDetail, 0, len(pageRows))
	for _, row := range pageRows {
		metrics := analyticsMetrics{
			requests: row.RequestCounts, inbound: row.InboundBytes, outbound: row.OutboundBytes,
			durationSum: row.DurationSum, statuses: row.StatusCodes, histogram: row.LatencyHistogram, partial: row.IsPartial,
		}
		kpis := analyticsKpis(metrics, window.step)
		records = append(records, management.AnalyticsDetail{
			ID: fmt.Sprintf("%d:%s", row.Period, row.HostName), HostName: row.HostName, Period: row.Period,
			RequestCounts: row.RequestCounts, InboundBytes: row.InboundBytes, OutboundBytes: row.OutboundBytes,
			TotalBytes: row.InboundBytes + row.OutboundBytes, ErrorRequests: kpis.ErrorRequests,
			ErrorRate: kpis.ErrorRate, AvgDurationMs: kpis.AvgDurationMs, P95Ms: kpis.P95Ms, IsPartial: row.IsPartial,
		})
	}
	result := management.AnalyticsDetails{
		AnalyticsBase: snapshot.baseDTO(), Records: records,
		Paging: management.PageCursor{PageIndex: pageIndex, PageSize: pageSize, Total: total},
	}
	if p.analyticsObserver != nil {
		p.analyticsObserver("details", source, len(pageRows), loaded.Sub(started), time.Since(loaded))
	}
	return result, nil
}

type analyticsDetailPageRow struct {
	HostName         string           `json:"hostName"`
	Period           int64            `json:"period"`
	RequestCounts    int64            `json:"requestCounts"`
	InboundBytes     int64            `json:"inboundBytes"`
	OutboundBytes    int64            `json:"outboundBytes"`
	DurationSum      int64            `json:"durationSum"`
	StatusCodes      map[string]int64 `json:"statusCodes"`
	LatencyHistogram map[string]int64 `json:"latencyHistogram"`
	IsPartial        bool             `json:"isPartial"`
}

// analyticsDetailsPageSQL keeps the potentially large time-window aggregation
// in PostgreSQL. Only the requested page's status and latency maps cross the
// connection; unrelated user-agent and response-mode maps are never loaded.
const analyticsDetailsPageSQL = `
WITH params AS (
	SELECT $1::integer AS window_cycle, $2::integer AS daily_cycle, $3::integer AS contribution_cycle,
		$4::boolean AS include_reports, $5::bigint AS report_from, $6::bigint AS report_to,
		$7::boolean AS include_contributions, $8::bigint AS contribution_from, $9::bigint AS contribution_to,
		$10::text[] AS domains, $11::integer AS requested_page, $12::integer AS page_size
), source_rows AS (
	SELECT c.host_name,
		CASE WHEN p.window_cycle=p.daily_cycle THEN (c.period / 86400) * 86400 ELSE c.period END AS period,
		c.request_qty, c.inbound_bytes, c.outbound_bytes, c.duration_sum, NOT c.is_final AS is_partial
	FROM gateway_traffic_contribution c CROSS JOIN params p
	WHERE p.include_contributions AND c.cycle_type=p.contribution_cycle
		AND c.period >= p.contribution_from AND c.period <= p.contribution_to
		AND (cardinality(p.domains)=0 OR c.host_name=ANY(p.domains))
	UNION ALL
	SELECT r.resource_uuid AS host_name, r.report_ts AS period, r.request_qty,
		r.inbound_bytes, r.outbound_bytes,
		CASE WHEN COALESCE(r.duration->>'avg','') ~ '^-?[0-9]+$' THEN (r.duration->>'avg')::bigint * r.request_qty ELSE 0 END AS duration_sum,
		r.is_partial
	FROM resource_report r CROSS JOIN params p
	WHERE p.include_reports AND r.cycle_type=p.window_cycle
		AND r.report_ts >= p.report_from AND r.report_ts <= p.report_to
		AND (cardinality(p.domains)=0 OR r.resource_uuid=ANY(p.domains))
), numeric_groups AS (
	SELECT host_name, period, SUM(request_qty)::bigint AS request_counts,
		SUM(inbound_bytes)::bigint AS inbound_bytes, SUM(outbound_bytes)::bigint AS outbound_bytes,
		SUM(duration_sum)::bigint AS duration_sum, BOOL_OR(is_partial) AS is_partial
	FROM source_rows
	GROUP BY host_name, period
), totals AS (
	SELECT COUNT(*)::bigint AS total FROM numeric_groups
), numbered_page AS (
	SELECT total, page_size,
		CASE WHEN total=0 THEN 1 ELSE LEAST(GREATEST(requested_page, 1), ((total + page_size - 1) / page_size)::integer) END AS page_index
	FROM totals CROSS JOIN params
), page_meta AS (
	SELECT total, page_size, page_index, (page_index - 1)::bigint * page_size AS page_offset
	FROM numbered_page
), ranked_groups AS (
	SELECT g.*, ROW_NUMBER() OVER (ORDER BY g.period DESC, g.host_name ASC) AS row_ordinal
	FROM numeric_groups g
), page_rows AS (
	SELECT g.*
	FROM ranked_groups g CROSS JOIN page_meta p
	WHERE g.row_ordinal > p.page_offset AND g.row_ordinal <= p.page_offset + p.page_size
), page_metric_rows AS (
	SELECT c.host_name,
		CASE WHEN p.window_cycle=p.daily_cycle THEN (c.period / 86400) * 86400 ELSE c.period END AS period,
		c.status_code, c.latency_histogram
	FROM gateway_traffic_contribution c CROSS JOIN params p
	WHERE p.include_contributions AND c.cycle_type=p.contribution_cycle
		AND c.period >= p.contribution_from AND c.period <= p.contribution_to
		AND (cardinality(p.domains)=0 OR c.host_name=ANY(p.domains))
		AND EXISTS (
			SELECT 1 FROM page_rows selected
			WHERE selected.host_name=c.host_name
				AND selected.period=CASE WHEN p.window_cycle=p.daily_cycle THEN (c.period / 86400) * 86400 ELSE c.period END
		)
	UNION ALL
	SELECT r.resource_uuid AS host_name, r.report_ts AS period, r.status_code, r.latency_histogram
	FROM resource_report r CROSS JOIN params p
	WHERE p.include_reports AND r.cycle_type=p.window_cycle
		AND r.report_ts >= p.report_from AND r.report_ts <= p.report_to
		AND (cardinality(p.domains)=0 OR r.resource_uuid=ANY(p.domains))
		AND EXISTS (
			SELECT 1 FROM page_rows selected
			WHERE selected.host_name=r.resource_uuid AND selected.period=r.report_ts
		)
), expanded_counts AS (
	SELECT s.host_name, s.period, 'status'::text AS kind, item.key,
		CASE WHEN item.value ~ '^-?[0-9]+$' THEN item.value::bigint ELSE 0 END AS value
	FROM page_metric_rows s CROSS JOIN LATERAL jsonb_each_text(COALESCE(s.status_code, '{}'::jsonb)) item
	UNION ALL
	SELECT s.host_name, s.period, 'latency'::text AS kind, item.key,
		CASE WHEN item.value ~ '^-?[0-9]+$' THEN item.value::bigint ELSE 0 END AS value
	FROM page_metric_rows s CROSS JOIN LATERAL jsonb_each_text(COALESCE(s.latency_histogram, '{}'::jsonb)) item
), summed_counts AS (
	SELECT host_name, period, kind, key, SUM(value)::bigint AS value
	FROM expanded_counts
	GROUP BY host_name, period, kind, key
), json_maps AS (
	SELECT host_name, period,
		COALESCE(jsonb_object_agg(key, value) FILTER (WHERE kind='status'), '{}'::jsonb) AS status_codes,
		COALESCE(jsonb_object_agg(key, value) FILTER (WHERE kind='latency'), '{}'::jsonb) AS latency_histogram
	FROM summed_counts
	GROUP BY host_name, period
), coverage_buckets AS (
	SELECT period, BOOL_OR(NOT is_partial) AS finalized
	FROM source_rows
	GROUP BY period
), coverage AS (
	SELECT COALESCE(MIN(period), 0)::bigint AS first_bucket,
		COALESCE(MAX(period) FILTER (WHERE finalized), 0)::bigint AS last_final_bucket,
		COUNT(*) FILTER (WHERE finalized)::bigint AS finalized_buckets
	FROM coverage_buckets
), page_payload AS (
	SELECT COALESCE(jsonb_agg(jsonb_build_object(
		'hostName', host_name, 'period', period, 'requestCounts', request_counts,
		'inboundBytes', inbound_bytes, 'outboundBytes', outbound_bytes, 'durationSum', duration_sum,
		'statusCodes', COALESCE(json_maps.status_codes, '{}'::jsonb),
		'latencyHistogram', COALESCE(json_maps.latency_histogram, '{}'::jsonb), 'isPartial', is_partial
	) ORDER BY page_rows.period DESC, page_rows.host_name ASC), '[]'::jsonb) AS value
	FROM page_rows LEFT JOIN json_maps USING (host_name, period)
)
SELECT page.page_index, page.page_size, page.total,
	coverage.first_bucket, coverage.last_final_bucket, coverage.finalized_buckets, records.value
FROM page_meta page CROSS JOIN coverage CROSS JOIN page_payload records`

func (p *Postgres) AnalyticsOverview(ctx context.Context, input management.AnalyticsOverviewQuery) (management.AnalyticsOverview, error) {
	started := time.Now()
	snapshot, loaded, err := p.loadAnalyticsQuery(ctx, input.AnalyticsQuery)
	if err != nil {
		return management.AnalyticsOverview{}, err
	}
	result := management.AnalyticsOverview{AnalyticsBase: snapshot.baseDTO()}
	for _, section := range input.Sections {
		switch section {
		case "overview":
			overview := snapshot.overviewDTO()
			result.Overview = &overview
		case "domains":
			result.Domains = topDomainsTyped(snapshot.rows, snapshot.window.end.Sub(snapshot.window.begin), 200)
		case "timeseries":
			result.Points = denseAnalyticsPointsTyped(snapshot.rows, snapshot.window)
		default:
			return management.AnalyticsOverview{}, fmt.Errorf("unsupported dashboard section %q", section)
		}
	}
	p.observeAnalyticsLoad("dashboard", snapshot, started, loaded)
	return result, nil
}

func (p *Postgres) observeAnalyticsLoad(operation string, snapshot analyticsSnapshot, started, loaded time.Time) {
	if p.analyticsObserver != nil {
		p.analyticsObserver(operation, snapshot.source, len(snapshot.rows), loaded.Sub(started), time.Since(loaded))
	}
}

func (value analyticsSnapshot) overviewDTO() management.AnalyticsSummary {
	total := combineAnalyticsRows(value.rows)
	latest := latestAnalyticsTimestamp(value.rows)
	lag := int64(0)
	if latest > 0 {
		lag = max(value.now.Unix()-latest, 0)
	}
	return management.AnalyticsSummary{
		AnalyticsBase: value.baseDTO(), DataLagSeconds: lag, HasData: latest > 0, IsPartial: total.partial,
		ActiveHostCount: len(analyticsDomainGroups(value.rows)), Kpis: analyticsKpis(total, value.window.end.Sub(value.window.begin)),
		StatusBreakdown: metricItemsTyped(statusFamilyCounts(total.statuses, total.requests)), DurationBuckets: metricItemsTyped(publicDurationBuckets(total.durationBuckets)), ErrorBreakdown: metricItemsTyped(total.errors),
		UserAgents: analyticsUserAgentsTyped(total), TopDomains: topDomainsTyped(value.rows, value.window.end.Sub(value.window.begin), 10),
		Modes: analyticsModeBreakdown(total),
	}
}

func analyticsModeBreakdown(m analyticsMetrics) management.AnalyticsModeBreakdown {
	return management.AnalyticsModeBreakdown{
		NonStream: analyticsModeMetrics(m, "non_stream"),
		Stream:    analyticsModeMetrics(m, "stream"),
		Upgrade:   analyticsModeMetrics(m, "upgrade"),
	}
}

func analyticsModeMetrics(m analyticsMetrics, mode string) management.AnalyticsModeMetrics {
	metrics := m.responseModeMetrics[mode]
	count := metrics["requests"]
	completion := modeHistogram(metrics, "duration_hist:")
	ttfb := modeHistogram(metrics, "ttfb_hist:")
	ttft := modeHistogram(metrics, "ttft_hist:")
	return management.AnalyticsModeMetrics{
		Requests:               count,
		CompletionLatency:      analyticsLatency(completion, metrics["duration_sum"], metrics["duration_max"], count),
		FirstByteLatency:       analyticsLatency(ttfb, metrics["ttfb_sum"], 0, metrics["ttfb_count"]),
		FirstTokenLatency:      analyticsLatency(ttft, metrics["ttft_sum"], 0, metrics["ttft_count"]),
		CompletionCoverageRate: ratio(sumCounts(completion), count),
		FirstByteCoverageRate:  ratio(sumCounts(ttfb), count),
		FirstTokenCoverageRate: ratio(sumCounts(ttft), count),
	}
}

func modeHistogram(values map[string]int64, prefix string) map[string]int64 {
	out := map[string]int64{}
	for key, value := range values {
		if strings.HasPrefix(key, prefix) {
			out[strings.TrimPrefix(key, prefix)] = value
		}
	}
	return out
}

func mergeNestedCounts(dst, src map[string]map[string]int64) map[string]map[string]int64 {
	if dst == nil {
		dst = map[string]map[string]int64{}
	}
	for mode, values := range src {
		if dst[mode] == nil {
			dst[mode] = map[string]int64{}
		}
		for key, value := range values {
			dst[mode][key] += value
		}
	}
	return dst
}

func analyticsLatency(hist map[string]int64, sum, maxValue, count int64) management.AnalyticsLatency {
	result := management.AnalyticsLatency{AvgMs: float64(safeAverage(sum, count))}
	if count > 0 && sumCounts(hist) == count {
		p50, p95, p99 := float64(estimateQuantile(hist, .50)), float64(estimateQuantile(hist, .95)), float64(estimateQuantile(hist, .99))
		result.P50Ms, result.P95Ms, result.P99Ms = &p50, &p95, &p99
	}
	_ = maxValue
	return result
}

type analyticsWindowValue struct {
	begin, end time.Time
	cycle      int
	grain      string
	step       time.Duration
}

func analyticsWindowForRange(now time.Time, from, to int64) (analyticsWindowValue, error) {
	now = now.UTC()
	if to <= 0 || to > now.Add(time.Minute).Unix() {
		to = now.Unix()
	}
	if from <= 0 {
		from = to - int64(time.Hour.Seconds())
	}
	if from >= to {
		return analyticsWindowValue{}, fmt.Errorf("from must be before to")
	}
	if time.Unix(to, 0).Sub(time.Unix(from, 0)) > 730*24*time.Hour {
		return analyticsWindowValue{}, fmt.Errorf("time range exceeds 730 days")
	}
	duration := time.Unix(to, 0).Sub(time.Unix(from, 0))
	window := analyticsWindowValue{begin: time.Unix(from, 0).UTC(), end: time.Unix(to, 0).UTC(), cycle: reportCycleMinutely, grain: "minute", step: time.Minute}
	if duration > 48*time.Hour {
		window.cycle, window.grain, window.step = reportCycleHourly, "hour", time.Hour
	}
	if duration > 365*24*time.Hour {
		window.cycle, window.grain, window.step = reportCycleDaily, "day", 24*time.Hour
	}
	window.begin = window.begin.Truncate(window.step)
	window.end = window.end.Truncate(window.step)
	if !window.end.After(window.begin) {
		window.end = window.begin.Add(window.step)
	}
	return window, nil
}

type analyticsRow struct {
	domain    string
	timestamp int64
	metrics   analyticsMetrics
}

type analyticsMetrics struct {
	requests, inbound, outbound, durationSum, durationMax int64
	statuses, errors, histogram                           map[string]int64
	browsers, spiders, oses, durationBuckets              map[string]int64
	responseModeMetrics                                   map[string]map[string]int64
	partial                                               bool
}

func (p *Postgres) analyticsRows(ctx context.Context, now time.Time, window analyticsWindowValue, domains []string) ([]analyticsRow, string, error) {
	if domains == nil {
		// pgx encodes a nil slice as SQL NULL, and cardinality(NULL)/ANY(NULL) both
		// evaluate to NULL, which would make the filters below match nothing.
		domains = []string{}
	}
	cutover := now.Add(-p.contributionRetention).UTC().Truncate(window.step)
	result := []analyticsRow{}
	source := analyticsSource
	appendContributions := func(from, to time.Time) error {
		if to.Before(from) {
			return nil
		}
		sourceCycle := window.cycle
		if sourceCycle == reportCycleDaily {
			sourceCycle = reportCycleHourly
		}
		rows, err := p.pool.Query(ctx, analyticsContributionRowsSQL, window.cycle, reportCycleDaily, sourceCycle, from.Unix(), to.Unix(), domains)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row analyticsRow
			var status, spider, browser, osValues, durationBuckets, errors, histogram, modeMetrics []byte
			if err := rows.Scan(&row.domain, &row.timestamp, &row.metrics.requests, &row.metrics.inbound, &row.metrics.outbound, &row.metrics.durationSum, &row.metrics.durationMax, &status, &spider, &browser, &osValues, &durationBuckets, &errors, &histogram, &modeMetrics, &row.metrics.partial); err != nil {
				return err
			}
			row.metrics.statuses, row.metrics.spiders, row.metrics.browsers, row.metrics.oses = decodeReportCounts(status), decodeReportCounts(spider), decodeReportCounts(browser), decodeReportCounts(osValues)
			row.metrics.durationBuckets, row.metrics.errors, row.metrics.histogram = decodeReportCounts(durationBuckets), decodeReportCounts(errors), decodeReportCounts(histogram)
			_ = json.Unmarshal(modeMetrics, &row.metrics.responseModeMetrics)
			result = append(result, row)
		}
		return rows.Err()
	}
	appendReports := func(from, to time.Time) error {
		if to.Before(from) {
			return nil
		}
		rows, err := p.pool.Query(ctx, `SELECT resource_uuid,report_ts,request_qty,inbound_bytes,outbound_bytes,
			CASE WHEN COALESCE(duration->>'avg','') ~ '^-?[0-9]+$' THEN (duration->>'avg')::bigint * request_qty ELSE 0 END,
			CASE WHEN COALESCE(duration->>'max','') ~ '^-?[0-9]+$' THEN (duration->>'max')::bigint ELSE 0 END,
			status_code,spider,browser,os,duration,error_counts,latency_histogram,response_mode_metrics,is_partial
			FROM resource_report
			WHERE cycle_type=$1 AND report_ts >= $2 AND report_ts <= $3 AND (cardinality($4::text[])=0 OR resource_uuid = ANY($4))
			ORDER BY report_ts,resource_uuid`, window.cycle, from.Unix(), to.Unix(), domains)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row analyticsRow
			var status, spider, browser, osValues, durationBuckets, errors, histogram, modeMetrics []byte
			if err := rows.Scan(&row.domain, &row.timestamp, &row.metrics.requests, &row.metrics.inbound, &row.metrics.outbound, &row.metrics.durationSum, &row.metrics.durationMax, &status, &spider, &browser, &osValues, &durationBuckets, &errors, &histogram, &modeMetrics, &row.metrics.partial); err != nil {
				return err
			}
			row.metrics.statuses, row.metrics.spiders, row.metrics.browsers, row.metrics.oses = decodeReportCounts(status), decodeReportCounts(spider), decodeReportCounts(browser), decodeReportCounts(osValues)
			row.metrics.durationBuckets, row.metrics.errors, row.metrics.histogram = decodeReportCounts(durationBuckets), decodeReportCounts(errors), decodeReportCounts(histogram)
			delete(row.metrics.durationBuckets, "avg")
			delete(row.metrics.durationBuckets, "max")
			_ = json.Unmarshal(modeMetrics, &row.metrics.responseModeMetrics)
			result = append(result, row)
		}
		return rows.Err()
	}

	switch {
	case !cutover.After(window.begin):
		if err := appendContributions(window.begin, window.end); err != nil {
			return nil, "", err
		}
	case cutover.After(window.end):
		source = analyticsReportSource
		if err := appendReports(window.begin, window.end); err != nil {
			return nil, "", err
		}
	default:
		source = analyticsMixedSource
		if err := appendReports(window.begin, cutover.Add(-window.step)); err != nil {
			return nil, "", err
		}
		if err := appendContributions(cutover, window.end); err != nil {
			return nil, "", err
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].timestamp != result[j].timestamp {
			return result[i].timestamp < result[j].timestamp
		}
		return result[i].domain < result[j].domain
	})
	return result, source, nil
}

func combineAnalyticsRows(rows []analyticsRow) analyticsMetrics {
	total := analyticsMetrics{
		statuses: map[string]int64{}, errors: map[string]int64{}, histogram: map[string]int64{}, browsers: map[string]int64{},
		spiders: map[string]int64{}, oses: map[string]int64{}, durationBuckets: map[string]int64{},
		responseModeMetrics: map[string]map[string]int64{},
	}
	for _, row := range rows {
		total.requests += row.metrics.requests
		total.inbound += row.metrics.inbound
		total.outbound += row.metrics.outbound
		total.durationSum += row.metrics.durationSum
		total.durationMax = max(total.durationMax, row.metrics.durationMax)
		total.partial = total.partial || row.metrics.partial
		total.responseModeMetrics = mergeNestedCounts(total.responseModeMetrics, row.metrics.responseModeMetrics)
		addCounts(total.statuses, row.metrics.statuses)
		addCounts(total.errors, row.metrics.errors)
		addCounts(total.histogram, row.metrics.histogram)
		addCounts(total.browsers, row.metrics.browsers)
		addCounts(total.spiders, row.metrics.spiders)
		addCounts(total.oses, row.metrics.oses)
		addCounts(total.durationBuckets, row.metrics.durationBuckets)
	}
	return total
}

func (m analyticsMetrics) kpis(duration time.Duration) map[string]any {
	seconds := duration.Seconds()
	if seconds <= 0 {
		seconds = 1
	}
	families := statusFamilyCounts(m.statuses, m.requests)
	errors := families["4xx"] + families["5xx"] + families["6xx"]
	histogramCoverage := ratio(sumCounts(m.histogram), m.requests)
	kpis := map[string]any{
		"requests": m.requests, "requestsPerSecond": float64(m.requests) / seconds,
		"inboundBytes": m.inbound, "outboundBytes": m.outbound,
		"inboundBytesPerSecond": float64(m.inbound) / seconds, "outboundBytesPerSecond": float64(m.outbound) / seconds,
		"errorRequests": errors, "errorRate": ratio(errors, m.requests),
		"avgDurationMs": safeAverage(m.durationSum, m.requests), "maxDurationMs": m.durationMax,
		"latencyCoverageRate": histogramCoverage,
	}
	if histogramCoverage == 1 {
		kpis["p50Ms"] = estimateQuantile(m.histogram, .50)
		kpis["p95Ms"] = estimateQuantile(m.histogram, .95)
		kpis["p99Ms"] = estimateQuantile(m.histogram, .99)
	} else {
		kpis["p50Ms"], kpis["p95Ms"], kpis["p99Ms"] = nil, nil, nil
	}
	return kpis
}

func analyticsKpis(m analyticsMetrics, duration time.Duration) management.AnalyticsKpis {
	seconds := duration.Seconds()
	if seconds <= 0 {
		seconds = 1
	}
	families := statusFamilyCounts(m.statuses, m.requests)
	errors := families["4xx"] + families["5xx"] + families["6xx"]
	coverage := ratio(sumCounts(m.histogram), m.requests)
	result := management.AnalyticsKpis{
		Requests: m.requests, RequestsPerSecond: float64(m.requests) / seconds,
		InboundBytes: m.inbound, OutboundBytes: m.outbound,
		InboundBytesPerSecond: float64(m.inbound) / seconds, OutboundBytesPerSecond: float64(m.outbound) / seconds,
		ErrorRequests: errors, ErrorRate: ratio(errors, m.requests),
		AvgDurationMs: float64(safeAverage(m.durationSum, m.requests)), MaxDurationMs: float64(m.durationMax), LatencyCoverageRate: coverage,
	}
	if coverage == 1 {
		p50, p95, p99 := float64(estimateQuantile(m.histogram, .50)), float64(estimateQuantile(m.histogram, .95)), float64(estimateQuantile(m.histogram, .99))
		result.P50Ms, result.P95Ms, result.P99Ms = &p50, &p95, &p99
	}
	return result
}

func metricItemsTyped(values map[string]int64) []management.MetricItem {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]management.MetricItem, 0, len(keys))
	for _, key := range keys {
		items = append(items, management.MetricItem{Type: key, Value: values[key]})
	}
	return items
}

func denseAnalyticsPointsTyped(rows []analyticsRow, window analyticsWindowValue) []management.AnalyticsPoint {
	byTimestamp := map[int64][]analyticsRow{}
	for _, row := range rows {
		byTimestamp[row.timestamp] = append(byTimestamp[row.timestamp], row)
	}
	points := make([]management.AnalyticsPoint, 0, int(window.end.Sub(window.begin)/window.step)+1)
	for at := window.begin; !at.After(window.end); at = at.Add(window.step) {
		metrics := combineAnalyticsRows(byTimestamp[at.Unix()])
		points = append(points, management.AnalyticsPoint{
			AnalyticsKpis: analyticsKpis(metrics, window.step), BucketStartAt: at.Unix(), IsPartial: metrics.partial,
			StatusBreakdown: metricItemsTyped(statusFamilyCounts(metrics.statuses, metrics.requests)), DurationBuckets: metricItemsTyped(publicDurationBuckets(metrics.durationBuckets)),
			Modes: analyticsModeBreakdown(metrics),
		})
	}
	return points
}

func (m analyticsMetrics) userAgentBreakdown() map[string]any {
	browserCoverage := sumCounts(m.browsers) + sumCounts(m.spiders)
	return map[string]any{
		"browsers": metricItems(m.browsers), "spiders": metricItems(m.spiders), "operatingSystems": metricItems(m.oses),
		"coverage": map[string]any{
			"browserRate": ratio(browserCoverage, m.requests),
			"osRate":      ratio(sumCounts(m.oses), m.requests),
			"spiderRate":  ratio(sumCounts(m.spiders), m.requests),
		},
	}
}

func analyticsUserAgentsTyped(m analyticsMetrics) management.AnalyticsUserAgents {
	browserCoverage := sumCounts(m.browsers) + sumCounts(m.spiders)
	return management.AnalyticsUserAgents{
		Browsers: metricItemsTyped(m.browsers), Spiders: metricItemsTyped(m.spiders), OperatingSystems: metricItemsTyped(m.oses),
		Coverage: management.AnalyticsUserAgentCoverage{BrowserRate: ratio(browserCoverage, m.requests), OSRate: ratio(sumCounts(m.oses), m.requests), SpiderRate: ratio(sumCounts(m.spiders), m.requests)},
	}
}

func statusFamilyCounts(statuses map[string]int64, requests int64) map[string]int64 {
	families := map[string]int64{"1xx": 0, "2xx": 0, "3xx": 0, "4xx": 0, "5xx": 0, "6xx": 0, "other": 0}
	for key, count := range statuses {
		if strings.HasPrefix(key, "__response_mode:") {
			continue
		}
		switch statusFamily(key) {
		case "1xx", "2xx", "3xx", "4xx", "5xx", "6xx":
			families[statusFamily(key)] += count
		default:
			families["other"] += count
		}
	}
	if known := sumCounts(families); known < requests {
		families["other"] += requests - known
	}
	return families
}

func publicDurationBuckets(values map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(values))
	for key, value := range values {
		if strings.HasPrefix(key, "__response_mode:") || key == "avg" || key == "max" {
			continue
		}
		out[key] = value
	}
	return out
}

func statusFamily(status string) string {
	code, err := strconv.Atoi(strings.TrimSpace(status))
	if err != nil || code < 100 || code >= 700 {
		return "other"
	}
	return strconv.Itoa(code/100) + "xx"
}

func sumCounts(values map[string]int64) int64 {
	var total int64
	for _, value := range values {
		total += value
	}
	return total
}

func safeAverage(total, count int64) int64 {
	if count == 0 {
		return 0
	}
	return total / count
}

func estimateQuantile(histogram map[string]int64, quantile float64) int64 {
	if quantile <= 0 || quantile > 1 {
		return 0
	}
	total := sumCounts(histogram)
	if total == 0 {
		return 0
	}
	target := int64(float64(total)*quantile + .999999)
	type bucket struct {
		upper int64
		count int64
	}
	buckets := make([]bucket, 0, len(histogram))
	for key, count := range histogram {
		if key == "+Inf" {
			buckets = append(buckets, bucket{upper: 30001, count: count})
			continue
		}
		upper, err := strconv.ParseInt(strings.TrimPrefix(key, "<="), 10, 64)
		if err == nil {
			buckets = append(buckets, bucket{upper: upper, count: count})
		}
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].upper < buckets[j].upper })
	var cumulative int64
	for _, bucket := range buckets {
		cumulative += bucket.count
		if cumulative >= target {
			return bucket.upper
		}
	}
	return 0
}

func topDomains(rows []analyticsRow, duration time.Duration, limit int) []map[string]any {
	groups := analyticsDomainGroups(rows)
	items := make([]map[string]any, 0, len(groups))
	for domain, domainRows := range groups {
		metrics := combineAnalyticsRows(domainRows)
		item := metrics.kpis(duration)
		item["domain"] = domain
		item["isPartial"] = metrics.partial
		item["statusBreakdown"] = metricItems(statusFamilyCounts(metrics.statuses, metrics.requests))
		item["durationBuckets"] = metricItems(metrics.durationBuckets)
		item["userAgents"] = metrics.userAgentBreakdown()
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i]["requests"].(int64), items[j]["requests"].(int64)
		if left != right {
			return left > right
		}
		return items[i]["domain"].(string) < items[j]["domain"].(string)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func topDomainsTyped(rows []analyticsRow, duration time.Duration, limit int) []management.AnalyticsDomain {
	groups := analyticsDomainGroups(rows)
	items := make([]management.AnalyticsDomain, 0, len(groups))
	for domain, domainRows := range groups {
		metrics := combineAnalyticsRows(domainRows)
		items = append(items, management.AnalyticsDomain{
			AnalyticsKpis: analyticsKpis(metrics, duration), Domain: domain, IsPartial: metrics.partial,
			StatusBreakdown: metricItemsTyped(statusFamilyCounts(metrics.statuses, metrics.requests)), DurationBuckets: metricItemsTyped(publicDurationBuckets(metrics.durationBuckets)), UserAgents: analyticsUserAgentsTyped(metrics),
			Modes: analyticsModeBreakdown(metrics),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Requests != items[j].Requests {
			return items[i].Requests > items[j].Requests
		}
		return items[i].Domain < items[j].Domain
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func analyticsDomainGroups(rows []analyticsRow) map[string][]analyticsRow {
	groups := map[string][]analyticsRow{}
	for _, row := range rows {
		groups[row.domain] = append(groups[row.domain], row)
	}
	return groups
}

func analyticsContributions(rows []analyticsRow, window analyticsWindowValue) (map[string]any, []map[string]any, []map[string]any) {
	groups := analyticsDomainGroups(rows)
	type rankedHost struct {
		host    string
		metrics analyticsMetrics
	}
	hosts := make([]rankedHost, 0, len(groups))
	total := combineAnalyticsRows(rows)
	for host, hostRows := range groups {
		metrics := combineAnalyticsRows(hostRows)
		hosts = append(hosts, rankedHost{host: host, metrics: metrics})
	}
	sort.Slice(hosts, func(i, j int) bool {
		if hosts[i].metrics.requests != hosts[j].metrics.requests {
			return hosts[i].metrics.requests > hosts[j].metrics.requests
		}
		return hosts[i].host < hosts[j].host
	})

	rankings := make([]map[string]any, 0, min(10, len(hosts)))
	for _, host := range hosts[:min(10, len(hosts))] {
		rankings = append(rankings, map[string]any{
			"hostName": host.host, "requestCounts": host.metrics.requests, "inboundBytes": host.metrics.inbound,
			"outboundBytes": host.metrics.outbound, "totalBytes": host.metrics.inbound + host.metrics.outbound,
			"share": ratio(host.metrics.requests, total.requests),
		})
	}

	topHosts := make(map[string]bool, min(5, len(hosts)))
	names := make([]string, 0, min(6, len(hosts)))
	for _, host := range hosts[:min(5, len(hosts))] {
		topHosts[host.host] = true
		names = append(names, host.host)
	}
	if len(hosts) > len(topHosts) {
		names = append(names, "其他")
	}
	byTimestamp := map[int64]map[string][]analyticsRow{}
	for _, row := range rows {
		host := row.domain
		if !topHosts[host] {
			host = "其他"
		}
		if byTimestamp[row.timestamp] == nil {
			byTimestamp[row.timestamp] = map[string][]analyticsRow{}
		}
		byTimestamp[row.timestamp][host] = append(byTimestamp[row.timestamp][host], row)
	}
	timeline := make([]map[string]any, 0, len(names)*int(window.end.Sub(window.begin)/window.step+1))
	for at := window.begin; !at.After(window.end); at = at.Add(window.step) {
		for _, host := range names {
			metrics := combineAnalyticsRows(byTimestamp[at.Unix()][host])
			timeline = append(timeline, map[string]any{
				"period": at.Unix(), "hostName": host, "requestCounts": metrics.requests, "isPartial": metrics.partial,
			})
		}
	}
	topHost, topRequests := "", int64(0)
	if len(hosts) > 0 {
		topHost, topRequests = hosts[0].host, hosts[0].metrics.requests
	}
	return map[string]any{
		"totalRequests": total.requests, "activeHostCount": int64(len(hosts)),
		"topHostName": topHost, "topHostRequestCounts": topRequests,
	}, rankings, timeline
}

func analyticsContributionsTyped(rows []analyticsRow, window analyticsWindowValue) (management.AnalyticsContributionSummary, []management.AnalyticsHostRanking, []management.AnalyticsContributionPoint) {
	groups := analyticsDomainGroups(rows)
	type rankedHost struct {
		host    string
		metrics analyticsMetrics
	}
	hosts := make([]rankedHost, 0, len(groups))
	total := combineAnalyticsRows(rows)
	for host, hostRows := range groups {
		hosts = append(hosts, rankedHost{host: host, metrics: combineAnalyticsRows(hostRows)})
	}
	sort.Slice(hosts, func(i, j int) bool {
		if hosts[i].metrics.requests != hosts[j].metrics.requests {
			return hosts[i].metrics.requests > hosts[j].metrics.requests
		}
		return hosts[i].host < hosts[j].host
	})
	rankings := make([]management.AnalyticsHostRanking, 0, min(10, len(hosts)))
	for _, host := range hosts[:min(10, len(hosts))] {
		rankings = append(rankings, management.AnalyticsHostRanking{HostName: host.host, RequestCounts: host.metrics.requests, InboundBytes: host.metrics.inbound, OutboundBytes: host.metrics.outbound, TotalBytes: host.metrics.inbound + host.metrics.outbound, Share: ratio(host.metrics.requests, total.requests)})
	}
	topHosts := make(map[string]bool, min(5, len(hosts)))
	names := make([]string, 0, min(6, len(hosts)))
	for _, host := range hosts[:min(5, len(hosts))] {
		topHosts[host.host] = true
		names = append(names, host.host)
	}
	if len(hosts) > len(topHosts) {
		names = append(names, "其他")
	}
	byTimestamp := map[int64]map[string][]analyticsRow{}
	for _, row := range rows {
		host := row.domain
		if !topHosts[host] {
			host = "其他"
		}
		if byTimestamp[row.timestamp] == nil {
			byTimestamp[row.timestamp] = map[string][]analyticsRow{}
		}
		byTimestamp[row.timestamp][host] = append(byTimestamp[row.timestamp][host], row)
	}
	timeline := make([]management.AnalyticsContributionPoint, 0, len(names)*int(window.end.Sub(window.begin)/window.step+1))
	for at := window.begin; !at.After(window.end); at = at.Add(window.step) {
		for _, host := range names {
			metrics := combineAnalyticsRows(byTimestamp[at.Unix()][host])
			timeline = append(timeline, management.AnalyticsContributionPoint{Period: at.Unix(), HostName: host, RequestCounts: metrics.requests, IsPartial: metrics.partial})
		}
	}
	summary := management.AnalyticsContributionSummary{TotalRequests: total.requests, ActiveHostCount: len(hosts)}
	if len(hosts) > 0 {
		summary.TopHostName, summary.TopHostRequestCount = hosts[0].host, hosts[0].metrics.requests
	}
	return summary, rankings, timeline
}

func analyticsDetails(rows []analyticsRow, window analyticsWindowValue, input map[string]any) ([]map[string]any, map[string]any) {
	type detailKey struct {
		host   string
		period int64
	}
	grouped := map[detailKey][]analyticsRow{}
	for _, row := range rows {
		key := detailKey{host: row.domain, period: row.timestamp}
		grouped[key] = append(grouped[key], row)
	}
	type detailRow struct {
		host    string
		period  int64
		metrics analyticsMetrics
	}
	values := make([]detailRow, 0, len(grouped))
	for key, groupedRows := range grouped {
		values = append(values, detailRow{host: key.host, period: key.period, metrics: combineAnalyticsRows(groupedRows)})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].period != values[j].period {
			return values[i].period > values[j].period
		}
		return values[i].host < values[j].host
	})
	pageIndex, pageSize := pageValues(input)
	total := len(values)
	start := (pageIndex - 1) * pageSize
	if start >= total && total > 0 {
		pageIndex = (total-1)/pageSize + 1
		start = (pageIndex - 1) * pageSize
	}
	end := min(start+pageSize, total)
	if start > total {
		start = total
	}
	records := make([]map[string]any, 0, end-start)
	for _, value := range values[start:end] {
		kpis := value.metrics.kpis(window.step)
		records = append(records, map[string]any{
			"id": fmt.Sprintf("%d:%s", value.period, value.host), "hostName": value.host, "period": value.period,
			"requestCounts": value.metrics.requests, "inboundBytes": value.metrics.inbound, "outboundBytes": value.metrics.outbound,
			"totalBytes": value.metrics.inbound + value.metrics.outbound, "errorRequests": kpis["errorRequests"],
			"errorRate": kpis["errorRate"], "avgDurationMs": kpis["avgDurationMs"], "p95Ms": kpis["p95Ms"],
			"isPartial": value.metrics.partial,
		})
	}
	return records, map[string]any{"pageIndex": pageIndex, "pageSize": pageSize, "total": total}
}

func analyticsDetailsTyped(rows []analyticsRow, window analyticsWindowValue, pageIndex, pageSize int) ([]management.AnalyticsDetail, management.PageCursor) {
	type detailKey struct {
		host   string
		period int64
	}
	grouped := map[detailKey][]analyticsRow{}
	for _, row := range rows {
		key := detailKey{host: row.domain, period: row.timestamp}
		grouped[key] = append(grouped[key], row)
	}
	type detailRow struct {
		host    string
		period  int64
		metrics analyticsMetrics
	}
	values := make([]detailRow, 0, len(grouped))
	for key, groupedRows := range grouped {
		values = append(values, detailRow{host: key.host, period: key.period, metrics: combineAnalyticsRows(groupedRows)})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].period != values[j].period {
			return values[i].period > values[j].period
		}
		return values[i].host < values[j].host
	})
	total := len(values)
	paging, offset, err := management.PageWindow(management.PageRequest{PageIndex: pageIndex, PageSize: pageSize}, int64(total))
	if err != nil {
		// The management layer validates requests before reaching this helper.
		// Keep a safe empty page for direct internal callers.
		return []management.AnalyticsDetail{}, management.PageCursor{PageIndex: management.DefaultPageIndex, PageSize: management.DefaultPageSize, Total: int64(total)}
	}
	start := int(offset)
	end := min(start+paging.PageSize, total)
	records := make([]management.AnalyticsDetail, 0, end-start)
	for _, value := range values[start:end] {
		kpis := analyticsKpis(value.metrics, window.step)
		records = append(records, management.AnalyticsDetail{ID: fmt.Sprintf("%d:%s", value.period, value.host), HostName: value.host, Period: value.period, RequestCounts: value.metrics.requests, InboundBytes: value.metrics.inbound, OutboundBytes: value.metrics.outbound, TotalBytes: value.metrics.inbound + value.metrics.outbound, ErrorRequests: kpis.ErrorRequests, ErrorRate: kpis.ErrorRate, AvgDurationMs: kpis.AvgDurationMs, P95Ms: kpis.P95Ms, IsPartial: value.metrics.partial})
	}
	return records, paging
}

func denseAnalyticsPoints(rows []analyticsRow, window analyticsWindowValue) []map[string]any {
	byTimestamp := map[int64][]analyticsRow{}
	for _, row := range rows {
		byTimestamp[row.timestamp] = append(byTimestamp[row.timestamp], row)
	}
	points := make([]map[string]any, 0, int(window.end.Sub(window.begin)/window.step)+1)
	for at := window.begin; !at.After(window.end); at = at.Add(window.step) {
		metrics := combineAnalyticsRows(byTimestamp[at.Unix()])
		point := metrics.kpis(window.step)
		point["bucketStartAt"] = at.Unix()
		point["isPartial"] = metrics.partial
		point["statusBreakdown"] = metricItems(statusFamilyCounts(metrics.statuses, metrics.requests))
		point["durationBuckets"] = metricItems(metrics.durationBuckets)
		points = append(points, point)
	}
	return points
}

func analyticsCoverage(window analyticsWindowValue, rows []analyticsRow) management.AnalyticsCoverage {
	covered, finalized := map[int64]bool{}, map[int64]bool{}
	first, lastFinal := int64(0), int64(0)
	for _, row := range rows {
		covered[row.timestamp] = true
		if first == 0 || row.timestamp < first {
			first = row.timestamp
		}
		if !row.metrics.partial {
			finalized[row.timestamp] = true
			if row.timestamp > lastFinal {
				lastFinal = row.timestamp
			}
		}
	}
	var missing int64
	for at := window.begin; !at.After(window.end); at = at.Add(window.step) {
		if !covered[at.Unix()] || !finalized[at.Unix()] {
			missing++
		}
	}
	coveredTo := int64(0)
	if lastFinal > 0 {
		coveredTo = lastFinal + int64(window.step.Seconds())
	}
	complete := first > 0 && missing == 0
	state := "collecting"
	if complete {
		state = "complete"
	} else if first > 0 {
		state = "incomplete"
	}
	return management.AnalyticsCoverage{From: first, To: coveredTo, IsComplete: complete, State: state, MissingBuckets: int(missing), WindowBuckets: int(window.end.Sub(window.begin)/window.step) + 1}
}

func latestAnalyticsTimestamp(rows []analyticsRow) int64 {
	var latest int64
	for _, row := range rows {
		latest = max(latest, row.timestamp)
	}
	return latest
}
