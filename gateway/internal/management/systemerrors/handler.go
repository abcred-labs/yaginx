// Package systemerrors serves the recorded server-side error journal.
package systemerrors

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/management"
)

// Store is the system-error slice of the management repository. A deployment
// whose repository does not record system errors leaves it nil.
type Store interface {
	SearchSystemErrors(context.Context, management.SystemErrorSearchQuery) (management.SystemErrorPage, error)
}

type Handler struct {
	Store   Store
	BodyMax int64
}

func (h Handler) Routes(r chi.Router) {
	r.Post("/system_errors/search", h.Search)
}

func (h Handler) Search(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		management.Fail(w, http.StatusNotImplemented, "system error search unavailable")
		return
	}
	input, err := management.DecodeTypedJSONBody[management.SystemErrorSearchQuery](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	input.PageRequest, err = management.NormalizePageRequest(input.PageRequest)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := h.Store.SearchSystemErrors(r.Context(), input)
	management.RespondStore(w, out, err)
}
