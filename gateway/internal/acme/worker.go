package acme

import (
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/repository"
)

type Settings struct {
	DirectoryURL      string
	Email             string
	ScanInterval      time.Duration
	RenewBefore       time.Duration
	LockRetryBase     time.Duration
	LockRetryJitter   time.Duration
	TemporaryRetryMax time.Duration
	Provider          KeyValueProvider
	// Preflight may be overridden by tests or an installation-specific
	// external probe. The default checks the configured provider and HTTP-01.
	Preflight           func(context.Context, string, KeyValueProvider) error
	ObserveAttempt      func(result, errorClass string)
	ObserveLockConflict func()
	RecordSystemError   func(operation, errorCode string, err error)
}

type issuedCertificate struct {
	ChainPEM []byte
	KeyPEM   []byte
	OrderURL string
}

type Worker struct {
	store  *repository.Postgres
	pool   *pgxpool.Pool
	nodeID string
	config Settings
	logger *slog.Logger
	issue  func(context.Context, string) (issuedCertificate, error)
}

type legoUser struct {
	email        string
	registration *registration.Resource
	key          crypto.PrivateKey
}

func (u *legoUser) GetEmail() string                        { return u.email }
func (u *legoUser) GetRegistration() *registration.Resource { return u.registration }
func (u *legoUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

func NewWorker(store *repository.Postgres, pool *pgxpool.Pool, nodeID string, settings Settings, logger *slog.Logger) (*Worker, error) {
	if store == nil || pool == nil || strings.TrimSpace(nodeID) == "" {
		return nil, fmt.Errorf("ACME store, pool and node ID are required")
	}
	if strings.TrimSpace(settings.DirectoryURL) == "" || strings.TrimSpace(settings.Email) == "" {
		return nil, fmt.Errorf("ACME directory URL and email are required")
	}
	if settings.ScanInterval <= 0 {
		settings.ScanInterval = 30 * time.Second
	}
	if settings.RenewBefore <= 0 {
		settings.RenewBefore = 7 * 24 * time.Hour
	}
	if settings.LockRetryBase <= 0 {
		settings.LockRetryBase = time.Minute
	}
	if settings.TemporaryRetryMax <= 0 {
		settings.TemporaryRetryMax = 6 * time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	worker := &Worker{store: store, pool: pool, nodeID: nodeID, config: settings, logger: logger.With("component", "acme")}
	if settings.Preflight == nil {
		worker.config.Preflight = func(ctx context.Context, domain string, provider KeyValueProvider) error {
			return PreflightDomain(ctx, domain, provider, nil, nil)
		}
	}
	worker.issue = worker.issueWithLego
	return worker, nil
}

func (w *Worker) Run(ctx context.Context) {
	w.ScanOnce(ctx)
	ticker := time.NewTicker(w.config.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.ScanOnce(ctx)
		}
	}
}

// ScanOnce is the renewal-only ticker path: it only ever revisits domains
// that already carry an active certificate. First-time issuance (and
// retrying a domain whose first attempt failed) happens exclusively through
// RequestNow, triggered by the console's "立即申请" action.
//
// It never creates an external order before the dedicated advisory lock is
// acquired, so every approved node can run it safely against the same DB.
func (w *Worker) ScanOnce(ctx context.Context) {
	if n, err := w.store.PurgeCompletedACMEOrders(ctx, repository.ACMECompletedOrderRetention); err != nil {
		w.logger.Error("purge completed ACME orders", "error", err)
	} else if n > 0 {
		w.logger.Info("purged completed ACME orders", "count", n)
	}
	domains, err := w.store.ListDomainsDueForRenewal(ctx)
	if err != nil {
		w.logger.Error("scan eligible ACME domains", "error", err)
		w.recordSystemError("scan", "ACME_SCAN_FAILED", err)
		return
	}
	for _, domain := range domains {
		if ctx.Err() != nil {
			return
		}
		w.processDomain(ctx, domain)
	}
}

// ProcessResult reports the outcome of a single domain pass, whether driven
// by the renewal ticker or by an explicit RequestNow call.
type ProcessResult struct {
	Status  string
	Message string
}

const (
	StatusAlreadyValid     = "already_valid"
	StatusIssued           = "issued"
	StatusLockBusy         = "lock_busy"
	StatusPermanentFailure = "permanent_failure"
	StatusTemporaryFailure = "temporary_failure"
	StatusInvalidDomain    = "invalid_domain"
	StatusInternalError    = "internal_error"
	StatusOrderAlreadyBusy = "order_in_progress"
)

// RequestNow is the manual-trigger entry point used by the console's "立即
// 申请证书" action. It clears any pending retry backoff so an operator does
// not have to wait out the exponential delay, then runs the exact same
// issuance path the renewal ticker uses — the uniqueness index and advisory
// lock already make concurrent/duplicate requests for the same domain safe,
// so no extra de-duplication is needed here.
func (w *Worker) RequestNow(ctx context.Context, domain repository.ACMEDomain) ProcessResult {
	normalized := normalizeDomain(domain.Name)
	if normalized != "" {
		if err := w.store.ForceRetryNow(ctx, normalized); err != nil {
			w.logger.Error("reset ACME retry backoff", "domain", normalized, "error", err)
		}
	}
	w.logger.Info("ACME immediate issuance requested", "domain", domain.Name, "trigger", "manual")
	return w.processDomain(ctx, domain)
}

func (w *Worker) processDomain(ctx context.Context, domain repository.ACMEDomain) ProcessResult {
	normalized := normalizeDomain(domain.Name)
	if normalized == "" {
		// No CA or key-value call is made for a domain that HTTP-01 cannot
		// support. The operator receives a stable message through the legacy UI.
		message := "域名格式无效或通配符域名不能使用 HTTP-01 自动证书"
		if err := w.store.FailInvalidACMEDomain(ctx, domain.ID, domain.Name, w.nodeID, message); err != nil {
			w.logger.Error("record invalid ACME domain", "domain", domain.Name, "error", err)
		}
		w.observeAttempt("failure", "invalid")
		return ProcessResult{Status: StatusInvalidDomain, Message: message}
	}
	due, err := w.store.CertificateDue(ctx, normalized, time.Now().UTC().Add(w.config.RenewBefore))
	if err != nil {
		w.logger.Error("check ACME certificate due", "domain", normalized, "error", err)
		w.recordSystemError("certificate_due", "ACME_CERTIFICATE_DUE_CHECK_FAILED", err)
		w.observeAttempt("failure", "other")
		return ProcessResult{Status: StatusInternalError, Message: err.Error()}
	}
	if !due {
		return ProcessResult{Status: StatusAlreadyValid, Message: "证书已有效，无需重新申请"}
	}
	lock, acquired, err := AcquireDomainLock(ctx, w.pool, normalized)
	if err != nil {
		w.logger.Error("acquire ACME domain lock", "domain", normalized, "error", err)
		w.recordSystemError("domain_lock", "ACME_DOMAIN_LOCK_FAILED", err)
		w.observeAttempt("failure", "lock")
		return ProcessResult{Status: StatusInternalError, Message: err.Error()}
	}
	if !acquired {
		w.observeAttempt("skipped", "lock")
		if w.config.ObserveLockConflict != nil {
			w.config.ObserveLockConflict()
		}
		if err := w.store.RecordACMELockConflict(ctx, normalized, w.nodeID, time.Now().UTC().Add(w.LockRetryDelay())); err != nil {
			w.logger.Error("record ACME lock conflict", "domain", normalized, "error", err)
		}
		return ProcessResult{Status: StatusLockBusy, Message: "该域名正在被其他节点处理，请稍后重试"}
	}
	defer lock.Release(context.Background())
	if w.config.Preflight != nil {
		if err := w.config.Preflight(ctx, normalized, w.config.Provider); err != nil {
			var preflightErr *PreflightError
			class := "temporary"
			if errors.As(err, &preflightErr) {
				class = preflightErr.Class
			}
			w.observeAttempt("failure", class)
			w.recordSystemError("preflight", "ACME_PREFLIGHT_FAILED", err)
			w.logger.Warn("ACME preflight blocked order", "domain", normalized, "error", err)
			return ProcessResult{Status: StatusTemporaryFailure, Message: err.Error()}
		}
	}
	orderID, started, err := w.store.StartACMEOrder(ctx, normalized, w.nodeID)
	if err != nil {
		w.logger.Error("start ACME order", "domain", normalized, "error", err)
		w.recordSystemError("order_start", "ACME_ORDER_START_FAILED", err)
		w.observeAttempt("failure", "other")
		return ProcessResult{Status: StatusInternalError, Message: err.Error()}
	}
	if !started {
		return ProcessResult{Status: StatusOrderAlreadyBusy, Message: "该域名已有一次申请正在进行中"}
	}
	attempt, attemptErr := w.store.ACMEOrderAttempt(ctx, orderID)
	if attemptErr != nil {
		attempt = 0
	}
	w.logger.Info("ACME order started", "domain", normalized, "order", orderID, "attempt", attempt)
	if err := w.store.MarkACMEAuthorizing(ctx, orderID, ""); err != nil {
		_ = w.store.RecordACMEOrderEvent(ctx, orderID, "authorizing", "authorizing", "failure", "ACME_ORDER_UPDATE_FAILED", err.Error())
		w.logger.Error("mark ACME order authorizing", "order", orderID, "error", err)
		w.recordSystemError("order_authorizing", "ACME_ORDER_UPDATE_FAILED", err)
		w.observeAttempt("failure", "other")
		return ProcessResult{Status: StatusInternalError, Message: err.Error()}
	}
	issued, err := w.issue(ctx, normalized)
	if err != nil {
		return w.recordIssueFailure(ctx, domain, orderID, attempt, err)
	}
	if err := w.store.MarkACMEFinalizing(ctx, orderID, issued.OrderURL); err != nil {
		_ = w.store.RecordACMEOrderEvent(ctx, orderID, "finalizing", "finalizing", "failure", "ACME_ORDER_UPDATE_FAILED", err.Error())
		w.recordSystemError("order_finalizing", "ACME_ORDER_UPDATE_FAILED", err)
		return w.recordIssueFailure(ctx, domain, orderID, attempt, err)
	}
	if err := w.store.CompleteACMEOrder(ctx, domain.ID, orderID, normalized, issued.ChainPEM, issued.KeyPEM); err != nil {
		_ = w.store.RecordACMEOrderEvent(ctx, orderID, "certificate_store", "finalizing", "failure", "ACME_CERTIFICATE_STORE_FAILED", err.Error())
		w.recordSystemError("certificate_store", "ACME_CERTIFICATE_STORE_FAILED", err)
		return w.recordIssueFailure(ctx, domain, orderID, attempt, err)
	}
	w.observeAttempt("success", "none")
	w.logger.Info("ACME certificate activated", "domain", normalized, "order", orderID, "attempt", attempt)
	return ProcessResult{Status: StatusIssued, Message: "证书申请成功"}
}

func (w *Worker) recordIssueFailure(ctx context.Context, domain repository.ACMEDomain, orderID int64, attempt int, err error) ProcessResult {
	if permanentACMEError(err) {
		w.observeAttempt("failure", "permanent")
		w.logger.Error("ACME issuance failed permanently", "domain", domain.Name, "order", orderID, "attempt", attempt, "error", err)
		if saveErr := w.store.FailACMEOrder(ctx, domain.ID, orderID, err.Error()); saveErr != nil {
			w.logger.Error("record permanent ACME error", "domain", domain.Name, "error", saveErr)
			w.recordSystemError("order_failure_record", "ACME_ORDER_UPDATE_FAILED", saveErr)
		}
		w.recordSystemError("certificate_issue", "ACME_CERTIFICATE_ISSUE_FAILED", err)
		return ProcessResult{Status: StatusPermanentFailure, Message: err.Error()}
	}
	w.observeAttempt("failure", "temporary")
	retry := time.Now().UTC().Add(temporaryRetry(attempt, w.config.TemporaryRetryMax))
	w.logger.Error("ACME issuance failed, will retry", "domain", domain.Name, "order", orderID, "attempt", attempt, "nextAttemptAt", retry, "error", err)
	if saveErr := w.store.RetryACMEOrder(ctx, orderID, "temporary", err.Error(), retry); saveErr != nil {
		w.logger.Error("record temporary ACME error", "domain", domain.Name, "error", saveErr)
		w.recordSystemError("order_failure_record", "ACME_ORDER_UPDATE_FAILED", saveErr)
	}
	w.recordSystemError("certificate_issue", "ACME_CERTIFICATE_ISSUE_FAILED", err)
	return ProcessResult{Status: StatusTemporaryFailure, Message: err.Error()}
}

func (w *Worker) recordSystemError(operation, code string, err error) {
	if w.config.RecordSystemError != nil && err != nil {
		w.config.RecordSystemError(operation, code, err)
	}
}

func (w *Worker) observeAttempt(result, errorClass string) {
	if w.config.ObserveAttempt != nil {
		w.config.ObserveAttempt(result, errorClass)
	}
}

func (w *Worker) issueWithLego(ctx context.Context, domain string) (issuedCertificate, error) {
	if err := ctx.Err(); err != nil {
		return issuedCertificate{}, err
	}
	account, err := w.loadOrCreateAccount(ctx)
	if err != nil {
		return issuedCertificate{}, err
	}
	client, err := w.newLegoClient(account)
	if err != nil {
		return issuedCertificate{}, err
	}
	if account.registration == nil {
		account, client, err = w.ensureRegisteredAccount(ctx, account)
		if err != nil {
			return issuedCertificate{}, err
		}
	}
	resource, err := client.Certificate.Obtain(certificate.ObtainRequest{Domains: []string{domain}, Bundle: true})
	if err != nil {
		return issuedCertificate{}, err
	}
	return issuedCertificate{ChainPEM: resource.Certificate, KeyPEM: resource.PrivateKey, OrderURL: resource.CertURL}, nil
}

func (w *Worker) newLegoClient(account *legoUser) (*lego.Client, error) {
	clientConfig := lego.NewConfig(account)
	clientConfig.CADirURL = w.config.DirectoryURL
	clientConfig.Certificate.KeyType = certcrypto.EC256
	clientConfig.HTTPClient = &http.Client{Timeout: 10 * time.Minute}
	client, err := lego.NewClient(clientConfig)
	if err != nil {
		return nil, err
	}
	if err := client.Challenge.SetHTTP01Provider(w.config.Provider); err != nil {
		return nil, err
	}
	return client, nil
}

func (w *Worker) ensureRegisteredAccount(ctx context.Context, account *legoUser) (*legoUser, *lego.Client, error) {
	lock, acquired, err := AcquireAccountLock(ctx, w.pool, w.config.DirectoryURL)
	if err != nil {
		return nil, nil, err
	}
	if !acquired {
		return nil, nil, fmt.Errorf("ACME account registration is busy")
	}
	defer lock.Release(context.Background())
	stored, exists, err := w.store.LoadACMEAccount(ctx, w.config.DirectoryURL)
	if err != nil {
		return nil, nil, err
	}
	if exists && len(stored.RegistrationJSON) > 0 {
		key, err := certcrypto.ParsePEMPrivateKey(stored.PrivateKeyPEM)
		if err != nil {
			return nil, nil, fmt.Errorf("parse stored ACME account key: %w", err)
		}
		registrationValue := new(registration.Resource)
		if err := json.Unmarshal(stored.RegistrationJSON, registrationValue); err != nil {
			return nil, nil, fmt.Errorf("parse stored ACME registration: %w", err)
		}
		account = &legoUser{email: stored.Email, key: key, registration: registrationValue}
		client, err := w.newLegoClient(account)
		return account, client, err
	}
	client, err := w.newLegoClient(account)
	if err != nil {
		return nil, nil, err
	}
	registrationValue, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, nil, fmt.Errorf("register ACME account: %w", err)
	}
	account.registration = registrationValue
	if err := w.persistAccount(ctx, account); err != nil {
		return nil, nil, err
	}
	return account, client, nil
}

