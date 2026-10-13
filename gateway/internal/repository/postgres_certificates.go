package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"
	"yaginx-gateway/internal/certificates"
)

// Certificate storage: PFX imports, ACME issuance results, and the load path
// the TLS front end reads at startup.

type acmeCertificatePayload struct {
	CertificatePEM []byte `json:"certificatePem"`
	PrivateKeyPEM  []byte `json:"privateKeyPem"`
}

const certificateChainCompletionBudget = 10 * time.Second

// ImportPFX stores parsed certificate PEM material directly in PostgreSQL.
func (p *Postgres) ImportPFX(ctx context.Context, pfx, password []byte) (string, error) {
	entry, err := certificates.ParsePFX(pfx, password)
	if err != nil {
		return "", err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(entry.Certificate.Certificate[0]))
	chainPEM, keyPEM, err := certificates.EncodePEM(entry)
	if err != nil {
		return "", err
	}
	_, err = p.pool.Exec(ctx, "INSERT INTO gateway_certificate(fingerprint,fingerprint_sha256,dns_names,source,status,is_active,not_before,not_after,certificate_pem,private_key_pem,created_at,updated_at) VALUES($1,$2,$3,'imported_pfx','active',true,$4,$5,$6,$7,clock_timestamp(),clock_timestamp()) ON CONFLICT(fingerprint) DO NOTHING", fingerprint, fingerprint, entry.Names, entry.NotBefore, entry.NotAfter, chainPEM, keyPEM)
	if err != nil {
		return "", err
	}
	return fingerprint, nil
}

// StoreACMECertificate persists the PEM resource returned by lego.
func (p *Postgres) StoreACMECertificate(ctx context.Context, chainPEM, keyPEM []byte) (string, error) {
	entry, err := certificates.ParsePEM(chainPEM, keyPEM, nil)
	if err != nil {
		return "", err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(entry.Certificate.Certificate[0]))
	_, err = p.pool.Exec(ctx, "INSERT INTO gateway_certificate(fingerprint,fingerprint_sha256,dns_names,source,status,is_active,not_before,not_after,certificate_pem,private_key_pem,created_at,updated_at) VALUES($1,$2,$3,'acme','active',true,$4,$5,$6,$7,clock_timestamp(),clock_timestamp()) ON CONFLICT(fingerprint) DO UPDATE SET dns_names=excluded.dns_names,source=excluded.source,status='active',is_active=true,not_before=excluded.not_before,not_after=excluded.not_after,certificate_pem=excluded.certificate_pem,private_key_pem=excluded.private_key_pem,updated_at=clock_timestamp()", fingerprint, fingerprint, entry.Names, entry.NotBefore, entry.NotAfter, chainPEM, keyPEM)
	if err != nil {
		return "", err
	}
	return fingerprint, nil
}

func (p *Postgres) LoadCertificates(ctx context.Context) ([]certificates.Entry, error) {
	chainContext, cancelChains := context.WithTimeout(ctx, certificateChainCompletionBudget)
	defer cancelChains()
	rows, err := p.pool.Query(ctx, "SELECT fingerprint,certificate_pem,private_key_pem FROM gateway_certificate WHERE status='active' AND is_active=true AND not_after > clock_timestamp() ORDER BY not_after DESC,created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []certificates.Entry{}
	for rows.Next() {
		var fingerprint string
		var chainPEM, keyPEM []byte
		if err := rows.Scan(&fingerprint, &chainPEM, &keyPEM); err != nil {
			return nil, err
		}
		entry, err := certificates.ParsePEM(chainPEM, keyPEM, nil)
		if err != nil {
			return nil, fmt.Errorf("certificate %s: %w", fingerprint, err)
		}
		if completed, err := certificates.CompleteChain(chainContext, entry); err == nil {
			entry = completed
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
