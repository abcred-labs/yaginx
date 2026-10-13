package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"yaginx-gateway/internal/certificates"
	"yaginx-gateway/internal/management"
	"yaginx-gateway/internal/platform/hostnorm"
)

// ACMEDomain is the legacy row selected by the renewal scanner. The numeric
// ID lets terminal failures update exactly the operator-managed domain row.
type ACMEDomain struct {
	ID   int64
	Name string
}

type ACMEAccount struct {
	ID               int64
	DirectoryURL     string
	Email            string
	AccountURL       string
	PrivateKeyPEM    []byte
	RegistrationJSON []byte
}

type acmeCertificateMaterial struct {
	fingerprint string
	names       []string
	notBefore   time.Time
	notAfter    time.Time
	chainPEM    []byte
	keyPEM      []byte
}

// GetACMEDomainByName resolves the web_domain row the console's "立即申请"
// action targets. ok is false when no such domain is registered.
func (p *Postgres) GetACMEDomainByName(ctx context.Context, name string) (domain ACMEDomain, isFreeCert bool, ok bool, err error) {
	err = p.pool.QueryRow(ctx, "SELECT id,name,is_use_free_cert FROM web_domain WHERE name=$1", name).Scan(&domain.ID, &domain.Name, &isFreeCert)
	if errors.Is(err, pgx.ErrNoRows) {
		return ACMEDomain{}, false, false, nil
	}
	if err != nil {
		return ACMEDomain{}, false, false, err
	}
	return domain, isFreeCert, true, nil
}

