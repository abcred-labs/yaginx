package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"yaginx-gateway/internal/management"
	"yaginx-gateway/internal/telemetry"
)

func (p *Postgres) SearchIPBlacklist(ctx context.Context, input management.IPBlacklistSearchRequest) (management.Page[management.IPBlacklistRule], error) {
	page, err := management.NormalizePageRequest(input.PageRequest)
	if err != nil {
		return management.Page[management.IPBlacklistRule]{}, err
	}
	filter, args := "true", []any{}
	if input.IncludeRemoved != nil && !*input.IncludeRemoved {
		filter = "removed_at IS NULL"
	}
	if keyword := strings.TrimSpace(input.Keyword); keyword != "" {
		args = append(args, keyword)
		filter += " AND cidr::text ILIKE '%' || $" + strconv.Itoa(len(args)) + " || '%'"
	}
	var total int64
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM gateway_ip_blacklist WHERE "+filter, args...).Scan(&total); err != nil {
		return management.Page[management.IPBlacklistRule]{}, err
	}
	paging, offset, err := management.PageWindow(page, total)
	if err != nil {
		return management.Page[management.IPBlacklistRule]{}, err
	}
	args = append(args, paging.PageSize, offset)
	rows, err := p.pool.Query(ctx, "SELECT id,cidr::text,profile::text,blocked_count,last_blocked_at,created_at,removed_at FROM gateway_ip_blacklist WHERE "+filter+" ORDER BY removed_at NULLS FIRST,created_at DESC,id DESC LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
	if err != nil {
		return management.Page[management.IPBlacklistRule]{}, err
	}
	defer rows.Close()
	records := make([]management.IPBlacklistRule, 0)
	for rows.Next() {
		record, err := scanBlacklist(rows)
		if err != nil {
			return management.Page[management.IPBlacklistRule]{}, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return management.Page[management.IPBlacklistRule]{}, err
	}
	return management.Page[management.IPBlacklistRule]{
		Records: records,
		Paging:  paging,
	}, nil
}

func (p *Postgres) GetIPBlacklist(ctx context.Context, key string) (management.IPBlacklistRule, error) {
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil {
		return management.IPBlacklistRule{}, management.ErrNotFound
	}
	row := p.pool.QueryRow(ctx, "SELECT id,cidr::text,profile::text,blocked_count,last_blocked_at,created_at,removed_at FROM gateway_ip_blacklist WHERE id=$1", id)
	return scanBlacklist(row)
}

func (p *Postgres) UpsertIPBlacklist(ctx context.Context, input management.IPBlacklistUpsertRequest) (management.IPBlacklistRule, error) {
	cidr, base, err := normalizeBlacklistCIDR(input.CIDR)
	if err != nil {
		return management.IPBlacklistRule{}, err
	}
	profile := management.IPProfile{
		IP:      base.String(),
		Network: &management.IPProfileNetwork{Type: "unknown"},
		Traits:  map[string]bool{},
	}
	if value, lookupErr := p.LookupIP(ctx, base.String()); lookupErr == nil {
		profile = value
	}
	plain, err := json.Marshal(profile)
	if err != nil {
		return management.IPBlacklistRule{}, err
	}
	var id int64
	err = p.pool.QueryRow(ctx, "INSERT INTO gateway_ip_blacklist(cidr,profile) VALUES($1::cidr,$2::jsonb) ON CONFLICT (cidr) WHERE removed_at IS NULL DO UPDATE SET cidr=EXCLUDED.cidr RETURNING id", cidr, string(plain)).Scan(&id)
	if err != nil {
		return management.IPBlacklistRule{}, err
	}
	return p.GetIPBlacklist(ctx, strconv.FormatInt(id, 10))
}

func (p *Postgres) RemoveIPBlacklist(ctx context.Context, key string) (management.IPBlacklistRule, error) {
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil {
		return management.IPBlacklistRule{}, management.ErrNotFound
	}
	command, err := p.pool.Exec(ctx, "UPDATE gateway_ip_blacklist SET removed_at=clock_timestamp() WHERE id=$1 AND removed_at IS NULL", id)
	if err != nil {
		return management.IPBlacklistRule{}, err
	}
	if command.RowsAffected() == 0 {
		return management.IPBlacklistRule{}, management.ErrNotFound
	}
	return p.GetIPBlacklist(ctx, key)
}

func (p *Postgres) LookupIP(ctx context.Context, raw string) (management.IPProfile, error) {
	raw = strings.TrimSpace(raw)
	if _, err := netip.ParseAddr(raw); err != nil {
		return management.IPProfile{}, fmt.Errorf("invalid IP address")
	}
	base, err := url.Parse(strings.TrimRight(p.ipProfileEndpoint, "/"))
	if err != nil {
		return management.IPProfile{}, err
	}
	type result struct {
		kind string
		body []byte
		err  error
	}
	results := make(chan result, 3)
	var wg sync.WaitGroup
	for _, kind := range []string{"QueryCity", "QueryCountry", "QueryAsn"} {
		wg.Add(1)
		go func(kind string) {
			defer wg.Done()
			target := *base
			target.Path = strings.TrimRight(base.Path, "/") + "/Query/" + kind
			query := target.Query()
			query.Set("ip", raw)
			target.RawQuery = query.Encode()
			request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
			if requestErr != nil {
				results <- result{kind: kind, err: requestErr}
				return
			}
			response, requestErr := p.ipProfileClient.Do(request)
			if requestErr != nil {
				results <- result{kind: kind, err: requestErr}
				return
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				results <- result{kind: kind, err: fmt.Errorf("IP profile request failed: %s", response.Status)}
				return
			}
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			results <- result{kind: kind, body: body, err: readErr}
		}(kind)
	}
	wg.Wait()
	close(results)
	var city, country, asn []byte
	for item := range results {
		if item.err != nil {
			return management.IPProfile{}, item.err
		}
		switch item.kind {
		case "QueryCity":
			city = item.body
		case "QueryCountry":
			country = item.body
		case "QueryAsn":
			asn = item.body
		}
	}
	var cityValue struct {
		IPAddress string `json:"ipAddress"`
		City      *struct {
			Name      *string `json:"name"`
			GeoNameID *int64  `json:"geoNameId"`
		} `json:"city"`
		Location *struct {
			Latitude  *float64 `json:"latitude"`
			Longitude *float64 `json:"longitude"`
			TimeZone  *string  `json:"timeZone"`
		} `json:"location"`
		Traits *struct {
			ISP                *string `json:"isp"`
			Organization       *string `json:"organization"`
			IsAnonymous        *bool   `json:"isAnonymous"`
			IsAnonymousProxy   *bool   `json:"isAnonymousProxy"`
			IsAnonymousVPN     *bool   `json:"isAnonymousVpn"`
			IsHostingProvider  *bool   `json:"isHostingProvider"`
			IsPublicProxy      *bool   `json:"isPublicProxy"`
			IsResidentialProxy *bool   `json:"isResidentialProxy"`
			IsTorExitNode      *bool   `json:"isTorExitNode"`
		} `json:"traits"`
	}
	var countryValue struct {
		IPAddress string `json:"ipAddress"`
		Country   *struct {
			Name    *string `json:"name"`
			ISOCode *string `json:"isoCode"`
		} `json:"country"`
	}
	var asnValue struct {
		IPAddress          string `json:"ipAddress"`
		SystemNumber       int64  `json:"systemNumber"`
		SystemOrganization string `json:"systemOrganization"`
		Network            string `json:"network"`
	}
	if err := json.Unmarshal(city, &cityValue); err != nil {
		return management.IPProfile{}, err
	}
	if err := json.Unmarshal(country, &countryValue); err != nil {
		return management.IPProfile{}, err
	}
	if err := json.Unmarshal(asn, &asnValue); err != nil {
		return management.IPProfile{}, err
	}
	value := management.IPProfile{IP: raw, Network: &management.IPProfileNetwork{Type: "unknown"}, Traits: map[string]bool{}}
	if cityValue.IPAddress != "" {
		value.IP = cityValue.IPAddress
	}
	if countryValue.IPAddress != "" {
		value.IP = countryValue.IPAddress
	}
	if asnValue.IPAddress != "" {
		value.IP = asnValue.IPAddress
	}
	if countryValue.Country != nil {
		value.Country = &management.IPProfileName{}
		if countryValue.Country.Name != nil {
			value.Country.Name = *countryValue.Country.Name
		}
		if countryValue.Country.ISOCode != nil {
			value.Country.ISOCode = *countryValue.Country.ISOCode
		}
	}
	if cityValue.City != nil {
		value.City = &management.IPProfileCity{}
		if cityValue.City.Name != nil {
			value.City.Name = *cityValue.City.Name
		}
		if cityValue.City.GeoNameID != nil {
			value.City.GeoNameID = *cityValue.City.GeoNameID
		}
	}
	if asnValue.SystemNumber != 0 || asnValue.SystemOrganization != "" || asnValue.Network != "" {
		value.ASN = &management.IPProfileASN{Number: asnValue.SystemNumber, Organization: asnValue.SystemOrganization, Network: asnValue.Network}
		value.Network.Organization = asnValue.SystemOrganization
	}
	if cityValue.Traits != nil {
		for name, flag := range map[string]*bool{"isAnonymous": cityValue.Traits.IsAnonymous, "isAnonymousProxy": cityValue.Traits.IsAnonymousProxy, "isAnonymousVpn": cityValue.Traits.IsAnonymousVPN, "isHostingProvider": cityValue.Traits.IsHostingProvider, "isPublicProxy": cityValue.Traits.IsPublicProxy, "isResidentialProxy": cityValue.Traits.IsResidentialProxy, "isTorExitNode": cityValue.Traits.IsTorExitNode} {
			if flag != nil {
				value.Traits[name] = *flag
			}
		}
		if cityValue.Traits.ISP != nil {
			value.Network.ISP = *cityValue.Traits.ISP
		}
		if cityValue.Traits.Organization != nil {
			value.Network.Organization = *cityValue.Traits.Organization
		}
	}
	return value, nil
}

func (p *Postgres) FlushBlacklistHits(ctx context.Context, events []telemetry.Event) error {
	type hit struct {
		count int64
		at    time.Time
	}
	hits := map[int64]hit{}
	for _, event := range events {
		if event.BlacklistRuleID == 0 {
			continue
		}
		current := hits[event.BlacklistRuleID]
		current.count++
		if event.At.After(current.at) {
			current.at = event.At
		}
		hits[event.BlacklistRuleID] = current
	}
	if len(hits) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for id, hit := range hits {
		if _, err := tx.Exec(ctx, "UPDATE gateway_ip_blacklist SET blocked_count=blocked_count+$2,last_blocked_at=GREATEST(COALESCE(last_blocked_at,to_timestamp(0)),$3) WHERE id=$1", id, hit.count, hit.at.UTC()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type blacklistScanner interface {
	Scan(...any) error
}

func scanBlacklist(row blacklistScanner) (management.IPBlacklistRule, error) {
	var id, count int64
	var cidr, profile string
	var lastBlocked *time.Time
	var created time.Time
	var removed *time.Time
	if err := row.Scan(&id, &cidr, &profile, &count, &lastBlocked, &created, &removed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return management.IPBlacklistRule{}, management.ErrNotFound
		}
		return management.IPBlacklistRule{}, err
	}
	var decoded management.IPProfile
	_ = json.Unmarshal([]byte(profile), &decoded)
	result := management.IPBlacklistRule{
		ID:            strconv.FormatInt(id, 10),
		CIDR:          cidr,
		Profile:       decoded,
		BlockedCount:  count,
		LastBlockedAt: optionalEpochSeconds(lastBlocked),
		CreatedAt:     created.UTC().Unix(),
		RemovedAt:     optionalEpochSeconds(removed),
		IsActive:      removed == nil,
	}
	return result, nil
}

func optionalEpochSeconds(value *time.Time) *int64 {
	if value == nil {
		return nil
	}
	seconds := value.UTC().Unix()
	return &seconds
}

func normalizeBlacklistCIDR(value string) (string, netip.Addr, error) {
	value = strings.TrimSpace(value)
	if address, err := netip.ParseAddr(value); err == nil {
		address = address.Unmap()
		bits := 128
		if address.Is4() {
			bits = 32
		}
		return netip.PrefixFrom(address, bits).String(), address, nil
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return "", netip.Addr{}, fmt.Errorf("CIDR must be a valid IPv4 or IPv6 address/network")
	}
	prefix = prefix.Masked()
	return prefix.String(), prefix.Addr(), nil
}
