// Package webdomain serves the Host (web domain) configuration operations.
package webdomain

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/management"
)

// Store is the web-domain slice of the management repository.
type Store interface {
	UpsertDomain(context.Context, management.DomainUpsertRequest) (management.Domain, error)
	ListDomains(context.Context) ([]management.DomainListItem, error)
	SearchDomains(context.Context, management.WebDomainSearchRequest) (management.Page[management.DomainListItem], error)
	GetDomain(context.Context, string) (management.DomainListItem, error)
	DeleteDomain(context.Context, string) error
}

// ACMERequestResult reports the outcome of an operator-triggered certificate
// request, mirroring acme.ProcessResult without exposing that package here.
type ACMERequestResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ACMEStatusResult is the latest order/certificate snapshot the console polls
// to show a domain's real verification status instead of a static label.
type ACMEStatusResult struct {
	Hostname            string             `json:"hostname"`
	Certificate         *CertificateStatus `json:"certificate"`
	ACMEOrder           *ACMEOrderStatus   `json:"acmeOrder"`
	CheckedAt           time.Time          `json:"checkedAt"`
	HasOrder            bool               `json:"hasOrder"`
	OrderStatus         string             `json:"orderStatus,omitempty"`
	ErrorMessage        string             `json:"errorMessage,omitempty"`
	Attempt             int                `json:"attempt,omitempty"`
	NextAttemptAt       *time.Time         `json:"nextAttemptAt,omitempty"`
	CertificateNotAfter *time.Time         `json:"certificateNotAfter,omitempty"`
}

type CertificateStatus struct {
	Exists           bool      `json:"exists"`
	Validity         string    `json:"validity"`
	IsActive         bool      `json:"isActive"`
	NotBefore        time.Time `json:"notBefore"`
	NotAfter         time.Time `json:"notAfter"`
	RemainingSeconds int64     `json:"remainingSeconds"`
	RemainingDays    int64     `json:"remainingDays"`
	Fingerprint      string    `json:"fingerprint"`
	Issuer           string    `json:"issuer"`
	SerialNumber     string    `json:"serialNumber"`
	DNSNames         []string  `json:"dnsNames"`
	Source           string    `json:"source"`
}

type ACMEOrderStatus struct {
	Exists        bool           `json:"exists"`
	IsInProgress  bool           `json:"isInProgress"`
	ID            *management.ID `json:"id"`
	Status        string         `json:"status"`
	Attempt       int            `json:"attempt"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
	NextAttemptAt *time.Time     `json:"nextAttemptAt,omitempty"`
	FinishedAt    *time.Time     `json:"finishedAt,omitempty"`
	ErrorClass    string         `json:"errorClass"`
	ErrorMessage  string         `json:"errorMessage"`
}

// ACME is the optional ACME-trigger slice. A deployment that does not wire
// one up answers both operations with 501, matching the systemerrors pattern.
type ACME interface {
	RequestNow(ctx context.Context, domainName string) (ACMERequestResult, error)
	Status(ctx context.Context, domainName string) (ACMEStatusResult, error)
}

type Handler struct {
	Store   Store
	ACME    ACME
	BodyMax int64
}

func (h Handler) Routes(r chi.Router) {
	r.Post("/web_domain/upsert", h.Upsert)
	r.Get("/web_domain/search", h.Search)
	r.Post("/web_domain/search", h.Search)
	r.Get("/web_domain/get", h.Get)
	r.Get("/web_domain/get_by_name", h.GetByName)
	r.Delete("/web_domain/delete", h.Delete)
	r.Post("/web_domain/acme/request", h.RequestCertificate)
	r.Get("/web_domain/acme/status", h.CertificateStatus)
}

func (h Handler) Upsert(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.DomainUpsertRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := h.Store.UpsertDomain(r.Context(), input)
	management.RespondStore(w, out, err)
}

// Search answers the GET form with the full, unpaged list and the POST form
// with a page, matching the website operation.
func (h Handler) Search(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		out, err := h.Store.ListDomains(r.Context())
		management.RespondStore(w, out, err)
		return
	}
	input, err := management.DecodeTypedJSONBody[management.WebDomainSearchRequest](r.Body, h.BodyMax)
	if err != nil && !errors.Is(err, io.EOF) {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	input.PageRequest, err = management.NormalizePageRequest(input.PageRequest)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := h.Store.SearchDomains(r.Context(), input)
	management.RespondStore(w, out, err)
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := management.RequiredIDQuery(r, "id")
	if err != nil {
		management.Fail(w, http.StatusBadRequest, "invalid query")
		return
	}
	out, err := h.Store.GetDomain(r.Context(), id)
	management.RespondMissingAsNull(w, out, err)
}

func (h Handler) GetByName(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.GetDomain(r.Context(), r.URL.Query().Get("name"))
	management.RespondMissingAsNull(w, out, err)
}

// Delete removes a Host resource by id (legacy) or by name. The name form also
// reaches hosts that exist only as observed traffic.
func (h Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if name := strings.TrimSpace(r.URL.Query().Get("name")); name != "" {
		management.RespondStore(w, management.EmptyResponse{}, h.Store.DeleteDomain(r.Context(), name))
		return
	}
	id, err := management.RequiredIDQuery(r, "id")
	if err != nil {
		management.Fail(w, http.StatusBadRequest, "invalid query")
		return
	}
	management.RespondStore(w, management.EmptyResponse{}, h.Store.DeleteDomain(r.Context(), id))
}

type acmeRequestBody struct {
	Name string `json:"name"`
}

// RequestCertificate is the console's "立即申请证书" action: it runs the exact
// same issuance path the renewal ticker uses, synchronously, and reports the
// outcome (already valid / issued / failed and why / busy).
func (h Handler) RequestCertificate(w http.ResponseWriter, r *http.Request) {
	if h.ACME == nil {
		management.Fail(w, http.StatusNotImplemented, "ACME 未启用")
		return
	}
	input, err := management.DecodeTypedJSONBody[acmeRequestBody](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := h.ACME.RequestNow(r.Context(), input.Name)
	management.RespondStore(w, out, err)
}

func (h Handler) CertificateStatus(w http.ResponseWriter, r *http.Request) {
	if h.ACME == nil {
		management.Fail(w, http.StatusNotImplemented, "ACME 未启用")
		return
	}
	out, err := h.ACME.Status(r.Context(), r.URL.Query().Get("name"))
	management.RespondStore(w, out, err)
}
