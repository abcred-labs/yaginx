// Package nodes serves the gateway-node fleet operations, including the two
// unauthenticated endpoints a node itself calls to join and stay alive.
package nodes

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/management"
)

// Store is the gateway-node slice of the management repository.
type Store interface {
	RegisterNode(context.Context, management.NodeRegistrationRequest) (management.NodeSummary, error)
	HeartbeatNode(context.Context, management.NodeHeartbeatRequest) (management.NodeSummary, error)
	SearchGatewayNodes(context.Context, management.GatewayNodeSearchRequest) (management.GatewayNodePage, error)
	GetGatewayNode(context.Context, string) (*management.GatewayNode, error)
	DisableGatewayNode(context.Context, string, string) (management.GatewayNode, error)
	DeleteGatewayNode(context.Context, string) error
	ReloadGatewayNode(context.Context, string) (management.GatewayNodeReloadResult, error)
	RestartGatewayNode(context.Context, string) (management.GatewayNodeRestartResult, error)
}

type Handler struct {
	Store   Store
	BodyMax int64
}

// Routes registers the console-facing fleet operations.
func (h Handler) Routes(r chi.Router) {
	r.Post("/gateway_node/search", h.Search)
	r.Get("/gateway_node/get", h.Get)
	r.Post("/gateway_node/disable", h.Disable)
	r.Post("/gateway_node/delete", h.Delete)
	r.Post("/gateway_node/reload", h.Reload)
	r.Post("/gateway_node/restart", h.Restart)
}

// PublicRoutes registers the two operations a gateway node calls before it
// holds any console credential. They are authenticated by the node's own
// fingerprint inside the store, not by a bearer token.
func (h Handler) PublicRoutes(r chi.Router) {
	r.Post("/node/register", h.Register)
	r.Post("/node/heartbeat", h.Heartbeat)
}

func (h Handler) Register(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.NodeRegistrationRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusInternalServerError, management.RequestError(err))
		return
	}
	out, err := h.Store.RegisterNode(r.Context(), input)
	management.RespondStore(w, out, err)
}

func (h Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.NodeHeartbeatRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusInternalServerError, management.RequestError(err))
		return
	}
	out, err := h.Store.HeartbeatNode(r.Context(), input)
	management.RespondStore(w, out, err)
}

func (h Handler) Search(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.GatewayNodeSearchRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	input.PageRequest, err = management.NormalizePageRequest(input.PageRequest)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := h.Store.SearchGatewayNodes(r.Context(), input)
	management.RespondStore(w, out, err)
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.GetGatewayNode(r.Context(), r.URL.Query().Get("id"))
	management.RespondMissingAsNull(w, out, err)
}

func (h Handler) Disable(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.GatewayNodeDisableRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusInternalServerError, management.RequestError(err))
		return
	}
	out, err := h.Store.DisableGatewayNode(r.Context(), input.NodeID, input.Reason)
	management.RespondStore(w, out, err)
}

func (h Handler) Delete(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.GatewayNodeDeleteRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	err = h.Store.DeleteGatewayNode(r.Context(), input.NodeID)
	management.RespondStore(w, true, err)
}

func (h Handler) Reload(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.GatewayNodeReloadRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusInternalServerError, management.RequestError(err))
		return
	}
	out, err := h.Store.ReloadGatewayNode(r.Context(), input.NodeID)
	management.RespondStore(w, out, err)
}

func (h Handler) Restart(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.GatewayNodeReloadRequest](r.Body, h.BodyMax)
	if err != nil {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	out, err := h.Store.RestartGatewayNode(r.Context(), input.NodeID)
	management.RespondStore(w, out, err)
}
