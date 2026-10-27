package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"yaginx-gateway/internal/management"
	"yaginx-gateway/internal/platform/hostnorm"
)

type resourceHost struct {
	name         string
	managed      bool
	domainConfig *management.DomainListItem
	websites     []resourceWebsiteRef
	certificate  *resourceCertificate
	autoSummary  *ACMEAutoSummary
	lastActivity int64
}

type resourceWebsiteRef struct {
	ID   management.ID `json:"id"`
	Name string        `json:"name"`
}

type resourceCertificate struct {
	Fingerprint string `json:"fingerprint"`
	NotBefore   int64  `json:"notBefore"`
	NotAfter    int64  `json:"notAfter"`
	IsActive    bool   `json:"isActive"`
}

func normalizeResourceHost(value string) string {
	value, _ = hostnorm.IPOrHost(value)
	return value
}

func normalizedWebsiteHosts(website management.Website) []string {
	values := append([]string(nil), website.Hosts...)
	for _, binding := range website.HostBindings {
		values = append(values, binding.Hostname)
	}
	if website.DefaultHost != nil {
		values = append(values, *website.DefaultHost)
	}
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if host := normalizeResourceHost(value); host != "" && !seen[host] {
			seen[host] = true
			result = append(result, host)
		}
	}
	sort.Strings(result)
	return result
}

