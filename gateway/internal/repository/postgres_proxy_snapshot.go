package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"yaginx-gateway/internal/proxy"

	"github.com/jackc/pgx/v5"
)

// The proxy's view of configuration: the full snapshot it loads at startup and
// the incremental config events it applies while running.

// PruneConfigEvents retains events until they are both older than retention
// and acknowledged by every active data-plane node. Nodes silent past the
// configured horizon intentionally do not hold the watermark: they recover
// with a full authoritative snapshot rather than an indefinitely growing log.
func (p *Postgres) PruneConfigEvents(ctx context.Context, now time.Time, retention, inactiveAfter time.Duration) error {
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	if inactiveAfter <= 0 {
		inactiveAfter = 7 * 24 * time.Hour
	}
	_, err := p.pool.Exec(ctx, `WITH active_watermark AS (
    SELECT min(applied_revision) AS revision
    FROM gateway_node
    WHERE status IN ('active', 'draining')
      AND last_heartbeat_at >= $2
), safe_watermark AS (
    SELECT COALESCE((SELECT revision FROM active_watermark), (SELECT max(id) FROM gateway_config_event), 0) AS revision
)
DELETE FROM gateway_config_event event
USING safe_watermark
WHERE event.created_at < $1
  AND event.id <= safe_watermark.revision`, now.Add(-retention), now.Add(-inactiveAfter))
	return err
}

// LoadProxySnapshot is the database-to-runtime boundary. Invalid rows abort
// the entire candidate so callers retain their last-known-good snapshot.
func (p *Postgres) LoadProxySnapshot(ctx context.Context) (*proxy.Snapshot, error) {
	rows, err := p.pool.Query(ctx, "SELECT id,name,specifications,hosts,proxy_rules,proxy_transforms,simple_responses FROM website ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	snapshot := &proxy.Snapshot{}
	for rows.Next() {
		var id int64
		var name string
		var specifications, hosts, rules, transforms, simpleResponses []byte
		if err := rows.Scan(&id, &name, &specifications, &hosts, &rules, &transforms, &simpleResponses); err != nil {
			return nil, err
		}
		site, err := decodeProxySite(id, name, specifications, hosts, rules, transforms, simpleResponses)
		if err != nil {
			return nil, err
		}
		snapshot.Sites = append(snapshot.Sites, site)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	blacklistRows, err := p.pool.Query(ctx, "SELECT id,cidr::text FROM gateway_ip_blacklist WHERE removed_at IS NULL ORDER BY masklen(cidr) DESC,id")
	if err != nil {
		return nil, err
	}
	defer blacklistRows.Close()
	for blacklistRows.Next() {
		var rule proxy.BlacklistRule
		if err := blacklistRows.Scan(&rule.ID, &rule.CIDR); err != nil {
			return nil, err
		}
		snapshot.Blacklist = append(snapshot.Blacklist, rule)
	}
	if err := blacklistRows.Err(); err != nil {
		return nil, err
	}
	var revision int64
	err = p.pool.QueryRow(ctx, "SELECT revision FROM gateway_config_revision LIMIT 1").Scan(&revision)
	if err == nil {
		snapshot.Revision = revision
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return snapshot, nil
}

// validateSnapshotTransforms rejects transforms that need route values which a
// website-level snapshot does not own. Silently ignoring them would redirect
// a request differently from both the legacy route and the approved safe Go
// behavior; callers retain the prior LKG candidate instead.
func validateSnapshotTransforms(raw []byte) error {
	normalized, err := normalizeSnapshotTransforms(raw)
	if err != nil {
		return err
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(normalized, &values); err != nil {
		return err
	}
	for _, key := range []string{"PathPattern", "QueryRouteParameter"} {
		value, ok := values[key]
		if !ok || string(value) == "null" {
			continue
		}
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return fmt.Errorf("%s must be a string", key)
		}
		if strings.TrimSpace(text) != "" {
			return fmt.Errorf("%s requires an unresolved route value", key)
		}
	}
	return nil
}

// normalizeSnapshotTransforms accepts the editor's key/value array as well as
// the object shape used by the proxy runtime.
func normalizeSnapshotTransforms(raw []byte) ([]byte, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	switch typed := value.(type) {
	case map[string]any:
		return json.Marshal(typed)
	case []any:
		out := make(map[string]any, len(typed))
		for _, item := range typed {
			entry, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("proxyTransforms item must be an object")
			}
			key, ok := entry["key"].(string)
			if !ok || strings.TrimSpace(key) == "" {
				return nil, fmt.Errorf("proxyTransforms item key is required")
			}
			value, ok := entry["value"].(string)
			if !ok {
				return nil, fmt.Errorf("proxyTransforms item value must be a string")
			}
			out[key] = value
		}
		return json.Marshal(out)
	default:
		return nil, fmt.Errorf("proxyTransforms must be an object or array")
	}
}

func simpleResponseHeaders(value map[string]any) (map[string]string, error) {
	raw, ok := value["extraHeaders"]
	if !ok || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("extraHeaders must be an array")
	}
	headers := make(map[string]string, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("extraHeaders item must be an object")
		}
		key, value := stringValue(entry, "key", ""), stringValue(entry, "value", "")
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n") {
			return nil, fmt.Errorf("extraHeaders key is invalid")
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("extraHeaders value is invalid")
		}
		headers[key] = value
	}
	return headers, nil
}