// ListDomainsDueForRenewal selects domains that already carry an active
// certificate. The background scan is renewal-only: a domain that has never
// been issued a certificate is only ever processed through an explicit
// RequestNow call, never by the ticker.
func (p *Postgres) ListDomainsDueForRenewal(ctx context.Context) ([]ACMEDomain, error) {
	rows, err := p.pool.Query(ctx, `SELECT id,name FROM web_domain
WHERE is_use_free_cert=true
AND EXISTS (SELECT 1 FROM gateway_certificate WHERE is_active=true AND status='active' AND web_domain.name=ANY(dns_names))
ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ACMEDomain
	for rows.Next() {
		var value ACMEDomain
		if err := rows.Scan(&value.ID, &value.Name); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// ForceRetryNow clears the exponential backoff on a domain's pending retry so
// an operator-triggered request is not blocked by it. Only the retry_wait
// status is affected; pending/failed rows are already free to start at once.
func (p *Postgres) ForceRetryNow(ctx context.Context, domain string) error {
	_, err := p.pool.Exec(ctx, "UPDATE gateway_certificate_order SET next_attempt_at=clock_timestamp(),updated_at=clock_timestamp() WHERE domain_name=$1 AND status='retry_wait'", domain)
	return err
}

// ACMEDomainStatus is the latest order/certificate snapshot for a domain,
// used to answer the console's status polling.
type ACMEDomainStatus struct {
	Domain              string
	HasOrder            bool
	OrderStatus         string
	ErrorMessage        string
	Attempt             int
	NextAttemptAt       *time.Time
	CertificateNotAfter *time.Time
	Hostname            string
	Certificate         *ACMECertificateStatus
	Order               *ACMEOrderStatus
	CheckedAt           time.Time
}

type ACMECertificateStatus struct {
	Exists           bool
	Validity         string
	IsActive         bool
	NotBefore        time.Time
	NotAfter         time.Time
	RemainingSeconds int64
	RemainingDays    int64
	Fingerprint      string
	Issuer           string
	SerialNumber     string
	DNSNames         []string
	Source           string
}

type ACMEOrderStatus struct {
	Exists        bool
	IsInProgress  bool
	ID            management.ID
	Status        string
	Attempt       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
	NextAttemptAt *time.Time
	FinishedAt    *time.Time
	ErrorClass    string
	ErrorMessage  string
}

var acmeInProgressStatuses = map[string]bool{
	"pending": true, "locked": true, "authorizing": true, "finalizing": true, "retry_wait": true,
}

func certificateValidity(notBefore, notAfter, now time.Time) string {
	switch {
	case now.Before(notBefore):
		return "not_yet_valid"
	case now.After(notAfter):
		return "expired"
	default:
		return "valid"
	}
}

func (p *Postgres) GetACMEDomainStatus(ctx context.Context, domain string) (ACMEDomainStatus, error) {
	normalized, ok := hostnorm.IPOrHost(domain)
	if !ok {
		return ACMEDomainStatus{}, fmt.Errorf("invalid host %q", domain)
	}
	status := ACMEDomainStatus{Domain: normalized, Hostname: normalized}
	var order ACMEOrderStatus
	var nextAttemptAt, finishedAt *time.Time
	err := p.pool.QueryRow(ctx, `SELECT id,status,COALESCE(error_class,''),COALESCE(error_message,''),attempt,created_at,updated_at,next_attempt_at,finished_at
FROM gateway_certificate_order WHERE domain_name=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, normalized).
		Scan(&order.ID, &order.Status, &order.ErrorClass, &order.ErrorMessage, &order.Attempt, &order.CreatedAt, &order.UpdatedAt, &nextAttemptAt, &finishedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return status, err
	}
	if err == nil {
		status.HasOrder = true
		order.Exists = true
		order.IsInProgress = acmeInProgressStatuses[order.Status]
		order.NextAttemptAt = nextAttemptAt
		order.FinishedAt = finishedAt
		status.Order = &order
		status.OrderStatus = order.Status
		status.ErrorMessage = order.ErrorMessage
		status.Attempt = order.Attempt
		status.NextAttemptAt = nextAttemptAt
	}
	var certificate ACMECertificateStatus
	err = p.pool.QueryRow(ctx, `SELECT fingerprint_sha256,COALESCE(issuer,''),COALESCE(serial_number,''),dns_names,not_before,not_after,source,is_active
FROM gateway_certificate c
WHERE lower(c.status)='active' AND EXISTS (
  SELECT 1 FROM unnest(c.dns_names) AS dns_name
  WHERE lower(trim(trailing '.' from dns_name))=$1
) ORDER BY c.is_active DESC,c.not_after DESC,c.id DESC LIMIT 1`, normalized).
		Scan(&certificate.Fingerprint, &certificate.Issuer, &certificate.SerialNumber, &certificate.DNSNames, &certificate.NotBefore, &certificate.NotAfter, &certificate.Source, &certificate.IsActive)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return status, err
	}
	now := time.Now().UTC()
	status.CheckedAt = now
	if err == nil {
		certificate.Exists = true
		certificate.RemainingSeconds = int64(certificate.NotAfter.Sub(now).Seconds())
		certificate.RemainingDays = int64(certificate.NotAfter.Sub(now).Hours() / 24)
		certificate.Validity = certificateValidity(certificate.NotBefore, certificate.NotAfter, now)
		status.Certificate = &certificate
		status.CertificateNotAfter = &certificate.NotAfter
	}
	return status, nil
}

func (p *Postgres) CertificateDue(ctx context.Context, normalizedDomain string, renewBefore time.Time) (bool, error) {
	var exists bool
	err := p.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM gateway_certificate WHERE is_active=true AND status='active' AND not_after > $2 AND $1=ANY(dns_names))", normalizedDomain, renewBefore).Scan(&exists)
	return !exists, err
}