func (w *Worker) loadOrCreateAccount(ctx context.Context) (*legoUser, error) {
	lock, acquired, err := AcquireAccountLock(ctx, w.pool, w.config.DirectoryURL)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, fmt.Errorf("ACME account registration is busy")
	}
	defer lock.Release(context.Background())
	saved, exists, err := w.store.LoadACMEAccount(ctx, w.config.DirectoryURL)
	if err != nil {
		return nil, err
	}
	if exists {
		key, err := certcrypto.ParsePEMPrivateKey(saved.PrivateKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("parse stored ACME account key: %w", err)
		}
		account := &legoUser{email: saved.Email, key: key}
		if len(saved.RegistrationJSON) > 0 {
			registrationValue := new(registration.Resource)
			if err := json.Unmarshal(saved.RegistrationJSON, registrationValue); err != nil {
				return nil, fmt.Errorf("parse stored ACME registration: %w", err)
			}
			account.registration = registrationValue
		}
		return account, nil
	}
	key, err := certcrypto.GeneratePrivateKey(certcrypto.EC256)
	if err != nil {
		return nil, err
	}
	account := &legoUser{email: w.config.Email, key: key}
	if err := w.persistAccount(ctx, account); err != nil {
		return nil, err
	}
	return account, nil
}

func (w *Worker) persistAccount(ctx context.Context, account *legoUser) error {
	var registrationJSON []byte
	var accountURL string
	if account.registration != nil {
		var err error
		registrationJSON, err = json.Marshal(account.registration)
		if err != nil {
			return err
		}
		accountURL = account.registration.URI
	}
	_, err := w.store.SaveACMEAccount(ctx, w.config.DirectoryURL, account.email, accountURL, certcrypto.PEMEncode(account.key), registrationJSON)
	return err
}

func permanentACMEError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "rejectedidentifier") || strings.Contains(value, "malformed") || strings.Contains(value, "unsupportedidentifier") || strings.Contains(value, "badcsr") || strings.Contains(value, "private key") || strings.Contains(value, "certificate") && strings.Contains(value, "parse")
}

func temporaryRetry(attempt int, maximum time.Duration) time.Duration {
	steps := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}
	index := attempt - 1
	if index < 0 {
		index = 0
	}
	if index >= len(steps) {
		index = len(steps) - 1
	}
	result := steps[index]
	if maximum > 0 && result > maximum {
		return maximum
	}
	return result
}

func (w *Worker) LockRetryDelay() time.Duration {
	delay := w.config.LockRetryBase
	if w.config.LockRetryJitter > 0 {
		delay += time.Duration(rand.Int64N(int64(w.config.LockRetryJitter) + 1))
	}
	return delay
}
