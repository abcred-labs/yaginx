// Package credentials serves the API credential operations.
package credentials

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/management"
)

// Store is the API-credential slice of the management repository.
type Store interface {
	ListAPICredentials(context.Context) ([]management.APICredential, error)
	CreateAPICredential(context.Context, management.APICredentialCreateRequest) (management.APICredentialSecret, error)
	ResetAPICredential(context.Context, string) (management.APICredentialSecret, error)
	RevokeAPICredential(context.Context, string) (management.APICredential, error)
}

type Handler struct {
	Store   Store
	BodyMax int64
}

func (h Handler) Routes(r chi.Router) {
	r.Get("/system_credentials", h.List)
	r.Post("/system_credentials", h.Create)
	r.Post("/system_credentials/{id}/reset", h.Reset)
	r.Post("/system_credentials/{id}/revoke", h.Revoke)
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.ListAPICredentials(r.Context())
	management.RespondStore(w, out, err)
}

func (h Handler) Create(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.APICredentialCreateRequest](r.Body, h.BodyMax)
	if err != nil || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Purpose) == "" {
		management.Fail(w, http.StatusBadRequest, management.RequestError(err))
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Purpose = strings.TrimSpace(input.Purpose)
	out, err := h.Store.CreateAPICredential(r.Context(), input)
	management.RespondStore(w, out, err)
}

func (h Handler) Reset(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.ResetAPICredential(r.Context(), chi.URLParam(r, "id"))
	management.RespondStore(w, out, err)
}

func (h Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.RevokeAPICredential(r.Context(), chi.URLParam(r, "id"))
	management.RespondStore(w, out, err)
}