func (p *Postgres) LoadACMEAccount(ctx context.Context, directoryURL string) (ACMEAccount, bool, error) {
	var account ACMEAccount
	err := p.pool.QueryRow(ctx, "SELECT id,directory_url,email,account_url,private_key_pem,registration_json FROM gateway_acme_account WHERE directory_url=$1", directoryURL).Scan(&account.ID, &account.DirectoryURL, &account.Email, &account.AccountURL, &account.PrivateKeyPEM, &account.RegistrationJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return ACMEAccount{}, false, nil
	}
	if err != nil {
		return ACMEAccount{}, false, err
	}
	if len(account.PrivateKeyPEM) == 0 {
		return ACMEAccount{}, false, fmt.Errorf("ACME account has no private key")
	}
	return account, true, nil
}

func (p *Postgres) SaveACMEAccount(ctx context.Context, directoryURL, email, accountURL string, privateKeyPEM, registrationJSON []byte) (ACMEAccount, error) {
	if strings.TrimSpace(directoryURL) == "" || strings.TrimSpace(email) == "" || len(privateKeyPEM) == 0 {
		return ACMEAccount{}, fmt.Errorf("ACME account directory, email and private key are required")
	}
	// pgx encodes a nil slice as SQL NULL, while an unregistered account is
	// represented by an empty registration payload.
	if registrationJSON == nil {
		registrationJSON = []byte{}
	}
	var account ACMEAccount
	err := p.pool.QueryRow(ctx, "INSERT INTO gateway_acme_account(directory_url,email,account_url,private_key_pem,registration_json,created_at,updated_at) VALUES($1,$2,$3,$4,$5,clock_timestamp(),clock_timestamp()) ON CONFLICT(directory_url) DO UPDATE SET email=excluded.email,account_url=excluded.account_url,private_key_pem=excluded.private_key_pem,registration_json=excluded.registration_json,updated_at=clock_timestamp() RETURNING id,directory_url,email,account_url", directoryURL, email, accountURL, privateKeyPEM, registrationJSON).Scan(&account.ID, &account.DirectoryURL, &account.Email, &account.AccountURL)
	if err != nil {
		return ACMEAccount{}, err
	}
	account.PrivateKeyPEM, account.RegistrationJSON = privateKeyPEM, registrationJSON
	return account, nil
}

const (
	acmeKeyValueMaxValue = 64 << 10
)

// SetACMEKeyValue persists an HTTP-01 key authorization for the shared
// provider endpoint. The caller supplies the expiry so tests and retention
// jobs can use deterministic database timestamps.
func (p *Postgres) SetACMEKeyValue(ctx context.Context, key, value string, expiresAt time.Time) error {
	if !validACMEKeyValueKey(key) {
		return fmt.Errorf("invalid ACME key-value key")
	}
	if value == "" || len(value) > acmeKeyValueMaxValue {
		return fmt.Errorf("invalid ACME key-value value")
	}
	if !expiresAt.After(time.Now().UTC()) {
		return fmt.Errorf("ACME key-value expiry must be in the future")
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO gateway_acme_key_value(key,value,expires_at,created_at,updated_at)
VALUES($1,$2,$3,clock_timestamp(),clock_timestamp())
ON CONFLICT(key) DO UPDATE SET value=excluded.value,expires_at=excluded.expires_at,updated_at=clock_timestamp()`,
		key, value, expiresAt.UTC())
	return err
}

// GetACMEKeyValue returns only unexpired values according to PostgreSQL time.
func (p *Postgres) GetACMEKeyValue(ctx context.Context, key string) (string, bool, error) {
	if !validACMEKeyValueKey(key) {
		return "", false, fmt.Errorf("invalid ACME key-value key")
	}
	var value string
	err := p.pool.QueryRow(ctx, `SELECT value FROM gateway_acme_key_value
WHERE key=$1 AND expires_at > clock_timestamp()`, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// PruneACMEKeyValues deletes expired challenge rows.
func (p *Postgres) PruneACMEKeyValues(ctx context.Context, now time.Time) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM gateway_acme_key_value WHERE expires_at <= $1`, now.UTC())
	return err
}