func normalizeSimpleResponsePath(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && !strings.HasPrefix(value, "/") {
		return "/" + value
	}
	return value
}

func (p *Postgres) Revision(ctx context.Context) (int64, error) {
	var revision int64
	err := p.pool.QueryRow(ctx, "SELECT revision FROM gateway_config_revision LIMIT 1").Scan(&revision)
	return revision, err
}

type ConfigEvent struct {
	ID           int64
	ResourceType string
	ResourceID   string
	Operation    string
}

// ApplyConfigEvents applies a bounded, contiguous event batch to the data
// plane. The second result reports whether the caller can avoid a full proxy
// rebuild; the third reports whether the certificate manager must be replaced.
func (p *Postgres) ApplyConfigEvents(ctx context.Context, current *proxy.Snapshot, batchSize int) (*proxy.Snapshot, bool, bool, error) {
	if current == nil || current.Revision <= 0 {
		return nil, false, false, nil
	}
	if batchSize < 1 {
		batchSize = 500
	}
	revision, err := p.Revision(ctx)
	if err != nil {
		return nil, false, false, err
	}
	if revision == current.Revision {
		return current, true, false, nil
	}
	if revision < current.Revision {
		return nil, false, false, nil
	}
	rows, err := p.pool.Query(ctx, "SELECT id,resource_type,resource_id,operation FROM gateway_config_event WHERE id>$1 ORDER BY id LIMIT $2", current.Revision, batchSize+1)
	if err != nil {
		return nil, false, false, err
	}
	defer rows.Close()
	events := make([]ConfigEvent, 0, batchSize+1)
	for rows.Next() {
		var event ConfigEvent
		if err := rows.Scan(&event.ID, &event.ResourceType, &event.ResourceID, &event.Operation); err != nil {
			return nil, false, false, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, false, false, err
	}
	if len(events) == 0 || len(events) > batchSize || events[0].ID != current.Revision+1 || events[len(events)-1].ID != revision {
		return nil, false, false, nil
	}
	next := &proxy.Snapshot{Revision: revision, Sites: append([]proxy.Site(nil), current.Sites...), Blacklist: append([]proxy.BlacklistRule(nil), current.Blacklist...)}
	reloadCertificates := false
	for _, event := range events {
		switch event.ResourceType {
		case "website":
			id, err := strconv.ParseInt(event.ResourceID, 10, 64)
			if err != nil || id <= 0 {
				return nil, false, false, nil
			}
			switch strings.ToLower(event.Operation) {
			case "delete":
				index := proxySiteIndex(next.Sites, id)
				if index < 0 {
					return nil, false, false, nil
				}
				next.Sites = append(next.Sites[:index], next.Sites[index+1:]...)
			case "insert", "update":
				site, found, err := p.LoadProxySite(ctx, id)
				if err != nil {
					return nil, false, false, err
				}
				if !found {
					return nil, false, false, nil
				}
				index := proxySiteIndex(next.Sites, id)
				if index < 0 {
					if strings.EqualFold(event.Operation, "update") {
						return nil, false, false, nil
					}
					next.Sites = append(next.Sites, site)
				} else {
					next.Sites[index] = site
				}
			default:
				return nil, false, false, nil
			}
		case "gateway_certificate":
			reloadCertificates = true
		case "gateway_ip_blacklist":
			return nil, false, false, nil
		case "web_domain", "gateway_system_setting", "gateway_runtime_material":
			// These rows are consumed directly by ACME/management paths; they
			// do not alter an already-built proxy route snapshot.
		default:
			return nil, false, false, nil
		}
	}
	return next, true, reloadCertificates, nil
}

func proxySiteIndex(sites []proxy.Site, id int64) int {
	for index, site := range sites {
		if site.ID == id {
			return index
		}
	}
	return -1
}

// LoadProxySite reads exactly one website row for incremental reconciliation.
func (p *Postgres) LoadProxySite(ctx context.Context, id int64) (proxy.Site, bool, error) {
	var name string
	var specifications, hosts, rules, transforms, simpleResponses []byte
	err := p.pool.QueryRow(ctx, "SELECT name,specifications,hosts,proxy_rules,proxy_transforms,simple_responses FROM website WHERE id=$1", id).Scan(&name, &specifications, &hosts, &rules, &transforms, &simpleResponses)
	if errors.Is(err, pgx.ErrNoRows) {
		return proxy.Site{}, false, nil
	}
	if err != nil {
		return proxy.Site{}, false, err
	}
	site, err := decodeProxySite(id, name, specifications, hosts, rules, transforms, simpleResponses)
	return site, err == nil, err
}

func decodeProxySite(id int64, name string, specifications, hosts, rules, transforms, simpleResponses []byte) (proxy.Site, error) {
	spec, err := decodeWebsiteSpecifications(specifications)
	if err != nil {
		return proxy.Site{}, fmt.Errorf("website %d specifications: %w", id, err)
	}
	hostValues, hasHosts, err := websiteCollectionValue(spec, "hosts", hosts)
	if err != nil {
		return proxy.Site{}, fmt.Errorf("website %d hosts: %w", id, err)
	}
	var hostNames []string
	if hasHosts {
		rawHosts, err := json.Marshal(hostValues)
		if err != nil {
			return proxy.Site{}, fmt.Errorf("website %d hosts: %w", id, err)
		}
		if err := json.Unmarshal(rawHosts, &hostNames); err != nil {
			return proxy.Site{}, fmt.Errorf("website %d hosts: %w", id, err)
		}
	}
	legacyDefaultHost := stringValue(spec, "defaultHost", "")
	ipAccessMode := strings.ToLower(strings.TrimSpace(stringValue(spec, "ipAccessMode", "disabled")))
	ipAccessCIDRs, err := stringSliceValue(spec, "ipAccessCIDRs")
	if err != nil {
		return proxy.Site{}, fmt.Errorf("website %d IP access CIDRs: %w", id, err)
	}
	if err := validateWebsiteIPAccess(ipAccessMode, ipAccessCIDRs); err != nil {
		return proxy.Site{}, fmt.Errorf("website %d: %w", id, err)
	}
	historyMode, historyRate, err := websiteRequestHistoryValue(spec)
	if err != nil {
		return proxy.Site{}, fmt.Errorf("website %d request history: %w", id, err)
	}
	site := proxy.Site{ID: id, Hosts: hostNames, RedirectHost: legacyDefaultHost, Destination: stringValue(spec, "defaultDestination", ""), DestinationHost: stringValue(spec, "defaultDestinationHost", ""), RedirectHTTPS: boolValue(spec, "redirectHttpToHttps"), MaintenanceMode: boolValue(spec, "isMaintenanceMode"), IPAccessMode: ipAccessMode, IPAccessCIDRs: ipAccessCIDRs, OriginalHost: boolValue(spec, "preserveOriginalHost"), WebProxy: stringValue(spec, "webProxy", ""), AllowUnsafeTLS: boolValue(spec, "allowInvalidTlsCertificate"), RequestHistoryMode: historyMode, RequestHistorySuccessSamplingRate: historyRate, Simple: map[string]proxy.Response{}}
	bindingsPresent := false
	if rawBindings, ok := spec["hostBindings"]; ok && rawBindings != nil {
		bindingsPresent = true
		encoded, err := json.Marshal(rawBindings)
		if err != nil {
			return proxy.Site{}, fmt.Errorf("website %d host bindings: %w", id, err)
		}
		var bindings []struct {
			Hostname           string `json:"hostname"`
			Enabled            bool   `json:"enabled"`
			Role               string `json:"role"`
			AccessMode         string `json:"accessMode"`
			RedirectStatusCode int    `json:"redirectStatusCode"`
			RedirectTargetHost string `json:"redirectTargetHost"`
		}
		if err := json.Unmarshal(encoded, &bindings); err != nil {
			return proxy.Site{}, fmt.Errorf("website %d host bindings: %w", id, err)
		}
		for _, binding := range bindings {
			if binding.Hostname == "" {
				continue
			}
			if binding.Role == "" {
				binding.Role = "alias"
			}
			if binding.AccessMode == "" {
				binding.AccessMode = "proxy"
			}
			if binding.RedirectStatusCode == 0 {
				binding.RedirectStatusCode = http.StatusPermanentRedirect
			}
			normalizedHostname := normalizeResourceHost(binding.Hostname)
			site.HostBindings = append(site.HostBindings, proxy.HostBinding{Hostname: normalizedHostname, Enabled: binding.Enabled, Role: binding.Role, AccessMode: binding.AccessMode, RedirectStatusCode: binding.RedirectStatusCode, RedirectTargetHost: normalizeResourceHost(binding.RedirectTargetHost)})
			if strings.EqualFold(binding.Role, "primary") && binding.Enabled {
				site.RedirectHost = normalizedHostname
			}
		}
		if bindingsPresent {
			site.Hosts = nil
			for _, binding := range site.HostBindings {
				if binding.Enabled {
					site.Hosts = append(site.Hosts, binding.Hostname)
				}
			}
		}
	}
	if !bindingsPresent {
		// Legacy configurations are converted into the authoritative binding
		// model. The old fields remain readable only for this migration step.
		legacyHosts := make([]string, 0, len(hostNames)+1)
		if legacyDefaultHost != "" {
			legacyHosts = append(legacyHosts, legacyDefaultHost)
		}
		legacyHosts = append(legacyHosts, hostNames...)
		seen := map[string]bool{}
		for index, hostname := range legacyHosts {
			normalized := normalizeResourceHost(hostname)
			if normalized == "" || seen[normalized] {
				continue
			}
			seen[normalized] = true
			role := "alias"
			if index == 0 {
				role = "primary"
				site.RedirectHost = normalized
			}
			site.HostBindings = append(site.HostBindings, proxy.HostBinding{Hostname: normalized, Enabled: true, Role: role, AccessMode: "proxy", RedirectStatusCode: http.StatusPermanentRedirect})
		}
		site.Hosts = nil
		for _, binding := range site.HostBindings {
			site.Hosts = append(site.Hosts, binding.Hostname)
		}
	}
	if bindingsPresent && len(site.HostBindings) == 0 {
		// An explicitly empty/new host configuration must not fall back to the
		// database row name or any other implicit hostname.
		site.Hosts = nil
		site.RedirectHost = ""
	}
	if rawPools, ok := spec["upstreamPools"]; ok && rawPools != nil {
		encoded, err := json.Marshal(rawPools)
		if err != nil {
			return proxy.Site{}, fmt.Errorf("website %d upstream pools: %w", id, err)
		}
		var pools []struct {
			ID        string `json:"id"`
			Strategy  string `json:"strategy"`
			Endpoints []struct {
				Address   string `json:"address"`
				Host      string `json:"host"`
				Weight    int    `json:"weight"`
				Priority  int    `json:"priority"`
				Enabled   bool   `json:"enabled"`
				TLSVerify *bool  `json:"tlsVerify"`
				Proxy     string `json:"proxy"`
			} `json:"endpoints"`
		}
		if err := json.Unmarshal(encoded, &pools); err != nil {
			return proxy.Site{}, fmt.Errorf("website %d upstream pools: %w", id, err)
		}
		site.UpstreamPools = map[string]proxy.UpstreamPool{}
		for _, pool := range pools {
			endpoints := make([]proxy.UpstreamEndpoint, 0, len(pool.Endpoints))
			for _, endpoint := range pool.Endpoints {
				if endpoint.Weight < 1 {
					endpoint.Weight = 1
				}
				endpoints = append(endpoints, proxy.UpstreamEndpoint{Address: endpoint.Address, Host: endpoint.Host, Weight: endpoint.Weight, Priority: endpoint.Priority, Enabled: endpoint.Enabled, TLSVerify: endpoint.TLSVerify, Proxy: endpoint.Proxy})
			}
			if pool.ID != "" {
				site.UpstreamPools[pool.ID] = proxy.UpstreamPool{Strategy: pool.Strategy, Endpoints: endpoints}
			}
		}
	}
	transformValues, hasTransforms, err := websiteCollectionValue(spec, "proxyTransforms", transforms)
	if err != nil {
		return proxy.Site{}, fmt.Errorf("website %d transforms: %w", id, err)
	}
	if hasTransforms {
		rawTransforms, err := json.Marshal(transformValues)
		if err != nil {
			return proxy.Site{}, fmt.Errorf("website %d transforms: %w", id, err)
		}
		rawTransforms = bytes.TrimSpace(rawTransforms)
		if len(rawTransforms) > 0 && !bytes.Equal(rawTransforms, []byte("null")) && !bytes.Equal(rawTransforms, []byte("{}")) {
			normalizedTransforms, err := normalizeSnapshotTransforms(rawTransforms)
			if err != nil {
				return proxy.Site{}, fmt.Errorf("website %d transforms: %w", id, err)
			}
			if err := validateSnapshotTransforms(normalizedTransforms); err != nil {
				return proxy.Site{}, fmt.Errorf("website %d transforms: %w", id, err)
			}
			if err := json.Unmarshal(normalizedTransforms, &site.Transforms); err != nil {
				return proxy.Site{}, fmt.Errorf("website %d transforms: %w", id, err)
			}
		}
	}
	ruleValues, hasRules, err := websiteCollectionValue(spec, "proxyRules", rules)
	if err != nil {
		return proxy.Site{}, fmt.Errorf("website %d rules: %w", id, err)
	}
	if hasRules {
		encodedRules, err := json.Marshal(ruleValues)
		if err != nil {
			return proxy.Site{}, fmt.Errorf("website %d rules: %w", id, err)
		}
		encodedRules = bytes.TrimSpace(encodedRules)
		if len(encodedRules) > 0 && !bytes.Equal(encodedRules, []byte("null")) && !bytes.Equal(encodedRules, []byte("[]")) {
			var decodedRules []struct {
				Enabled                                                 bool `json:"isEnabled"`
				PathPattern, Destination, DestinationHost, RemovePrefix string
				WebProxy                                                *string `json:"webProxy"`
				OriginalHost                                            *bool   `json:"preserveOriginalHost"`
				AllowUnsafeTLS                                          *bool   `json:"allowInvalidTlsCertificate"`
				UpstreamPoolID                                          string  `json:"upstreamPoolId"`
				IsFallback                                              bool    `json:"isFallback"`
			}
			if err := json.Unmarshal(encodedRules, &decodedRules); err != nil {
				return proxy.Site{}, fmt.Errorf("website %d rules: %w", id, err)
			}
			for _, rule := range decodedRules {
				webProxy := ""
				if rule.WebProxy != nil {
					webProxy = *rule.WebProxy
				}
				allowUnsafe := false
				allowUnsafeSet := rule.AllowUnsafeTLS != nil
				if rule.AllowUnsafeTLS != nil {
					allowUnsafe = *rule.AllowUnsafeTLS
				}
				if rule.UpstreamPoolID == "" && rule.AllowUnsafeTLS == nil {
					allowUnsafeSet = true
				}
				site.Rules = append(site.Rules, proxy.Rule{Enabled: rule.Enabled, PathPattern: rule.PathPattern, Destination: rule.Destination, DestinationHost: rule.DestinationHost, RemovePrefix: rule.RemovePrefix, OriginalHost: rule.OriginalHost, WebProxy: webProxy, WebProxySet: rule.WebProxy != nil, AllowUnsafeTLS: allowUnsafe, AllowUnsafeTLSSet: allowUnsafeSet, UpstreamPoolID: rule.UpstreamPoolID, IsFallback: rule.IsFallback})
			}
		}
	}
	// Legacy default destination becomes an explicit fallback rule in the runtime
	// snapshot so unmatched requests no longer use an implicit site destination.
	if site.Destination != "" {
		if site.UpstreamPools == nil {
			site.UpstreamPools = map[string]proxy.UpstreamPool{}
		}
		if _, exists := site.UpstreamPools["legacy-default"]; !exists {
			site.UpstreamPools["legacy-default"] = proxy.UpstreamPool{Strategy: "weightedRoundRobin", Endpoints: []proxy.UpstreamEndpoint{{Address: site.Destination, Host: site.DestinationHost, Weight: 1, Enabled: true}}}
		}
		hasFallback := false
		for _, rule := range site.Rules {
			if rule.IsFallback {
				hasFallback = true
				break
			}
		}
		if !hasFallback {
			site.Rules = append(site.Rules, proxy.Rule{Enabled: true, IsFallback: true, UpstreamPoolID: "legacy-default", OriginalHost: nil})
		}
		site.Destination, site.DestinationHost = "", ""
	}
	responseValues, hasResponses, err := websiteCollectionValue(spec, "simpleResponses", simpleResponses)
	if err != nil {
		return proxy.Site{}, fmt.Errorf("website %d simple responses: %w", id, err)
	}
	if hasResponses {
		rawResponses, err := json.Marshal(responseValues)
		if err != nil {
			return proxy.Site{}, fmt.Errorf("website %d simple responses: %w", id, err)
		}
		rawResponses = bytes.TrimSpace(rawResponses)
		if len(rawResponses) > 0 && !bytes.Equal(rawResponses, []byte("null")) {
			var rawResponseValues []map[string]any
			if err := json.Unmarshal(rawResponses, &rawResponseValues); err != nil {
				return proxy.Site{}, fmt.Errorf("website %d simple responses: %w", id, err)
			}
			for _, value := range rawResponseValues {
				path := normalizeSimpleResponsePath(stringValue(value, "path", ""))
				if path == "" {
					return proxy.Site{}, fmt.Errorf("website %d has a simple response without path", id)
				}
				headers, err := simpleResponseHeaders(value)
				if err != nil {
					return proxy.Site{}, fmt.Errorf("website %d simple response %q: %w", id, path, err)
				}
				site.Simple[path] = proxy.Response{Status: intFrom(value, "statusCode", 200), ContentType: stringValue(value, "contentType", "text/plain; charset=utf-8"), Body: stringValue(value, "responseContent", ""), Headers: headers}
			}
		}
	}
	return site, nil
}
