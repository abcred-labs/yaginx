package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"yaginx-gateway/internal/management"

	"github.com/jackc/pgx/v5"
)

// Website configuration: the console's write model and its list/search reads.
// The specifications column is JSONB, which is why so much of this file is
// map plumbing rather than struct scanning.

func ptr(value string) *string { return &value }

func (p *Postgres) UpsertWebsite(ctx context.Context, value management.WebsiteUpsertRequest) (management.Website, error) {
	if value.Name == nil {
		return management.Website{}, fmt.Errorf("website name is required")
	}
	name := strings.TrimSpace(*value.Name)
	if name == "" {
		return management.Website{}, fmt.Errorf("website name is required")
	}
	if err := validateWebsiteIPAccess(value.IPAccessMode, value.IPAccessCIDRs); err != nil {
		return management.Website{}, err
	}
	if err := validateWebsiteHostBindings(value.HostBindings); err != nil {
		return management.Website{}, err
	}
	if err := validateWebsiteUpstreamPools(value.UpstreamPools); err != nil {
		return management.Website{}, err
	}
	if err := validateWebsiteRules(value.ProxyRules, value.UpstreamPools); err != nil {
		return management.Website{}, err
	}
	if err := value.RequestHistory.Normalize(); err != nil {
		return management.Website{}, err
	}
	if _, err := management.ParseWebsiteResponseHeaderTimeout(value.ResponseHeaderTimeout); err != nil {
		return management.Website{}, err
	}
	value.ResponseHeaderTimeout = strings.TrimSpace(value.ResponseHeaderTimeout)
	value.IPAccessMode = strings.ToLower(strings.TrimSpace(value.IPAccessMode))
	if value.IPAccessMode == "" {
		value.IPAccessMode = "disabled"
	}
	for index := range value.IPAccessCIDRs {
		value.IPAccessCIDRs[index] = strings.TrimSpace(value.IPAccessCIDRs[index])
	}
	value = normalizeWebsiteDomainValues(value)
	spec, err := json.Marshal(value)
	if err != nil {
		return management.Website{}, err
	}
	var specObject map[string]any
	if err := json.Unmarshal(spec, &specObject); err != nil {
		return management.Website{}, err
	}
	specObject["schemaVersion"] = 2
	spec, err = json.Marshal(specObject)
	if err != nil {
		return management.Website{}, err
	}
	hosts, err := json.Marshal(value.Hosts)
	if err != nil {
		return management.Website{}, err
	}
	rules, err := json.Marshal(value.ProxyRules)
	if err != nil {
		return management.Website{}, err
	}
	transforms, err := json.Marshal(value.ProxyTransforms)
	if err != nil {
		return management.Website{}, err
	}
	responses, err := json.Marshal(value.SimpleResponses)
	if err != nil {
		return management.Website{}, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return management.Website{}, err
	}
	defer tx.Rollback(ctx)
	var previousHosts []string
	var outID int64
	if value.ID != nil {
		var previousSpec []byte
		if err := tx.QueryRow(ctx, "SELECT specifications FROM website WHERE id=$1 FOR UPDATE", int64(*value.ID)).Scan(&previousSpec); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return management.Website{}, err
		}
		if len(previousSpec) > 0 {
			var previousRequest management.WebsiteUpsertRequest
			if err := json.Unmarshal(previousSpec, &previousRequest); err != nil {
				return management.Website{}, err
			}
			previousHosts = normalizedWebsiteHosts(management.Website{WebsiteUpsertRequest: previousRequest})
		}
		err = tx.QueryRow(ctx, "UPDATE website SET name=$2,specifications=$3,hosts=$4,proxy_rules=$5,proxy_transforms=$6,simple_responses=$7,data_schema_version=2,update_time=clock_timestamp() WHERE id=$1 RETURNING id", int64(*value.ID), name, spec, hosts, rules, transforms, responses).Scan(&outID)
	} else {
		err = tx.QueryRow(ctx, "INSERT INTO website(name,specifications,hosts,proxy_rules,proxy_transforms,simple_responses,data_schema_version,create_time,update_time) VALUES($1,$2,$3,$4,$5,$6,2,clock_timestamp(),clock_timestamp()) RETURNING id", name, spec, hosts, rules, transforms, responses).Scan(&outID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return management.Website{}, management.ErrNotFound
	}
	if err != nil {
		return management.Website{}, err
	}
	if err := syncWebsiteDomains(ctx, tx, value); err != nil {
		return management.Website{}, err
	}
	if err := disableRemovedWebsiteHosts(ctx, tx, previousHosts, normalizedWebsiteHosts(management.Website{WebsiteUpsertRequest: value})); err != nil {
		return management.Website{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return management.Website{}, err
	}
	return p.GetWebsite(ctx, strconv.FormatInt(outID, 10))
}

func validateWebsiteRules(rules []management.WebsiteProxyRule, pools []management.UpstreamPool) error {
	poolIDs := map[string]bool{}
	for _, pool := range pools {
		poolIDs[strings.TrimSpace(pool.ID)] = true
	}
	fallbacks := 0
	for _, rule := range rules {
		if rule.IsFallback {
			if !rule.IsEnabled {
				continue
			}
			fallbacks++
			if rule.PathPattern != nil && strings.TrimSpace(*rule.PathPattern) != "" {
				return fmt.Errorf("fallback rule must not have a path pattern")
			}
		}
		if strings.TrimSpace(rule.UpstreamPoolID) != "" && !poolIDs[strings.TrimSpace(rule.UpstreamPoolID)] {
			return fmt.Errorf("rule references unknown upstream pool %q", rule.UpstreamPoolID)
		}
		if strings.TrimSpace(rule.UpstreamPoolID) == "" && (rule.Destination == nil || strings.TrimSpace(*rule.Destination) == "") && rule.IsEnabled {
			return fmt.Errorf("enabled rule requires an upstream pool or destination")
		}
	}
	if fallbacks > 1 {
		return fmt.Errorf("website can have only one enabled fallback rule")
	}
	return nil
}

func validateWebsiteUpstreamPools(pools []management.UpstreamPool) error {
	seen := map[string]bool{}
	for _, pool := range pools {
		id := strings.TrimSpace(pool.ID)
		if id == "" {
			return fmt.Errorf("upstream pool id is required")
		}
		if seen[id] {
			return fmt.Errorf("duplicate upstream pool %q", id)
		}
		seen[id] = true
		strategy := strings.TrimSpace(pool.Strategy)
		if strategy == "" {
			strategy = "weightedRoundRobin"
		}
		switch strategy {
		case "weightedRoundRobin", "random", "leastConnections", "failover":
		default:
			return fmt.Errorf("invalid upstream pool strategy %q", pool.Strategy)
		}
		for _, endpoint := range pool.Endpoints {
			if strings.TrimSpace(endpoint.Address) == "" {
				return fmt.Errorf("upstream pool %q contains an endpoint without address", id)
			}
			if endpoint.Weight < 1 {
				return fmt.Errorf("upstream pool %q endpoint weight must be at least 1", id)
			}
		}
	}
	return nil
}

func validateWebsiteHostBindings(bindings []management.WebsiteHost) error {
	seen := map[string]bool{}
	primary := 0
	primaryHost := ""
	for _, binding := range bindings {
		host := normalizeResourceHost(binding.Hostname)
		if host == "" {
			return fmt.Errorf("website host is required")
		}
		if seen[host] {
			return fmt.Errorf("duplicate website host %q", host)
		}
		seen[host] = true
		role := strings.ToLower(strings.TrimSpace(binding.Role))
		mode := strings.ToLower(strings.TrimSpace(binding.AccessMode))
		if role == "" {
			role = "alias"
		}
		if role != "primary" && role != "alias" {
			return fmt.Errorf("invalid website host role %q", binding.Role)
		}
		if role == "primary" {
			primary++
			primaryHost = host
			if mode != "" && mode != "proxy" {
				return fmt.Errorf("primary host %q must use proxy access", host)
			}
		}
		if mode == "" {
			mode = "proxy"
		}
		if mode != "proxy" && mode != "redirect" {
			return fmt.Errorf("invalid website host access mode %q", binding.AccessMode)
		}
		certificateMode := strings.ToLower(strings.TrimSpace(binding.CertificateMode))
		if certificateMode != "" && certificateMode != "existing" && certificateMode != "request" && certificateMode != "manual" {
			return fmt.Errorf("invalid website host certificate mode %q", binding.CertificateMode)
		}
		if mode == "redirect" {
			target := primaryHost
			if binding.RedirectTargetHost != nil && normalizeResourceHost(*binding.RedirectTargetHost) != "" {
				target = normalizeResourceHost(*binding.RedirectTargetHost)
			}
			if target == host {
				return fmt.Errorf("website host %q cannot redirect to itself", host)
			}
		}
		if binding.RedirectStatusCode != 0 && binding.RedirectStatusCode != 301 && binding.RedirectStatusCode != 308 {
			return fmt.Errorf("website host %q redirect status must be 301 or 308", host)
		}
	}
	if primary > 1 {
		return fmt.Errorf("website can have only one primary host")
	}
	if primary == 1 {
		for _, binding := range bindings {
			if strings.EqualFold(strings.TrimSpace(binding.AccessMode), "redirect") && binding.RedirectTargetHost != nil && normalizeResourceHost(*binding.RedirectTargetHost) != primaryHost {
				return fmt.Errorf("redirect target %q must be the primary host %q", *binding.RedirectTargetHost, primaryHost)
			}
		}
	}
	return nil
}

func normalizeWebsiteDomainValues(value management.WebsiteUpsertRequest) management.WebsiteUpsertRequest {
	if value.HostBindings != nil {
		// The new binding list is authoritative, including an explicit empty list.
		value.Hosts = nil
	}
	value.DefaultHost = normalizeDomainPointer(value.DefaultHost)
	seen := make(map[string]struct{}, len(value.Hosts))
	hosts := make([]string, 0, len(value.Hosts))
	for _, raw := range value.Hosts {
		host := normalizeResourceHost(raw)
		if host == "" {
			continue
		}
		if _, exists := seen[host]; exists {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	value.Hosts = hosts
	for index := range value.HostBindings {
		binding := &value.HostBindings[index]
		binding.Hostname = normalizeResourceHost(binding.Hostname)
		if binding.Role == "" {
			binding.Role = "alias"
		}
		if binding.AccessMode == "" {
			binding.AccessMode = "proxy"
		}
		// "existing" is a legacy value kept readable for old rows; the console
		// now only ever sends "request" (auto-renew on) or "manual" (off).
		if binding.CertificateMode == "" || binding.CertificateMode == "existing" {
			binding.CertificateMode = "manual"
		}
		if binding.VerificationStatus == "" {
			binding.VerificationStatus = "unknown"
		}
		if binding.RedirectStatusCode != 301 && binding.RedirectStatusCode != 308 {
			binding.RedirectStatusCode = 308
		}
	}
	for index := range value.UpstreamPools {
		pool := &value.UpstreamPools[index]
		pool.ID = strings.TrimSpace(pool.ID)
		pool.Strategy = strings.TrimSpace(pool.Strategy)
		if pool.Strategy == "" {
			pool.Strategy = "weightedRoundRobin"
		}
		for endpointIndex := range pool.Endpoints {
			if pool.Endpoints[endpointIndex].Weight < 1 {
				pool.Endpoints[endpointIndex].Weight = 1
			}
			pool.Endpoints[endpointIndex].Address = strings.TrimSpace(pool.Endpoints[endpointIndex].Address)
		}
	}
	return value
}

func validateWebsiteIPAccess(mode string, cidrs []string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "disabled"
	}
	if mode != "disabled" && mode != "blacklist" && mode != "whitelist" {
		return fmt.Errorf("invalid website IP access mode %q", mode)
	}
	for _, raw := range cidrs {
		value := strings.TrimSpace(raw)
		if value == "" {
			return fmt.Errorf("website IP access CIDR must not be empty")
		}
		if _, err := netip.ParseAddr(value); err == nil {
			continue
		}
		if _, err := netip.ParsePrefix(value); err != nil {
			return fmt.Errorf("invalid website IP access CIDR %q", raw)
		}
	}
	return nil
}

func stringSliceValue(v map[string]any, key string) ([]string, error) {
	raw, ok := v[key]
	if !ok || raw == nil {
		return []string{}, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s must contain strings", key)
		}
		out = append(out, strings.TrimSpace(text))
	}
	return out, nil
}

func websiteRequestHistoryValue(spec map[string]any) (string, *float64, error) {
	mode := "inherit"
	var rate *float64
	raw, ok := spec["requestHistory"]
	if !ok || raw == nil {
		return mode, nil, nil
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return "", nil, fmt.Errorf("requestHistory must be an object")
	}
	if value, ok := object["mode"]; ok && value != nil {
		text, ok := value.(string)
		if !ok {
			return "", nil, fmt.Errorf("requestHistory.mode must be a string")
		}
		mode = text
	}
	if value, ok := object["successSamplingRate"]; ok && value != nil {
		number, ok := value.(float64)
		if !ok {
			return "", nil, fmt.Errorf("requestHistory.successSamplingRate must be a number")
		}
		rate = &number
	}
	policy := &management.WebsiteRequestHistoryPolicy{Mode: mode, SuccessSamplingRate: rate}
	if err := policy.Normalize(); err != nil {
		return "", nil, err
	}
	return policy.Mode, policy.SuccessSamplingRate, nil
}

func normalizeDomainPointer(value *string) *string {
	if value == nil {
		return nil
	}
	host := normalizeResourceHost(*value)
	return &host
}

func syncWebsiteDomains(ctx context.Context, tx pgx.Tx, value management.WebsiteUpsertRequest) error {
	website := management.Website{WebsiteUpsertRequest: value}
	for _, host := range normalizedWebsiteHosts(website) {
		if _, err := tx.Exec(ctx, `INSERT INTO web_domain(name,is_use_free_cert,is_verified,free_cert_message)
VALUES($1,false,false,'') ON CONFLICT (name) WHERE name IS NOT NULL DO NOTHING`, host); err != nil {
			return err
		}
	}
	for _, binding := range website.HostBindings {
		if host := normalizeResourceHost(binding.Hostname); host != "" {
			requestCertificate := strings.EqualFold(strings.TrimSpace(binding.CertificateMode), "request")
			if requestCertificate {
				// Turning auto-request on (off -> on) clears a stopped renewal;
				// re-saving an already-enabled host leaves it stopped.
				if _, err := tx.Exec(ctx, `DELETE FROM gateway_acme_auto_state WHERE domain_name=$1
AND EXISTS (SELECT 1 FROM web_domain WHERE name=$1 AND NOT is_use_free_cert)`, host); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `INSERT INTO web_domain(name,is_use_free_cert,is_verified,free_cert_message)
VALUES($1,$2,false,'') ON CONFLICT (name) WHERE name IS NOT NULL DO UPDATE SET is_use_free_cert=EXCLUDED.is_use_free_cert,free_cert_message=CASE WHEN EXCLUDED.is_use_free_cert THEN NULL ELSE web_domain.free_cert_message END`, host, requestCertificate); err != nil {
				return err
			}
		}
	}
	return nil
}

// disableRemovedWebsiteHosts turns off auto-renewal and cancels any in-flight
// ACME order for a hostname that a website save no longer references (e.g.
// the operator typed a hostname wrong and corrected it, or removed a host
// binding). Without this, the old hostname keeps retrying ACME indefinitely
// for a host nobody can see or manage anymore.
func disableRemovedWebsiteHosts(ctx context.Context, tx pgx.Tx, previousHosts, currentHosts []string) error {
	current := make(map[string]bool, len(currentHosts))
	for _, host := range currentHosts {
		current[host] = true
	}
	for _, host := range previousHosts {
		if current[host] {
			continue
		}
		if _, err := tx.Exec(ctx, "UPDATE web_domain SET is_use_free_cert=false WHERE name=$1", host); err != nil {
			return err
		}
		if err := cancelPendingACMEOrders(ctx, tx, host, 0, "host removed from website"); err != nil {
			return err
		}
	}
	return nil
}

func rejectLegacyWebsiteFields(v map[string]any) error {
	normalizeLegacyWebsiteMap(v)
	return nil
}

func (p *Postgres) ListWebsites(ctx context.Context) ([]management.Website, error) {
	return p.listWebsites(ctx, management.WebsiteSearchFilter{})
}

func (p *Postgres) SearchWebsites(ctx context.Context, input management.WebsiteSearchRequest) (management.Page[management.Website], error) {
	filter := normalizeWebsiteSearchFilter(input.WebsiteSearchFilter)
	query, args, where := websiteSearchQuery(filter)
	var total int64
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM website w"+where, args...).Scan(&total); err != nil {
		return management.Page[management.Website]{}, fmt.Errorf("website search count: %w", err)
	}
	paging, offset, err := management.PageWindow(input.PageRequest, total)
	if err != nil {
		return management.Page[management.Website]{}, err
	}
	queryArgs := append(append([]any{}, args...), paging.PageSize, offset)
	rows, err := p.pool.Query(ctx, query+where+" ORDER BY w.id LIMIT $"+strconv.Itoa(len(args)+1)+" OFFSET $"+strconv.Itoa(len(args)+2), queryArgs...)
	if err != nil {
		return management.Page[management.Website]{}, fmt.Errorf("website search page: %w", err)
	}
	defer rows.Close()
	records, err := scanWebsiteRows(rows)
	if err != nil {
		return management.Page[management.Website]{}, err
	}
	return management.Page[management.Website]{Records: records, Paging: paging}, nil
}