func validACMEKeyValueKey(value string) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// ImportACMEAccountIfAbsent preserves a Go-managed registration on repeated
// legacy imports. A legacy LettuceEncrypt account does not contain the ACME
// registration resource, so replacing an account that has since been
// registered by lego would lose its account URL and registration JSON.
func (p *Postgres) ImportACMEAccountIfAbsent(ctx context.Context, directoryURL, email string, privateKeyPEM []byte) (bool, error) {
	if strings.TrimSpace(directoryURL) == "" || strings.TrimSpace(email) == "" || len(privateKeyPEM) == 0 {
		return false, fmt.Errorf("ACME account directory, email and private key are required")
	}
	var exists bool
	if err := p.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM gateway_acme_account WHERE directory_url=$1)", directoryURL).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	command, err := p.pool.Exec(ctx, "INSERT INTO gateway_acme_account(directory_url,email,account_url,private_key_pem,registration_json,created_at,updated_at) VALUES($1,$2,'',$3,''::bytea,clock_timestamp(),clock_timestamp()) ON CONFLICT(directory_url) DO NOTHING", directoryURL, email, privateKeyPEM)
	if err != nil {
		return false, err
	}
	return command.RowsAffected() == 1, nil
}

// StartACMEOrder transitions at most one locally owned due row to locked. It
// is called only after the dedicated domain advisory lock has been acquired.
// started is false when an existing retry has not reached its scheduled time.
func (p *Postgres) StartACMEOrder(ctx context.Context, domain, nodeID string) (orderID int64, started bool, err error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, "UPDATE gateway_certificate_order SET status='locked',node_id=$2::uuid,attempt=attempt+1,error_class='',error_message='',next_attempt_at=NULL,started_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=(SELECT id FROM gateway_certificate_order WHERE domain_name=$1 AND status IN ('pending','retry_wait') AND (next_attempt_at IS NULL OR next_attempt_at<=clock_timestamp()) ORDER BY created_at DESC LIMIT 1) RETURNING id", domain, nodeID).Scan(&orderID)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return 0, false, err
		}
		p.recordACMEOrderEvent(ctx, orderID, "lock", "locked", "started", "", "order processing started")
		return orderID, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, err
	}
	var retryScheduled bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM gateway_certificate_order WHERE domain_name=$1 AND status IN ('pending','retry_wait'))", domain).Scan(&retryScheduled); err != nil {
		return 0, false, err
	}
	if retryScheduled {
		if err := tx.Commit(ctx); err != nil {
			return 0, false, err
		}
		return 0, false, nil
	}
	if err := tx.QueryRow(ctx, "INSERT INTO gateway_certificate_order(domains,domain_name,status,node_id,attempt,order_url,error_class,error_message,next_attempt_at,started_at,created_at,updated_at) VALUES(ARRAY[$1],$1,'locked',$2::uuid,1,'','',NULL,NULL,clock_timestamp(),clock_timestamp(),clock_timestamp()) RETURNING id", domain, nodeID).Scan(&orderID); err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	p.recordACMEOrderEvent(ctx, orderID, "create", "locked", "created", "", "order created")
	return orderID, true, nil
}

// RecordACMELockConflict persists the retry schedule without creating an
// external CA order. Multiple contenders can leave retry rows; the domain
// advisory lock serializes which one later becomes locked.
func (p *Postgres) RecordACMELockConflict(ctx context.Context, domain, nodeID string, retryAt time.Time) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, "UPDATE gateway_certificate_order SET status='retry_wait',node_id=$2::uuid,error_class='lock_conflict',error_message='',next_attempt_at=$3,updated_at=clock_timestamp() WHERE id=(SELECT id FROM gateway_certificate_order WHERE domain_name=$1 AND status IN ('pending','retry_wait') ORDER BY created_at DESC LIMIT 1) RETURNING id", domain, nodeID, retryAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, "INSERT INTO gateway_certificate_order(domains,domain_name,status,node_id,attempt,order_url,error_class,error_message,next_attempt_at,created_at,updated_at) VALUES(ARRAY[$1],$1,'retry_wait',$2::uuid,0,'','lock_conflict','',$3,clock_timestamp(),clock_timestamp()) RETURNING id", domain, nodeID, retryAt).Scan(&id)
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	p.recordACMEOrderEvent(ctx, id, "lock_conflict", "retry_wait", "retry_scheduled", "lock_conflict", "another node holds the domain lock")
	return nil
}

