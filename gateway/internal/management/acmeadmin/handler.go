package acmeadmin

import (
	"context"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strconv"
	"yaginx-gateway/internal/management"
)

type Store interface {
	SearchACMEOrders(context.Context, int, int, string, string) (management.ACMEOrderPage, error)
	SearchACMEOrderLogs(context.Context, management.ID, management.PageRequest) (management.ACMEOrderLogPage, error)
	CancelACMEOrder(context.Context, management.ID) error
	RetryACMEOrderForOperator(context.Context, management.ID) error
}
type Handler struct {
	Store   Store
	BodyMax int64
	Logs    interface {
		SearchSystemErrors(context.Context, management.SystemErrorSearchQuery) (management.SystemErrorPage, error)
	}
}

func (h Handler) Routes(r chi.Router) {
	r.Get("/acme/orders", h.Search)
	r.Get("/acme/orders/{id}/logs", h.OrderLogs)
	r.Post("/acme/logs", h.LogsSearch)
	r.Post("/acme/orders/cancel", h.Cancel)
	r.Post("/acme/orders/retry", h.Retry)
}

func (h Handler) OrderLogs(w http.ResponseWriter, r *http.Request) {
	id, err := management.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		management.Fail(w, http.StatusBadRequest, "invalid order id")
		return
	}
	page, err := management.ParsePageQuery(r)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := h.Store.SearchACMEOrderLogs(r.Context(), id, page)
	management.RespondStore(w, out, err)
}

func (h Handler) LogsSearch(w http.ResponseWriter, r *http.Request) {
	if h.Logs == nil {
		management.Fail(w, http.StatusNotImplemented, "ACME logs unavailable")
		return
	}
	in, err := management.DecodeTypedJSONBody[management.SystemErrorSearchQuery](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	in.Component = "acme"
	out, err := h.Logs.SearchSystemErrors(r.Context(), in)
	management.RespondStore(w, out, err)
}
func (h Handler) Search(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("pageIndex"))
	size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	out, err := h.Store.SearchACMEOrders(r.Context(), page, size, r.URL.Query().Get("domain"), r.URL.Query().Get("status"))
	management.RespondStore(w, out, err)
}

type orderID struct {
	ID management.ID `json:"id"`
}

func (h Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	in, err := management.DecodeTypedJSONBody[orderID](r.Body, h.BodyMax)
	if err == nil {
		err = h.Store.CancelACMEOrder(r.Context(), in.ID)
	}
	management.RespondStore(w, true, err)
}
func (h Handler) Retry(w http.ResponseWriter, r *http.Request) {
	in, err := management.DecodeTypedJSONBody[orderID](r.Body, h.BodyMax)
	if err == nil {
		err = h.Store.RetryACMEOrderForOperator(r.Context(), in.ID)
	}
	management.RespondStore(w, true, err)
}