func (p *Postgres) listWebsites(ctx context.Context, filter management.WebsiteSearchFilter) ([]management.Website, error) {
	query, args, where := websiteSearchQuery(filter)
	rows, err := p.pool.Query(ctx, query+where+" ORDER BY w.id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWebsiteRows(rows)
}

func normalizeWebsiteSearchFilter(filter management.WebsiteSearchFilter) management.WebsiteSearchFilter {
	filter.Domain = strings.TrimSpace(filter.Domain)
	filter.Upstream = strings.TrimSpace(filter.Upstream)
	return filter
}

// jsonArrayOrEmpty wraps a jsonb expression so that NULL or non-array values
// iterate as an empty array instead of failing jsonb_array_elements.
func jsonArrayOrEmpty(expr string) string {
	return "CASE WHEN jsonb_typeof(" + expr + ") = 'array' THEN " + expr + " ELSE '[]'::jsonb END"
}

func websiteSearchQuery(filter management.WebsiteSearchFilter) (string, []any, string) {
	filter = normalizeWebsiteSearchFilter(filter)
	query := websiteResponseQuery
	args := []any{}
	where := []string{}
	if filter.Domain != "" {
		args = append(args, "%"+filter.Domain+"%")
		placeholder := "CAST($" + strconv.Itoa(len(args)) + " AS text)"
		where = append(where, `(COALESCE(w.specifications->>'defaultHost', '') ILIKE `+placeholder+`
OR EXISTS (
    SELECT 1
    FROM jsonb_array_elements_text(`+jsonArrayOrEmpty("w.hosts")+`) AS host(value)
    WHERE host.value ILIKE `+placeholder+`
)
OR EXISTS (
    SELECT 1
    FROM jsonb_array_elements(`+jsonArrayOrEmpty("w.specifications->'hostBindings'")+`) AS binding
    WHERE COALESCE(binding->>'hostname', '') ILIKE `+placeholder+`
))`)
	}
	if filter.Upstream != "" {
		args = append(args, "%"+filter.Upstream+"%")
		placeholder := "CAST($" + strconv.Itoa(len(args)) + " AS text)"
		// 旧版只有 defaultDestination；新版的转发规则直连地址和上游池端点也要能搜到。
		where = append(where, `(COALESCE(w.specifications->>'defaultDestination', '') ILIKE `+placeholder+`
OR EXISTS (
    SELECT 1
    FROM jsonb_array_elements(`+jsonArrayOrEmpty("w.proxy_rules")+`) AS rule
    WHERE COALESCE(rule->>'destination', '') ILIKE `+placeholder+`
)
OR EXISTS (
    SELECT 1
    FROM jsonb_array_elements(`+jsonArrayOrEmpty("w.specifications->'upstreamPools'")+`) AS pool,
         jsonb_array_elements(`+jsonArrayOrEmpty("pool->'endpoints'")+`) AS endpoint
    WHERE COALESCE(endpoint->>'address', '') ILIKE `+placeholder+`
))`)
	}
	if filter.HasCertificate != nil {
		if *filter.HasCertificate {
			where = append(where, websiteHasCertificateQuery)
		} else {
			where = append(where, "NOT ("+websiteHasCertificateQuery+")")
		}
	}
	if len(where) > 0 {
		return query, args, " WHERE " + strings.Join(where, " AND ")
	}
	return query, args, ""
}

func scanWebsiteRows(rows pgx.Rows) ([]management.Website, error) {
	out := []management.Website{}
	for rows.Next() {
		var id int64
		var name string
		var spec []byte
		var hasCertificate bool
		if err := rows.Scan(&id, &name, &spec, &hasCertificate); err != nil {
			return nil, err
		}
		item, err := websiteMap(id, name, spec, hasCertificate)
		if err != nil {
			return nil, err
		}
		website, err := websiteFromMap(item)
		if err != nil {
			return nil, err
		}
		out = append(out, website)
	}
	return out, rows.Err()
}

func (p *Postgres) GetWebsite(ctx context.Context, key string) (management.Website, error) {
	var id int64
	var name string
	var spec, hosts, rules, transforms, responses []byte
	query := websiteDetailQuery + " WHERE w.id=$1"
	arg := any(key)
	if _, err := strconv.ParseInt(key, 10, 64); err != nil {
		query = websiteDetailQuery + " WHERE w.name=$1"
	}
	var hasCertificate bool
	err := p.pool.QueryRow(ctx, query, arg).Scan(&id, &name, &spec, &hosts, &rules, &transforms, &responses, &hasCertificate)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.Website{}, management.ErrNotFound
	}
	if err != nil {
		return management.Website{}, err
	}
	item, err := websiteMapWithCollections(id, name, spec, hosts, rules, transforms, responses, hasCertificate)
	if err != nil {
		return management.Website{}, err
	}
	return websiteFromMap(item)
}