func (p *Postgres) ACMEOrderAttempt(ctx context.Context, orderID int64) (int, error) {
	var attempt int
	err := p.pool.QueryRow(ctx, "SELECT attempt FROM gateway_certificate_order WHERE id=$1", orderID).Scan(&attempt)
	return attempt, err
}

func (p *Postgres) MarkACMEAuthorizing(ctx context.Context, orderID int64, orderURL string) error {
	_, err := p.pool.Exec(ctx, "UPDATE gateway_certificate_order SET status='authorizing',order_url=$2,acme_order_url=$2,updated_at=clock_timestamp() WHERE id=$1 AND status='locked'", orderID, orderURL)
	if err == nil {
		p.recordACMEOrderEvent(ctx, orderID, "authorizing", "authorizing", "started", "", "challenge authorization started")
	}
	return err
}

func (p *Postgres) MarkACMEFinalizing(ctx context.Context, orderID int64, orderURL string) error {
	_, err := p.pool.Exec(ctx, "UPDATE gateway_certificate_order SET status='finalizing',order_url=CASE WHEN $2='' THEN order_url ELSE $2 END,acme_order_url=CASE WHEN $2='' THEN acme_order_url ELSE $2 END,updated_at=clock_timestamp() WHERE id=$1 AND status IN ('locked','authorizing')", orderID, orderURL)
	if err == nil {
		p.recordACMEOrderEvent(ctx, orderID, "finalizing", "finalizing", "started", "", "certificate finalization started")
	}
	return err
}

func (p *Postgres) RetryACMEOrder(ctx context.Context, orderID int64, class, message string, retryAt time.Time) error {
	_, err := p.pool.Exec(ctx, "UPDATE gateway_certificate_order SET status='retry_wait',error_class=$2,error_message=$3,next_attempt_at=$4,updated_at=clock_timestamp() WHERE id=$1 AND status NOT IN ('valid','failed','canceled')", orderID, class, sanitizeACMEError(message), retryAt)
	if err == nil {
		p.recordACMEOrderEvent(ctx, orderID, "retry", "retry_wait", "scheduled", class, sanitizeACMEError(message))
	}
	return err
}

func (p *Postgres) FailACMEOrder(ctx context.Context, domainID, orderID int64, message string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "UPDATE gateway_certificate_order SET status='failed',error_class='permanent',error_message=$2,finished_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1", orderID, sanitizeACMEError(message)); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE web_domain SET is_use_free_cert=false,free_cert_message=$2 WHERE id=$1", domainID, stableFreeCertMessage(message)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	p.recordACMEOrderEvent(ctx, orderID, "failed", "failed", "failure", "permanent", sanitizeACMEError(message))
	return nil
}

// FailInvalidACMEDomain records an input-level permanent failure without ever
// acquiring a CA order or touching the HTTP-01 key-value service.
func (p *Postgres) FailInvalidACMEDomain(ctx context.Context, domainID int64, domain, nodeID, message string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var orderID int64
	err = tx.QueryRow(ctx, "INSERT INTO gateway_certificate_order(domains,domain_name,status,node_id,attempt,order_url,error_class,error_message,finished_at,created_at,updated_at) VALUES(ARRAY[$1],lower(trim(trailing '.' FROM $1)),'failed',$2::uuid,0,'','permanent',$3,clock_timestamp(),clock_timestamp(),clock_timestamp()) RETURNING id", domain, nodeID, sanitizeACMEError(message)).Scan(&orderID)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE web_domain SET is_use_free_cert=false,free_cert_message=$2 WHERE id=$1", domainID, stableFreeCertMessage(message)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	p.recordACMEOrderEvent(ctx, orderID, "invalid_domain", "failed", "failure", "permanent", sanitizeACMEError(message))
	return nil
}

