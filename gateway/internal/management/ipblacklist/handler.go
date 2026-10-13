// Package ipblacklist serves the IP blacklist rule operations.
package ipblacklist

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/management"
)

// Store is the IP-blacklist slice of the management repository.
type Store interface {
	SearchIPBlacklist(context.Context, management.IPBlacklistSearchRequest) (management.Page[management.IPBlacklistRule], error)
	GetIPBlacklist(context.Context, string) (management.IPBlacklistRule, error)
	UpsertIPBlacklist(context.Context, management.IPBlacklistUpsertRequest) (management.IPBlacklistRule, error)
	RemoveIPBlacklist(context.Context, string) (management.IPBlacklistRule, error)
	LookupIP(context.Context, string) (management.IPProfile, error)
}

type Handler struct {
	Store   Store
	BodyMax int64
}

func (h Handler) Routes(r chi.Router) {
	r.Post("/ip_blacklist/search", h.Search)
	r.Get("/ip_blacklist/get", h.Get)
	r.Post("/ip_blacklist/upsert", h.Upsert)
	r.Post("/ip_blacklist/remove", h.Remove)
	r.Get("/ip_blacklist/lookup", h.Lookup)
}

func (h Handler) Search(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.IPBlacklistSearchRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	input.PageRequest, err = management.NormalizePageRequest(input.PageRequest)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := h.Store.SearchIPBlacklist(r.Context(), input)
	management.RespondStore(w, out, err)
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := management.RequiredIDQuery(r, "id")
	if err != nil {
		management.Fail(w, http.StatusBadRequest, "invalid query")
		return
	}
	out, err := h.Store.GetIPBlacklist(r.Context(), id)
	management.RespondMissingAsNull(w, out, err)
}

// Upsert answers a malformed body with 500, the legacy status the console's
// rule editor distinguishes from a rejected rule.
func (h Handler) Upsert(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.IPBlacklistUpsertRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusInternalServerError, management.RequestError(err))
		return
	}
	out, err := h.Store.UpsertIPBlacklist(r.Context(), input)
	management.RespondStore(w, out, err)
}

func (h Handler) Remove(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.IDRequest](r.Body, h.BodyMax)
	if err != nil || !management.ValidIDString(input.ID) {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := h.Store.RemoveIPBlacklist(r.Context(), input.ID)
	management.RespondStore(w, out, err)
}

func (h Handler) Lookup(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.LookupIP(r.Context(), r.URL.Query().Get("ip"))
	management.RespondStore(w, out, err)
}