func (p *Postgres) resourceHosts(ctx context.Context, hostFilter []string) (map[string]*resourceHost, error) {
	if hostFilter == nil {
		// pgx encodes a nil slice as SQL NULL, and cardinality(NULL)/ANY(NULL) both
		// evaluate to NULL, which would make the activity query match nothing.
		// An empty (non-nil) slice makes the "cardinality(...)=0" branch true instead.
		hostFilter = []string{}
	}
	items := map[string]*resourceHost{}
	ensure := func(value string) *resourceHost {
		host := normalizeResourceHost(value)
		if host == "" {
			return nil
		}
		if items[host] == nil {
			items[host] = &resourceHost{name: host, websites: []resourceWebsiteRef{}}
		}
		return items[host]
	}

	// These four sources are independent. Loading them concurrently removes
	// their serial round-trip latency while preserving the existing merge
	// semantics and real-time activity values.
	var websites []management.Website
	var domains []management.DomainListItem
	type certificateRow struct {
		name, fingerprint   string
		notBefore, notAfter time.Time
	}
	var certificates []certificateRow
	type activityRow struct {
		name      string
		timestamp int64
	}
	var activities []activityRow
	var autoSummaries map[string]*ACMEAutoSummary
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { var err error; websites, err = p.ListWebsites(groupCtx); return err })
	group.Go(func() error { var err error; domains, err = p.ListDomains(groupCtx); return err })
	group.Go(func() error { var err error; autoSummaries, err = p.ACMEAutoSummaries(groupCtx); return err })
	group.Go(func() error {
		rows, err := p.pool.Query(groupCtx, `SELECT lower(trim(name)), fingerprint, not_before, not_after
			FROM gateway_certificate c CROSS JOIN LATERAL unnest(c.dns_names) AS name
			WHERE c.status='active' AND c.is_active=true ORDER BY not_after DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item certificateRow
			if err := rows.Scan(&item.name, &item.fingerprint, &item.notBefore, &item.notAfter); err != nil {
				return err
			}
			certificates = append(certificates, item)
		}
		return rows.Err()
	})
	group.Go(func() error {
		rows, err := p.pool.Query(groupCtx, `SELECT host_name, max(activity_ts) FROM (
			SELECT host_name, period AS activity_ts FROM host_traffic WHERE (cardinality($1::text[])=0 OR host_name = ANY($1))
			UNION ALL SELECT host, request_ts / 1000 AS activity_ts FROM request_history WHERE (cardinality($1::text[])=0 OR host = ANY($1))
			UNION ALL SELECT resource_uuid AS host_name, report_ts AS activity_ts FROM resource_report WHERE (cardinality($1::text[])=0 OR resource_uuid = ANY($1))
		) activity WHERE host_name IS NOT NULL GROUP BY host_name`, hostFilter)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item activityRow
			if err := rows.Scan(&item.name, &item.timestamp); err != nil {
				return err
			}
			activities = append(activities, item)
		}
		return rows.Err()
	})
	if err := group.Wait(); err != nil {
		return nil, err
	}
	for _, website := range websites {
		for _, host := range normalizedWebsiteHosts(website) {
			item := ensure(host)
			item.managed = true
			item.websites = append(item.websites, resourceWebsiteRef{ID: website.ID, Name: stringPointerValue(website.Name)})
		}
	}

	for _, domain := range domains {
		if item := ensure(stringPointerValue(domain.Name)); item != nil {
			item.managed = true
			item.domainConfig = &domain
		}
	}

	for name, summary := range autoSummaries {
		if item := items[normalizeResourceHost(name)]; item != nil {
			item.autoSummary = summary
		}
	}
	for _, activity := range activities {
		if item := ensure(activity.name); item != nil && activity.timestamp > item.lastActivity {
			item.lastActivity = activity.timestamp
		}
	}
	// A certificate only decorates a host that is otherwise known (website,
	// web_domain row or observed traffic). Deleting a host keeps its issued
	// certificate, so certificates must not resurrect a deleted host.
	for _, certificate := range certificates {
		if item := items[normalizeResourceHost(certificate.name)]; item != nil {
			item.managed = true
			if item.certificate == nil {
				item.certificate = &resourceCertificate{Fingerprint: certificate.fingerprint, NotBefore: certificate.notBefore.Unix(), NotAfter: certificate.notAfter.Unix(), IsActive: true}
			}
		}
	}
	return items, nil
}

func resourceHostDTO(item *resourceHost) management.ResourceHost {
	status := "unmanaged"
	if item.managed {
		status = "managed"
	}
	websites := make([]management.ResourceWebsiteReference, len(item.websites))
	for index, website := range item.websites {
		websites[index] = management.ResourceWebsiteReference{ID: website.ID, Name: website.Name}
	}
	var certificate *management.ResourceCertificate
	if item.certificate != nil {
		certificate = &management.ResourceCertificate{Fingerprint: item.certificate.Fingerprint, NotBefore: item.certificate.NotBefore, NotAfter: item.certificate.NotAfter, IsActive: item.certificate.IsActive}
	}
	return management.ResourceHost{Host: item.name, ManagementStatus: status, DomainConfig: item.domainConfig, Websites: websites, Certificate: certificate, AutoRenew: resourceAutoRenew(item), LastActivity: item.lastActivity}
}

// resourceAutoRenew is nil for hosts with no web_domain row (never configured).
func resourceAutoRenew(item *resourceHost) *management.ResourceAutoRenew {
	if item.domainConfig == nil {
		return nil
	}
	out := &management.ResourceAutoRenew{Status: "off", MaxPerDay: ACMEAutoMaxAttemptsPerDay}
	if item.domainConfig.AutoRequestCertificate {
		out.Status = "active"
	}
	if summary := item.autoSummary; summary != nil {
		out.FailDays, out.AttemptsToday = summary.FailDays, summary.AttemptsToday
		if summary.StoppedAt != nil && item.domainConfig.AutoRequestCertificate {
			out.Status, out.StopReason = "stopped", summary.StopReason
		}
		if summary.LastOrder != nil {
			out.LatestOrder = &management.ResourceACMEOrderRef{ID: management.ID(summary.LastOrder.ID), Status: summary.LastOrder.Status, UpdatedAt: summary.LastOrder.UpdatedAt.Unix()}
		}
	}
	return out
}

type resourceCatalogFilter struct {
	host               string
	status             string
	websiteAssociation string
}

func resourceCatalogFilterFromQuery(input management.ResourceCatalogQuery) resourceCatalogFilter {
	host := strings.ToLower(strings.TrimSpace(input.Host))
	return resourceCatalogFilter{
		host:               host,
		status:             strings.ToLower(strings.TrimSpace(input.Status)),
		websiteAssociation: strings.ToLower(strings.TrimSpace(input.WebsiteAssociation)),
	}
}

func resourceHostMatchesCatalogFilter(item *resourceHost, filter resourceCatalogFilter) bool {
	if filter.host != "" && !strings.Contains(strings.ToLower(item.name), filter.host) {
		return false
	}
	switch filter.status {
	case "managed":
		if !item.managed {
			return false
		}
	case "unmanaged":
		if item.managed {
			return false
		}
	}
	switch filter.websiteAssociation {
	case "associated":
		if len(item.websites) == 0 {
			return false
		}
	case "unassociated":
		if len(item.websites) != 0 {
			return false
		}
	}
	return true
}

func (p *Postgres) ResourceCatalog(ctx context.Context, input management.ResourceCatalogQuery) (management.ResourceCatalog, error) {
	items, err := p.resourceHosts(ctx, nil)
	if err != nil {
		return management.ResourceCatalog{}, err
	}
	filter := resourceCatalogFilterFromQuery(input)
	managed, unmanaged := []management.ResourceHost{}, []management.ResourceHost{}
	for _, item := range items {
		if !resourceHostMatchesCatalogFilter(item, filter) {
			continue
		}
		if item.managed {
			managed = append(managed, resourceHostDTO(item))
		} else {
			unmanaged = append(unmanaged, resourceHostDTO(item))
		}
	}
	sortResourceHosts(managed)
	sortResourceHosts(unmanaged)
	all := append(managed, unmanaged...)
	page, err := management.NormalizePageRequest(input.PageRequest)
	if err != nil {
		return management.ResourceCatalog{}, err
	}
	paging, offset, err := management.PageWindow(page, int64(len(all)))
	if err != nil {
		return management.ResourceCatalog{}, err
	}
	start := int(offset)
	end := min(start+page.PageSize, len(all))
	return management.ResourceCatalog{Records: all[start:end], Paging: paging, ManagedTotal: len(managed), UnmanagedTotal: len(unmanaged)}, nil
}

func sortResourceHosts(items []management.ResourceHost) {
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].LastActivity, items[j].LastActivity
		if left != right {
			return left > right
		}
		return items[i].Host < items[j].Host
	})
}

func (p *Postgres) DomainResourceView(ctx context.Context, input management.DomainResourceViewQuery) (management.DomainResourceView, error) {
	host := normalizeResourceHost(input.Host)
	if host == "" {
		return management.DomainResourceView{}, fmt.Errorf("host is required")
	}
	items, err := p.resourceHosts(ctx, []string{host})
	if err != nil {
		return management.DomainResourceView{}, err
	}
	item := items[host]
	if item == nil {
		item = &resourceHost{name: host, websites: []resourceWebsiteRef{}}
	}
	overview, series, err := p.resourceAnalytics(ctx, input.ResourceViewQuery, []string{host})
	if err != nil {
		return management.DomainResourceView{}, err
	}
	return management.DomainResourceView{Host: resourceHostDTO(item), Overview: overview, Timeseries: series}, nil
}

func (p *Postgres) WebsiteResourceView(ctx context.Context, input management.WebsiteResourceViewQuery) (management.WebsiteResourceView, error) {
	website, err := p.GetWebsite(ctx, input.ID)
	if err != nil {
		return management.WebsiteResourceView{}, err
	}
	hosts := normalizedWebsiteHosts(website)
	items := map[string]*resourceHost{}
	if len(hosts) > 0 {
		// An empty (non-nil) filter means "match everything" inside resourceHosts,
		// so only call it when there's an actual host list to scope the query to.
		items, err = p.resourceHosts(ctx, hosts)
		if err != nil {
			return management.WebsiteResourceView{}, err
		}
	}
	hostItems := make([]management.ResourceHost, 0, len(hosts))
	for _, host := range hosts {
		if item := items[host]; item != nil {
			hostItems = append(hostItems, resourceHostDTO(item))
		} else {
			hostItems = append(hostItems, resourceHostDTO(&resourceHost{name: host, websites: []resourceWebsiteRef{}}))
		}
	}
	overview, series, err := p.resourceAnalytics(ctx, input.ResourceViewQuery, hosts)
	if err != nil {
		return management.WebsiteResourceView{}, err
	}
	return management.WebsiteResourceView{Website: website, Hosts: hostItems, Overview: overview, Timeseries: series}, nil
}

func (p *Postgres) resourceAnalytics(ctx context.Context, input management.ResourceViewQuery, domains []string) (management.AnalyticsSubjectOverview, management.AnalyticsSubjectTimeseries, error) {
	now, err := p.databaseNow(ctx)
	if err != nil {
		return management.AnalyticsSubjectOverview{}, management.AnalyticsSubjectTimeseries{}, err
	}
	window, err := analyticsWindowForRange(now, input.From, input.To)
	if err != nil {
		return management.AnalyticsSubjectOverview{}, management.AnalyticsSubjectTimeseries{}, err
	}
	rows := []analyticsRow{}
	source := analyticsReportSource
	if len(domains) > 0 {
		var err error
		rows, source, err = p.analyticsRows(ctx, now, window, domains)
		if err != nil {
			return management.AnalyticsSubjectOverview{}, management.AnalyticsSubjectTimeseries{}, err
		}
	}
	total := combineAnalyticsRows(rows)
	latest := latestAnalyticsTimestamp(rows)
	lag := int64(0)
	if latest > 0 {
		lag = max(now.Unix()-latest, 0)
	}
	overview := management.AnalyticsSubjectOverview{AsOf: now.Unix(), From: window.begin.Unix(), To: window.end.Unix(), Grain: window.grain, DataLagSeconds: lag, HasData: latest > 0, IsPartial: total.partial, Kpis: analyticsKpis(total, window.end.Sub(window.begin)), ErrorBreakdown: metricItemsTyped(total.errors), StatusBreakdown: metricItemsTyped(statusFamilyCounts(total.statuses, total.requests)), DurationBuckets: metricItemsTyped(total.durationBuckets), UserAgents: analyticsUserAgentsTyped(total), Coverage: analyticsCoverage(window, rows), Source: source}
	series := management.AnalyticsSubjectTimeseries{AsOf: now.Unix(), From: window.begin.Unix(), To: window.end.Unix(), Grain: window.grain, Domains: domains, Points: denseAnalyticsPointsTyped(rows, window)}
	return overview, series, nil
}

func (p *Postgres) analyticsRowsForDomains(ctx context.Context, cycle int, from, to int64, domains []string) ([]analyticsRow, error) {
	if len(domains) == 0 {
		return []analyticsRow{}, nil
	}
	rows, err := p.pool.Query(ctx, `SELECT resource_uuid,report_ts,request_qty,inbound_bytes,outbound_bytes,status_code,error_counts,latency_histogram,duration,is_partial
		FROM resource_report WHERE cycle_type=$1 AND report_ts >= $2 AND report_ts <= $3 AND resource_uuid = ANY($4) ORDER BY report_ts`, cycle, from, to, domains)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []analyticsRow{}
	for rows.Next() {
		var domain string
		var timestamp, requests, inbound, outbound int64
		var status, errors, histogram, duration []byte
		var partial bool
		if err := rows.Scan(&domain, &timestamp, &requests, &inbound, &outbound, &status, &errors, &histogram, &duration, &partial); err != nil {
			return nil, err
		}
		durations := decodeReportCounts(duration)
		result = append(result, analyticsRow{domain: domain, timestamp: timestamp, metrics: analyticsMetrics{requests: requests, inbound: inbound, outbound: outbound, durationSum: durations["avg"] * requests, durationMax: durations["max"], statuses: decodeReportCounts(status), errors: decodeReportCounts(errors), histogram: decodeReportCounts(histogram), partial: partial}})
	}
	return result, rows.Err()
}