func (p *Postgres) DeleteWebsite(ctx context.Context, key string) error {
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil {
		return err
	}
	result, err := p.DeleteWebsites(ctx, []int64{id})
	if err != nil {
		return err
	}
	if len(result.DeletedIDs) == 0 {
		return management.ErrNotFound
	}
	return nil
}

func (p *Postgres) DeleteWebsites(ctx context.Context, ids []int64) (management.WebsiteBatchDeleteResult, error) {
	result := management.WebsiteBatchDeleteResult{
		DeletedIDs:  make([]management.ID, 0, len(ids)),
		NotFoundIDs: make([]management.ID, 0, len(ids)),
	}
	if len(ids) == 0 {
		return result, fmt.Errorf("at least one website id is required")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, "DELETE FROM website WHERE id = ANY($1) RETURNING id, COALESCE(specifications->>'defaultHost',''), hosts", ids)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	deleted := make(map[int64]struct{}, len(ids))
	var hostNames []string
	for rows.Next() {
		var id int64
		var defaultHost string
		var rawHosts []byte
		if err := rows.Scan(&id, &defaultHost, &rawHosts); err != nil {
			return result, err
		}
		deleted[id] = struct{}{}
		hostNames = append(hostNames, defaultHost)
		var list []string
		if json.Unmarshal(rawHosts, &list) == nil {
			hostNames = append(hostNames, list...)
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	rows.Close()
	if err := deleteOrphanedACMEOrders(ctx, tx, hostNames); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	for _, id := range ids {
		if _, exists := deleted[id]; exists {
			result.DeletedIDs = append(result.DeletedIDs, management.ID(id))
		} else {
			result.NotFoundIDs = append(result.NotFoundIDs, management.ID(id))
		}
	}
	return result, nil
}

// deleteOrphanedACMEOrders removes ACME orders (and, via cascade, their logs)
// for hosts that were bound to just-deleted websites and are no longer bound
// to any remaining website.
func deleteOrphanedACMEOrders(ctx context.Context, tx pgx.Tx, hosts []string) error {
	normalized := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = strings.ToLower(strings.TrimRight(strings.TrimSpace(host), "."))
		if host != "" {
			normalized = append(normalized, host)
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `DELETE FROM gateway_certificate_order o
WHERE o.domain_name = ANY($1)
  AND NOT EXISTS (
    SELECT 1 FROM website w
    WHERE lower(trim(trailing '.' from coalesce(w.specifications->>'defaultHost',''))) = o.domain_name
       OR EXISTS (
            SELECT 1 FROM jsonb_array_elements_text(
                CASE WHEN jsonb_typeof(w.hosts) = 'array' THEN w.hosts ELSE '[]'::jsonb END
            ) AS host(value)
            WHERE lower(trim(trailing '.' from host.value)) = o.domain_name
       )
  )`, normalized)
	return err
}

const websiteHasCertificateQuery = `EXISTS(
    SELECT 1 FROM gateway_certificate c
    WHERE c.status='active'
      AND c.is_active=true
      AND c.not_after > clock_timestamp()
      AND COALESCE(w.specifications->>'defaultHost', '') = ANY(c.dns_names)
)`

const websiteResponseQuery = `SELECT w.id,w.name,w.specifications,
` + websiteHasCertificateQuery + ` AS is_have_ssl_cert
FROM website w`

const websiteDetailQuery = `SELECT w.id,w.name,w.specifications,w.hosts,w.proxy_rules,w.proxy_transforms,w.simple_responses,
` + websiteHasCertificateQuery + ` AS is_have_ssl_cert
FROM website w`

func websiteMap(id int64, name string, spec []byte, hasCertificate bool) (map[string]any, error) {
	normalized, err := decodeWebsiteSpecifications(spec)
	if err != nil {
		return nil, fmt.Errorf("website %d specifications: %w", id, err)
	}
	return websiteMapFromSpecifications(id, name, normalized, hasCertificate), nil
}

// websiteMapWithCollections makes the dedicated website columns authoritative
// for the editor.
func websiteMapWithCollections(id int64, name string, spec, hosts, rules, transforms, responses []byte, hasCertificate bool) (map[string]any, error) {
	out, err := websiteMap(id, name, spec, hasCertificate)
	if err != nil {
		return nil, err
	}
	specifications, err := decodeWebsiteSpecifications(spec)
	if err != nil {
		return nil, fmt.Errorf("website %d specifications: %w", id, err)
	}
	for _, collection := range []struct {
		field string
		raw   []byte
	}{
		{field: "hosts", raw: hosts},
		{field: "proxyRules", raw: rules},
		{field: "proxyTransforms", raw: transforms},
		{field: "simpleResponses", raw: responses},
	} {
		value, exists, err := websiteCollectionValue(specifications, collection.field, collection.raw)
		if err != nil {
			return nil, fmt.Errorf("website %d %s: %w", id, collection.field, err)
		}
		if !exists {
			continue
		}
		normalizedCollection := map[string]any{collection.field: value}
		normalizeLegacyWebsiteMap(normalizedCollection)
		value = normalizedCollection[collection.field]
		// Historical empty transform columns used an object while the v2 editor
		// contract exposes transforms as a key/value array. This normalization is
		// limited to the editor response; snapshot decoding retains its object
		// representation.
		if collection.field == "proxyTransforms" {
			if object, ok := value.(map[string]any); ok {
				if len(object) == 0 {
					value = []any{}
				} else {
					items := make([]any, 0, len(object))
					for key, item := range object {
						items = append(items, map[string]any{"key": key, "value": fmt.Sprint(item)})
					}
					value = items
				}
			}
		}
		if collection.field == "proxyRules" {
			value = normalizeLegacyRuleIDs(value)
		}
		out[collection.field] = value
	}
	return out, nil
}

func decodeWebsiteCollection(raw []byte) (any, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false, nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, false, err
	}
	if value == nil {
		return nil, false, nil
	}
	return value, true, nil
}

// normalizeLegacyRuleIDs upgrades numeric rule IDs stored in pre-string JSON
// columns before the strict management model decodes them. UseNumber above is
// important here: converting a large legacy ID through float64 would already
// lose the digits we are trying to preserve.
func normalizeLegacyRuleIDs(value any) any {
	switch typed := value.(type) {
	case []any:
		for index := range typed {
			typed[index] = normalizeLegacyRuleIDs(typed[index])
		}
	case map[string]any:
		for key, item := range typed {
			if key == "ruleId" {
				switch number := item.(type) {
				case json.Number:
					if parsed, err := strconv.ParseInt(string(number), 10, 64); err == nil {
						typed[key] = strconv.FormatInt(parsed, 10)
					}
				case float64:
					if parsed := int64(number); float64(parsed) == number {
						typed[key] = strconv.FormatInt(parsed, 10)
					}
				}
				continue
			}
			typed[key] = normalizeLegacyRuleIDs(item)
		}
	}
	return value
}

// websiteCollectionValue gives dedicated columns precedence and falls back to
// the v2 specification copy only when a column is absent.
func websiteCollectionValue(specifications map[string]any, field string, raw []byte) (any, bool, error) {
	value, exists, err := decodeWebsiteCollection(raw)
	if err != nil || exists {
		return value, exists, err
	}
	value, exists = specifications[field]
	if !exists || value == nil {
		return nil, false, nil
	}
	return value, true, nil
}

func websiteMapFromSpecifications(id int64, name string, specifications map[string]any, hasCertificate bool) map[string]any {
	out := make(map[string]any, len(specifications)+3)
	for key, value := range specifications {
		out[key] = value
	}
	out["id"] = strconv.FormatInt(id, 10)
	out["name"] = name
	out["hasCertificate"] = hasCertificate
	out["isMaintenanceMode"] = boolValue(specifications, "isMaintenanceMode")
	for _, field := range []string{"hosts", "proxyRules", "proxyTransforms", "simpleResponses"} {
		if _, ok := out[field]; !ok {
			out[field] = []any{}
		}
	}
	out["proxyRules"] = normalizeLegacyRuleIDs(out["proxyRules"])
	return out
}

func websiteFromMap(value map[string]any) (management.Website, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return management.Website{}, err
	}
	var website management.Website
	if err := json.Unmarshal(encoded, &website); err != nil {
		return management.Website{}, err
	}
	return website, nil
}

