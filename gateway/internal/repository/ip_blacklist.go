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
	if scope := strings.ToLower(strings.TrimSpace(input.Scope)); scope != "" && scope != "gateway" {
		return management.IPBlacklistRule{}, fmt.Errorf("gateway IP blacklist scope must be gateway")
	}
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

func (p *Postgres) SummarizeIPBlacklist(ctx context.Context) (management.IPBlacklistSummary, error) {
	var summary management.IPBlacklistSummary
	err := p.pool.QueryRow(ctx, "SELECT COUNT(*) FILTER (WHERE removed_at IS NULL), COALESCE(SUM(blocked_count),0)::bigint FROM gateway_ip_blacklist").Scan(&summary.ActiveRules, &summary.BlockedTotal)
	return summary, err
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
	target := *base
	target.Path = strings.TrimRight(base.Path, "/") + "/ip/" + raw
	target.RawPath = strings.TrimRight(base.EscapedPath(), "/") + "/ip/" + url.PathEscape(raw)
	target.RawQuery = url.Values{"format": {"json"}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return management.IPProfile{}, err
	}
	response, err := p.ipProfileClient.Do(request)
	if err != nil {
		return management.IPProfile{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return management.IPProfile{}, fmt.Errorf("IP profile request failed: %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return management.IPProfile{}, err
	}
	var payload struct {
		IPAddress string `json:"ipAddress"`
		Country   *struct {
			Name    *string `json:"name"`
			ISOCode *string `json:"isoCode"`
		} `json:"country"`
		ASN *struct {
			Number       int64   `json:"number"`
			Organization *string `json:"organization"`
			Network      *string `json:"network"`
		} `json:"asn"`
		Location *struct {
			City *string `json:"city"`
		} `json:"location"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return management.IPProfile{}, err
	}
	value := management.IPProfile{IP: raw, Network: &management.IPProfileNetwork{Type: "unknown"}}
	if payload.IPAddress != "" {
		value.IP = payload.IPAddress
	}
	if payload.Country != nil {
		value.Country = &management.IPProfileName{}
		if payload.Country.Name != nil {
			value.Country.Name = *payload.Country.Name
		}
		if payload.Country.ISOCode != nil {
			value.Country.ISOCode = *payload.Country.ISOCode
		}
	}
	if payload.Location != nil && payload.Location.City != nil && *payload.Location.City != "" {
		value.City = &management.IPProfileCity{Name: *payload.Location.City}
	}
	if payload.ASN != nil {
		asn := management.IPProfileASN{Number: payload.ASN.Number, Network: canonicalProfileNetwork(payload.ASN.Network)}
		if payload.ASN.Organization != nil {
			asn.Organization = *payload.ASN.Organization
		}
		if asn.Number != 0 || asn.Organization != "" || asn.Network != "" {
			value.ASN = &asn
			value.Network.Organization = asn.Organization
		}
	}
	return value, nil
}

// canonicalProfileNetwork normalises the provider's ASN network to a masked CIDR.
// The provider may return host bits set ("8.8.8.8/24") or a bare address; the
// latter is not a network and is dropped so it is never offered as a block target.
func canonicalProfileNetwork(value *string) string {
	if value == nil {
		return ""
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(*value))
	if err != nil {
		return ""
	}
	return prefix.Masked().String()
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
		Scope:         "gateway",
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
