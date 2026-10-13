// Package analytics serves every traffic aggregate the console charts. The
// seven operations here replace the former observability, resource_view,
// host_traffic and resource_report stacks, which each answered the same
// question with a different request and response shape.
package analytics

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/management"
)

// Store is the analytics slice of the management repository.
type Store interface {
	AnalyticsSummary(context.Context, management.AnalyticsQuery) (management.AnalyticsSummary, error)
	AnalyticsDomains(context.Context, management.AnalyticsQuery) (management.AnalyticsDomains, error)
	AnalyticsTimeseries(context.Context, management.AnalyticsQuery) (management.AnalyticsTimeseries, error)
	AnalyticsContributions(context.Context, management.AnalyticsQuery) (management.AnalyticsContributions, error)
	AnalyticsDetails(context.Context, management.AnalyticsPagedQuery) (management.AnalyticsDetails, error)
	AnalyticsOverview(context.Context, management.AnalyticsOverviewQuery) (management.AnalyticsOverview, error)
	ResourceCatalog(context.Context, management.ResourceCatalogQuery) (management.ResourceCatalog, error)
	DomainResourceView(context.Context, management.DomainResourceViewQuery) (management.DomainResourceView, error)
	WebsiteResourceView(context.Context, management.WebsiteResourceViewQuery) (management.WebsiteResourceView, error)
}

// Handler owns the analytics operations. ActiveProxyRequests is read live from
// the proxy rather than from the store, because in-flight count is the one
// analytics value that cannot come from a materialized bucket.
type Handler struct {
	Store               Store
	BodyMax             int64
	ActiveProxyRequests func() int64
	Observe             management.ObserveRequest
}

func (h Handler) Routes(r chi.Router) {
	r.Post("/analytics/overview", h.Overview)
	r.Post("/analytics/domains", h.Domains)
	r.Post("/analytics/timeseries", h.Timeseries)
	r.Post("/analytics/contributions", h.Contributions)
	r.Post("/analytics/details", h.Details)
	r.Get("/analytics/catalog", h.Catalog)
	r.Get("/analytics/subject", h.Subject)
}

// Overview answers with only the requested sections, so the console's
// dashboard needs one request where it previously fired three.
func (h Handler) Overview(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	input, err := management.DecodeTypedJSONBody[management.AnalyticsOverviewQuery](r.Body, h.BodyMax)
	if err == nil {
		input.Sections, err = normalizeSections(input.Sections)
	}
	if err != nil {
		h.Observe.Observe("overview", err, started)
		management.Fail(w, http.StatusBadRequest, "invalid sections")
		return
	}
	out, err := h.Store.AnalyticsOverview(r.Context(), input)
	if err == nil && out.Overview != nil && h.ActiveProxyRequests != nil {
		out.Overview.CurrentInFlight = h.ActiveProxyRequests()
	}
	h.Observe.Observe("overview", err, started)
	management.RespondStore(w, out, err)
}

func (h Handler) Domains(w http.ResponseWriter, r *http.Request) {
	h.respondQuery(w, r, "domains", func(ctx context.Context, query management.AnalyticsQuery) (any, error) {
		return h.Store.AnalyticsDomains(ctx, query)
	})
}

func (h Handler) Timeseries(w http.ResponseWriter, r *http.Request) {
	h.respondQuery(w, r, "timeseries", func(ctx context.Context, query management.AnalyticsQuery) (any, error) {
		return h.Store.AnalyticsTimeseries(ctx, query)
	})
}

func (h Handler) Contributions(w http.ResponseWriter, r *http.Request) {
	h.respondQuery(w, r, "contributions", func(ctx context.Context, query management.AnalyticsQuery) (any, error) {
		return h.Store.AnalyticsContributions(ctx, query)
	})
}