func decodeWebsiteSpecifications(raw []byte) (map[string]any, error) {
	specifications := map[string]any{}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return specifications, nil
	}
	if err := json.Unmarshal(raw, &specifications); err != nil {
		return nil, err
	}
	if specifications == nil {
		specifications = map[string]any{}
	}
	if err := rejectLegacyWebsiteFields(specifications); err != nil {
		return nil, err
	}
	return specifications, nil
}

// normalizeLegacyWebsiteMap converts the JSON shape emitted by the old .NET
// management API into the v2 camelCase shape. It is deliberately in-memory:
// reads must never mutate PostgreSQL. The conversion is lossless for fields
// understood by the v2 contract and preserves unknown fields for validation.
func normalizeLegacyWebsiteMap(v map[string]any) {
	for old, next := range map[string]string{
		"DefaultHost": "defaultHost", "DefaultDestination": "defaultDestination",
		"DefaultDestinationHost": "defaultDestinationHost", "WebProxy": "webProxy",
		"AuthorizationPolicy": "authorizationPolicy", "IsWithOriginalHostHeader": "preserveOriginalHost",
		"IsAllowUnsafeSslCertificate": "allowInvalidTlsCertificate", "IsAutoRedirectHttp2Https": "redirectHttpToHttps",
		"IsMaintenanceMode": "isMaintenanceMode", "IsAllowAnySourceCors": "isAllowAnySourceCors",
		"Hosts": "hosts", "ProxyRules": "proxyRules", "ProxyTransforms": "proxyTransforms", "SimpleResponses": "simpleResponses",
	} {
		if value, ok := v[old]; ok {
			if _, exists := v[next]; !exists {
				v[next] = value
			}
			delete(v, old)
		}
	}
	for _, field := range []string{"proxyRules", "simpleResponses"} {
		if items, ok := v[field].([]any); ok {
			for _, item := range items {
				object, ok := item.(map[string]any)
				if !ok {
					continue
				}
				mapping := map[string]string{"Name": "name", "Description": "description", "RuleId": "ruleId", "IsEnabled": "isEnabled", "PathPattern": "pathPattern", "IsRegexMatchPattern": "isRegexMatchPattern", "Destination": "destination", "DestinationHost": "destinationHost", "RemovePrefix": "removePrefix", "Url": "path", "ContentType": "contentType", "StatusCode": "statusCode", "ResponseContent": "responseContent", "ExtraHeaders": "extraHeaders", "IsWithOriginalHostHeader": "preserveOriginalHost", "IsAllowUnsafeSslCertificate": "allowInvalidTlsCertificate"}
				for old, next := range mapping {
					if value, exists := object[old]; exists {
						if _, present := object[next]; !present {
							object[next] = value
						}
						delete(object, old)
					}
				}
				if headers, ok := object["extraHeaders"].([]any); ok {
					for _, header := range headers {
						if item, ok := header.(map[string]any); ok {
							if value, exists := item["Key"]; exists {
								item["key"] = value
								delete(item, "Key")
							}
							if value, exists := item["Value"]; exists {
								item["value"] = value
								delete(item, "Value")
							}
						}
					}
				}
				normalizeLegacyWebsiteMap(object)
			}
		}
	}
}