// CompleteACMEOrder validates, stores and activates the new chain
// before superseding matching certificates. The certificate trigger only fires
// on commit, so peers never observe a TLS empty window.
func (p *Postgres) CompleteACMEOrder(ctx context.Context, domainID, orderID int64, normalizedDomain string, chainPEM, keyPEM []byte) error {
	material, err := p.prepareACMECertificate(chainPEM, keyPEM)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var certificateID int64
	err = tx.QueryRow(ctx, "INSERT INTO gateway_certificate(fingerprint,fingerprint_sha256,dns_names,source,status,is_active,not_before,not_after,certificate_pem,private_key_pem,created_at,updated_at) VALUES($1,$2,$3,'acme','active',true,$4,$5,$6,$7,clock_timestamp(),clock_timestamp()) ON CONFLICT(fingerprint) DO UPDATE SET dns_names=excluded.dns_names,source='acme',status='active',is_active=true,not_before=excluded.not_before,not_after=excluded.not_after,certificate_pem=excluded.certificate_pem,private_key_pem=excluded.private_key_pem,updated_at=clock_timestamp() RETURNING id", material.fingerprint, material.fingerprint, material.names, material.notBefore, material.notAfter, material.chainPEM, material.keyPEM).Scan(&certificateID)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE gateway_certificate SET is_active=false,replaced_by_id=$2,updated_at=clock_timestamp() WHERE is_active=true AND id<>$2 AND $1=ANY(dns_names)", normalizedDomain, certificateID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE gateway_certificate_order SET status='valid',certificate_id=$2,error_class='',error_message='',next_attempt_at=NULL,finished_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1", orderID, certificateID); err != nil {
		return err
	}
	if err = cancelPendingACMEOrders(ctx, tx, normalizedDomain, orderID, "superseded"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE web_domain SET is_use_free_cert=true,free_cert_message=NULL WHERE id=$1", domainID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	p.recordACMEOrderEvent(ctx, orderID, "complete", "valid", "success", "", "certificate issued and activated")
	return nil
}

// cancelPendingACMEOrders cancels every non-terminal order for a domain other
// than keepOrderID (pass 0 to cancel all of them). Used both when a
// certificate is successfully issued (superseding stale retries) and when a
// website save removes a host that no longer needs its in-flight order.
func cancelPendingACMEOrders(ctx context.Context, tx pgx.Tx, normalizedDomain string, keepOrderID int64, reason string) error {
	_, err := tx.Exec(ctx, "UPDATE gateway_certificate_order SET status='canceled',error_class='operator_canceled',error_message=$3,next_attempt_at=NULL,finished_at=clock_timestamp(),updated_at=clock_timestamp() WHERE domain_name=$1 AND id<>$2 AND status IN ('pending','locked','authorizing','finalizing','retry_wait')", normalizedDomain, keepOrderID, reason)
	return err
}

func (p *Postgres) prepareACMECertificate(chainPEM, keyPEM []byte) (acmeCertificateMaterial, error) {
	entry, err := certificates.ParsePEM(chainPEM, keyPEM, nil)
	if err != nil {
		return acmeCertificateMaterial{}, err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(entry.Certificate.Certificate[0]))
	return acmeCertificateMaterial{fingerprint: fingerprint, names: entry.Names, notBefore: entry.NotBefore, notAfter: entry.NotAfter, chainPEM: chainPEM, keyPEM: keyPEM}, nil
}

func sanitizeACMEError(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}

func stableFreeCertMessage(_ string) string {
	return "免费证书申请失败，请检查域名解析和验证配置后重新开启。"
}
