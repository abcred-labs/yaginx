package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
	"yaginx-gateway/internal/management"

	"github.com/jackc/pgx/v5"
)

// Managed domains: the per-hostname certificate and verification state.

func (p *Postgres) UpsertDomain(ctx context.Context, value management.DomainUpsertRequest) (management.Domain, error) {
	if value.Name == nil {
		return management.Domain{}, fmt.Errorf("domain name is required")
	}
	name := normalizeResourceHost(*value.Name)
	if name == "" {
		return management.Domain{}, fmt.Errorf("domain name is required")
	}
	free := value.AutoRequestCertificate
	verified := value.IsVerified
	msg := ""
	if value.FreeCertMessage != nil {
		msg = *value.FreeCertMessage
	}
	var outID int64
	var outName, outMsg string
	var outFree, outVerified bool
	var err error
	if value.ID != nil {
		err = p.pool.QueryRow(ctx, "UPDATE web_domain SET name=$2,is_use_free_cert=$3,is_verified=$4,free_cert_message=$5 WHERE id=$1 RETURNING id,name,is_use_free_cert,is_verified,coalesce(free_cert_message,'')", int64(*value.ID), name, free, verified, msg).Scan(&outID, &outName, &outFree, &outVerified, &outMsg)
	} else {
		err = p.pool.QueryRow(ctx, `INSERT INTO web_domain(name,is_use_free_cert,is_verified,free_cert_message)
VALUES($1,$2,$3,$4)
ON CONFLICT (name) WHERE name IS NOT NULL DO UPDATE SET
    is_use_free_cert=EXCLUDED.is_use_free_cert,
    is_verified=EXCLUDED.is_verified,
    free_cert_message=EXCLUDED.free_cert_message
RETURNING id,name,is_use_free_cert,is_verified,coalesce(free_cert_message,'')`, name, free, verified, msg).Scan(&outID, &outName, &outFree, &outVerified, &outMsg)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return management.Domain{}, management.ErrNotFound
	}
	if err != nil {
		return management.Domain{}, err
	}
	if outFree {
		// An operator explicitly (re-)enabling auto-request clears a stopped
		// automatic renewal.
		if _, err := p.pool.Exec(ctx, "DELETE FROM gateway_acme_auto_state WHERE domain_name=$1", outName); err != nil {
			return management.Domain{}, err
		}
	}
	return management.Domain{
		ID: management.ID(outID),
		DomainUpsertRequest: management.DomainUpsertRequest{
			Name:                   ptr(outName),
			AutoRequestCertificate: outFree,
			IsVerified:             outVerified,
			FreeCertMessage:        ptr(outMsg),
		},
	}, nil
}

