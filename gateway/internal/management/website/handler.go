// Package website serves the website configuration operations.
package website

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/management"
)

// RuleRefreshDebounce bounds how often a rule refresh is acknowledged as new
// work. The console polls it from several panels at once.
const RuleRefreshDebounce = 30 * time.Second

// Store is the website slice of the management repository.
type Store interface {
	UpsertWebsite(context.Context, management.WebsiteUpsertRequest) (management.Website, error)
	UpsertWebsiteIPAccess(context.Context, management.WebsiteIPAccessUpsertRequest) (management.WebsiteIPAccessRule, error)
	RemoveWebsiteIPAccess(context.Context, management.WebsiteIPAccessRemoveRequest) (management.WebsiteIPAccessRule, error)
	ListWebsites(context.Context) ([]management.Website, error)
	SearchWebsites(context.Context, management.WebsiteSearchRequest) (management.Page[management.Website], error)
	GetWebsite(context.Context, string) (management.Website, error)
	DeleteWebsite(context.Context, string) error
	DeleteWebsites(context.Context, []int64) (management.WebsiteBatchDeleteResult, error)
}

type Handler struct {
	Store   Store
	BodyMax int64

	refreshMu       sync.Mutex
	lastRuleRefresh time.Time
}

func (h *Handler) Routes(r chi.Router) {
	r.Post("/website/upsert", h.Upsert)
	r.Post("/website/ip_access/upsert", h.UpsertIPAccess)
	r.Post("/website/ip_access/remove", h.RemoveIPAccess)
	r.Get("/website/search", h.Search)
	r.Post("/website/search", h.Search)
	r.Get("/website/get", h.Get)
	r.Get("/website/get_by_name", h.GetByName)
	r.Delete("/website/delete", h.Delete)
	r.Post("/website/delete_batch", h.DeleteBatch)
	r.Post("/website/rule/refresh", h.RefreshRules)
}

func (h *Handler) UpsertIPAccess(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.WebsiteIPAccessUpsertRequest](r.Body, h.BodyMax)
	if err != nil || input.WebsiteID <= 0 {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	if err := validateIPAccessCIDR(input.CIDR); err != nil {
		management.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := h.Store.UpsertWebsiteIPAccess(r.Context(), input)
	management.RespondStore(w, out, err)
}

func (h *Handler) RemoveIPAccess(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.WebsiteIPAccessRemoveRequest](r.Body, h.BodyMax)
	if err != nil || input.WebsiteID <= 0 {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	if err := validateIPAccessCIDR(input.CIDR); err != nil {
		management.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := h.Store.RemoveWebsiteIPAccess(r.Context(), input)
	management.RespondStore(w, out, err)
}

func validateIPAccessCIDR(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("CIDR must be a valid IPv4 or IPv6 address/network")
	}
	if address, err := netip.ParseAddr(raw); err == nil {
		_ = address
		return nil
	}
	if _, err := netip.ParsePrefix(raw); err != nil {
		return fmt.Errorf("CIDR must be a valid IPv4 or IPv6 address/network")
	}
	return nil
}

func (h *Handler) Upsert(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.WebsiteUpsertRequest](r.Body, h.BodyMax)
	if err == nil {
		err = input.RequestHistory.Normalize()
	}
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := h.Store.UpsertWebsite(r.Context(), input)
	management.RespondStore(w, out, err)
}

// Search answers the GET form with the full, unpaged list the console's
// selectors rely on, and the POST form with a page.
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		out, err := h.Store.ListWebsites(r.Context())
		management.RespondStore(w, out, err)
		return
	}
	input, err := management.DecodeTypedJSONBody[management.WebsiteSearchRequest](r.Body, h.BodyMax)
	if err != nil && !errors.Is(err, io.EOF) {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	input.PageRequest, err = management.NormalizePageRequest(input.PageRequest)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := h.Store.SearchWebsites(r.Context(), input)
	management.RespondStore(w, out, err)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := management.RequiredIDQuery(r, "id")
	if err != nil {
		management.Fail(w, http.StatusBadRequest, "invalid query")
		return
	}
	out, err := h.Store.GetWebsite(r.Context(), id)
	management.RespondMissingAsNull(w, out, err)
}

func (h *Handler) GetByName(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.GetWebsite(r.Context(), r.URL.Query().Get("name"))
	management.RespondMissingAsNull(w, out, err)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := management.RequiredIDQuery(r, "id")
	if err != nil {
		management.Fail(w, http.StatusBadRequest, "invalid query")
		return
	}
	err = h.Store.DeleteWebsite(r.Context(), id)
	management.RespondStore(w, management.EmptyResponse{}, err)
}

func (h *Handler) DeleteBatch(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.WebsiteBatchDeleteRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	ids := make([]int64, 0, len(input.IDs))
	seen := make(map[management.ID]struct{}, len(input.IDs))
	for _, id := range input.IDs {
		if id <= 0 {
			err = errors.New("website id must be positive")
			break
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, int64(id))
	}
	// An empty or rejected batch answers 500 because the legacy console treats a
	// 400 here as a retryable validation hint and loops.
	if err != nil || len(ids) == 0 {
		management.Fail(w, http.StatusInternalServerError, management.RequestError(err))
		return
	}
	out, err := h.Store.DeleteWebsites(r.Context(), ids)
	management.RespondStore(w, out, err)
}

func (h *Handler) RefreshRules(w http.ResponseWriter, r *http.Request) {
	h.refreshMu.Lock()
	defer h.refreshMu.Unlock()
	now := time.Now().UTC()
	if !h.lastRuleRefresh.IsZero() && now.Sub(h.lastRuleRefresh) < RuleRefreshDebounce {
		management.OK(w, h.lastRuleRefresh.Format(time.RFC3339Nano))
		return
	}
	h.lastRuleRefresh = now
	management.OK(w, "OK")
}