func (h Handler) Details(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	input, err := management.DecodeTypedJSONBody[management.AnalyticsPagedQuery](r.Body, h.BodyMax)
	if err == nil {
		input.PageRequest, err = management.NormalizePageRequest(input.PageRequest)
	}
	if err != nil {
		h.Observe.Observe("details", err, started)
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := h.Store.AnalyticsDetails(r.Context(), input)
	h.Observe.Observe("details", err, started)
	management.RespondStore(w, out, err)
}

func (h Handler) Catalog(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	page, err := management.ParsePageQuery(r)
	if err != nil {
		h.Observe.Observe("catalog", err, started)
		management.Fail(w, http.StatusBadRequest, "invalid query")
		return
	}
	query := management.ResourceCatalogQuery{
		Host:               management.QueryValue(r, "host"),
		Status:             management.QueryValue(r, "status"),
		WebsiteAssociation: management.QueryValue(r, "websiteAssociation"),
		PageRequest:        page,
	}
	out, err := h.Store.ResourceCatalog(r.Context(), query)
	h.Observe.Observe("catalog", err, started)
	management.RespondStore(w, out, err)
}

// Subject serves one Host or one website through the same response shape. The
// console used to call two endpoints whose payloads differed only in whether a
// website record was present.
func (h Handler) Subject(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	window, err := windowQuery(r)
	if err != nil {
		h.Observe.Observe("subject", err, started)
		management.Fail(w, http.StatusBadRequest, "invalid query")
		return
	}
	key := management.QueryValue(r, "key")
	switch subjectType := management.QueryValue(r, "subjectType"); subjectType {
	case "domain":
		if key == "" {
			h.Observe.Observe("subject", errors.New("key is required"), started)
			management.Fail(w, http.StatusBadRequest, "invalid query")
			return
		}
		view, err := h.Store.DomainResourceView(r.Context(), management.DomainResourceViewQuery{ResourceViewQuery: window, Host: key})
		h.Observe.Observe("subject", err, started)
		management.RespondMissingAsNull(w, management.AnalyticsSubjectView{
			SubjectType: "domain",
			Hosts:       []management.ResourceHost{view.Host},
			Overview:    view.Overview,
			Timeseries:  view.Timeseries,
		}, err)
	case "website":
		if !management.ValidIDString(key) {
			h.Observe.Observe("subject", errors.New("key is required"), started)
			management.Fail(w, http.StatusBadRequest, "invalid query")
			return
		}
		view, err := h.Store.WebsiteResourceView(r.Context(), management.WebsiteResourceViewQuery{ResourceViewQuery: window, ID: key})
		h.Observe.Observe("subject", err, started)
		website := view.Website
		management.RespondMissingAsNull(w, management.AnalyticsSubjectView{
			SubjectType: "website",
			Website:     &website,
			Hosts:       view.Hosts,
			Overview:    view.Overview,
			Timeseries:  view.Timeseries,
		}, err)
	default:
		h.Observe.Observe("subject", errors.New("invalid subjectType"), started)
		management.Fail(w, http.StatusBadRequest, "invalid query")
	}
}

func (h Handler) respondQuery(w http.ResponseWriter, r *http.Request, operation string, load func(context.Context, management.AnalyticsQuery) (any, error)) {
	started := time.Now()
	input, err := management.DecodeTypedJSONBody[management.AnalyticsQuery](r.Body, h.BodyMax)
	if err != nil {
		h.Observe.Observe(operation, err, started)
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := load(r.Context(), input)
	h.Observe.Observe(operation, err, started)
	management.RespondStore(w, out, err)
}

// normalizeSections de-duplicates the requested sections and rejects anything
// outside the contract's enumeration.
func normalizeSections(requested []string) ([]string, error) {
	seen := make(map[string]bool, len(requested))
	sections := make([]string, 0, len(requested))
	for _, value := range requested {
		switch value {
		case "overview", "domains", "timeseries":
			if !seen[value] {
				seen[value] = true
				sections = append(sections, value)
			}
		default:
			return nil, errors.New("invalid sections")
		}
	}
	if len(sections) == 0 {
		return nil, errors.New("invalid sections")
	}
	return sections, nil
}

func windowQuery(r *http.Request) (management.ResourceViewQuery, error) {
	from, err := management.OptionalInt64Query(r, "from")
	if err != nil {
		return management.ResourceViewQuery{}, err
	}
	to, err := management.OptionalInt64Query(r, "to")
	if err != nil {
		return management.ResourceViewQuery{}, err
	}
	return management.ResourceViewQuery{From: from, To: to}, nil
}