func (p *Postgres) ListDomains(ctx context.Context) ([]management.DomainListItem, error) {
	rows, err := p.pool.Query(ctx, domainListQuery+" ORDER BY d.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []management.DomainListItem{}
	for rows.Next() {
		var id int64
		var name, msg string
		var free, verified bool
		var fingerprint *string
		var notBefore, notAfter *time.Time
		if err := rows.Scan(&id, &name, &free, &verified, &msg, &fingerprint, &notBefore, &notAfter); err != nil {
			return nil, err
		}
		out = append(out, domainListItem(id, name, free, verified, msg, fingerprint, notBefore, notAfter))
	}
	return out, rows.Err()
}

func (p *Postgres) SearchDomains(ctx context.Context, input management.WebDomainSearchRequest) (management.Page[management.DomainListItem], error) {
	var total int64
	if err := p.pool.QueryRow(ctx, domainCountQuery).Scan(&total); err != nil {
		return management.Page[management.DomainListItem]{}, err
	}
	paging, offset, err := management.PageWindow(input.PageRequest, total)
	if err != nil {
		return management.Page[management.DomainListItem]{}, err
	}
	rows, err := p.pool.Query(ctx, domainListQuery+" ORDER BY d.id LIMIT $1 OFFSET $2", paging.PageSize, offset)
	if err != nil {
		return management.Page[management.DomainListItem]{}, err
	}
	defer rows.Close()
	records := make([]management.DomainListItem, 0)
	for rows.Next() {
		var id int64
		var name, msg string
		var free, verified bool
		var fingerprint *string
		var notBefore, notAfter *time.Time
		if err := rows.Scan(&id, &name, &free, &verified, &msg, &fingerprint, &notBefore, &notAfter); err != nil {
			return management.Page[management.DomainListItem]{}, err
		}
		records = append(records, domainListItem(id, name, free, verified, msg, fingerprint, notBefore, notAfter))
	}
	if err := rows.Err(); err != nil {
		return management.Page[management.DomainListItem]{}, err
	}
	return management.Page[management.DomainListItem]{Records: records, Paging: paging}, nil
}

func (p *Postgres) GetDomain(ctx context.Context, key string) (management.DomainListItem, error) {
	var id int64
	var name, msg string
	var free, verified bool
	query := domainListQuery + " AND d.id=$1"
	if _, err := strconv.ParseInt(key, 10, 64); err != nil {
		query = domainListQuery + " AND d.name=$1"
	}
	var fingerprint *string
	var notBefore, notAfter *time.Time
	err := p.pool.QueryRow(ctx, query, key).Scan(&id, &name, &free, &verified, &msg, &fingerprint, &notBefore, &notAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.DomainListItem{}, management.ErrNotFound
	}
	if err != nil {
		return management.DomainListItem{}, err
	}
	return domainListItem(id, name, free, verified, msg, fingerprint, notBefore, notAfter), nil
}

// DeleteDomain removes a Host resource. A purely numeric key is the legacy
// web_domain row ID; anything else is a hostname. Deleting by hostname also
// covers hosts that only exist as observed traffic (no web_domain row), and is
// refused while any website still binds the host.
func (p *Postgres) DeleteDomain(ctx context.Context, key string) error {
	if _, err := strconv.ParseInt(key, 10, 64); err == nil {
		tag, err := p.pool.Exec(ctx, "DELETE FROM web_domain WHERE id=$1", key)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return management.ErrNotFound
		}
		return nil
	}
	host := normalizeResourceHost(key)
	if host == "" {
		return management.ErrNotFound
	}
	websites, err := p.ListWebsites(ctx)
	if err != nil {
		return err
	}
	for _, website := range websites {
		for _, bound := range normalizedWebsiteHosts(website) {
			if bound == host {
				return fmt.Errorf("%w: host %s is bound to website %s", management.ErrConflict, host, stringPointerValue(website.Name))
			}
		}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := cancelPendingACMEOrders(ctx, tx, host, 0, "host deleted"); err != nil {
		return err
	}
	// Host names are stored as received by the proxy (case and trailing dot are
	// not normalized on write), so match on the normalized form.
	const norm = "lower(trim(trailing '.' from %s))"
	for _, statement := range []string{
		"DELETE FROM web_domain WHERE " + fmt.Sprintf(norm, "name") + "=$1",
		"DELETE FROM gateway_acme_auto_state WHERE domain_name=$1",
		"DELETE FROM gateway_acme_attempt WHERE domain_name=$1",
		"DELETE FROM host_traffic WHERE " + fmt.Sprintf(norm, "host_name") + "=$1",
		"DELETE FROM gateway_traffic_contribution WHERE " + fmt.Sprintf(norm, "host_name") + "=$1",
		"DELETE FROM resource_report WHERE " + fmt.Sprintf(norm, "resource_uuid") + "=$1",
		"DELETE FROM resource_monitor_info WHERE " + fmt.Sprintf(norm, "resource_uuid") + "=$1",
		"DELETE FROM request_history WHERE " + fmt.Sprintf(norm, "host") + "=$1",
	} {
		if _, err := tx.Exec(ctx, statement, host); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

const domainManagedWhere = `WHERE EXISTS (
    SELECT 1 FROM website w
    WHERE lower(trim(trailing '.' from d.name)) = lower(trim(trailing '.' from coalesce(w.specifications->>'defaultHost','')))
       OR EXISTS (
            SELECT 1 FROM jsonb_array_elements_text(
                CASE WHEN jsonb_typeof(w.hosts) = 'array' THEN w.hosts ELSE '[]'::jsonb END
            ) AS host(value)
            WHERE lower(trim(trailing '.' from host.value)) = lower(trim(trailing '.' from d.name))
       )
)`

const domainCountQuery = `SELECT count(*) FROM web_domain d ` + domainManagedWhere

const domainListQuery = `SELECT d.id,d.name,d.is_use_free_cert,d.is_verified,coalesce(d.free_cert_message,''),c.fingerprint,c.not_before,c.not_after
FROM web_domain d
LEFT JOIN LATERAL (
    SELECT fingerprint,not_before,not_after
    FROM gateway_certificate
    WHERE status='active' AND is_active=true AND not_after > clock_timestamp() AND d.name=ANY(dns_names)
    ORDER BY not_after DESC,id DESC
    LIMIT 1
) c ON true ` + domainManagedWhere

func domainResponseMap(id int64, name string, free, verified bool, msg string) map[string]any {
	return map[string]any{"id": strconv.FormatInt(id, 10), "name": name, "autoRequestCertificate": free, "isVerified": verified, "freeCertMessage": msg}
}

func domainListItemMap(id int64, name string, free, verified bool, msg string, fingerprint *string, notBefore, notAfter *time.Time) map[string]any {
	out := domainResponseMap(id, name, free, verified, msg)
	certSpec := map[string]any{"hasCertificate": false, "notBefore": int64(-62135596800), "notAfter": int64(-62135596800), "thumbprint": nil}
	if fingerprint != nil && notBefore != nil && notAfter != nil {
		certSpec["hasCertificate"] = true
		certSpec["notBefore"] = notBefore.Unix()
		certSpec["notAfter"] = notAfter.Unix()
		certSpec["thumbprint"] = *fingerprint
	}
	out["certSpec"] = certSpec
	return out
}

func domainListItem(id int64, name string, free, verified bool, msg string, fingerprint *string, notBefore, notAfter *time.Time) management.DomainListItem {
	certSpec := management.CertificateSummary{NotBefore: -62135596800, NotAfter: -62135596800}
	if fingerprint != nil && notBefore != nil && notAfter != nil {
		certSpec.HasCertificate = true
		certSpec.NotBefore = notBefore.Unix()
		certSpec.NotAfter = notAfter.Unix()
		certSpec.Thumbprint = fingerprint
	}
	return management.DomainListItem{
		ID:                     management.ID(id),
		Name:                   ptr(name),
		AutoRequestCertificate: free,
		IsVerified:             verified,
		CertSpec:               certSpec,
	}
}
