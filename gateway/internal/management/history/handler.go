// Package history serves the per-request audit trail. It is deliberately
// separate from analytics: these are individual request records, not aggregates.
package history

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/management"
)

// Store is the request-history slice of the management repository.
type Store interface {
	SearchRequestHistory(context.Context, management.RequestHistorySearchQuery) (management.RequestHistoryPage, error)
	GetRequestHistory(context.Context, string) (management.RequestHistoryDetail, error)
}

type Handler struct {
	Store   Store
	BodyMax int64
}

func (h Handler) Routes(r chi.Router) {
	r.Post("/request_history/search", h.Search)
	r.Get("/request_history/get", h.Get)
}

func (h Handler) Search(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeBody[management.RequestHistorySearchQuery](r, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	input.PageRequest, err = management.NormalizePageRequest(input.PageRequest)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	input.StatusCodePattern, err = management.NormalizeStatusCodePattern(input.StatusCodePattern)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := h.Store.SearchRequestHistory(r.Context(), input)
	management.RespondStore(w, out, err)
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := management.RequiredIDQuery(r, "id")
	if err != nil {
		management.Fail(w, http.StatusBadRequest, "invalid query")
		return
	}
	out, err := h.Store.GetRequestHistory(r.Context(), id)
	management.RespondMissingAsNull(w, out, err)
}
