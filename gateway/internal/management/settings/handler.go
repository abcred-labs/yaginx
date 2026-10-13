// Package settings serves the SMTP and shared-runtime system settings.
package settings

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/management"
)

// Store is the system-setting slice of the management repository.
type Store interface {
	GetSMTP(context.Context) (*management.SMTPSettings, error)
	SetSMTP(context.Context, management.SMTPSettings) error
	TestSMTP(context.Context, string) error
	GetRuntimeSetting(context.Context) (*management.RuntimeSetting, error)
	SetRuntimeSetting(context.Context, management.RuntimeSettingUpsertRequest) (*management.RuntimeSetting, error)
	RuntimeSettingStatus(context.Context) (management.RuntimeSettingStatus, error)
}

type Handler struct {
	Store   Store
	BodyMax int64
}

func (h Handler) Routes(r chi.Router) {
	r.Get("/system_setting/smtp", h.GetSMTP)
	r.Post("/system_setting/smtp", h.UpsertSMTP)
	r.Post("/system_setting/smtp/test_send", h.TestSMTP)
	r.Get("/system_setting/runtime", h.GetRuntime)
	r.Post("/system_setting/runtime", h.UpsertRuntime)
	r.Get("/system_setting/runtime/status", h.RuntimeStatus)
	r.Get("/system_setting/runtime/access-context", h.RuntimeAccessContext)
}

func (h Handler) GetSMTP(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.GetSMTP(r.Context())
	management.RespondStore(w, out, err)
}

func (h Handler) UpsertSMTP(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.SMTPSettings](r.Body, h.BodyMax)
	if err == nil {
		err = h.Store.SetSMTP(r.Context(), input)
	}
	management.RespondStore(w, true, err)
}

func (h Handler) TestSMTP(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.SMTPTestSendRequest](r.Body, h.BodyMax)
	if err != nil {
		// Legacy status: the console's test-send panel reads a 400 as "address
		// rejected" and a 500 as "request rejected".
		management.Fail(w, http.StatusInternalServerError, management.RequestError(err))
		return
	}
	management.RespondStore(w, true, h.Store.TestSMTP(r.Context(), input.Email))
}

func (h Handler) GetRuntime(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.GetRuntimeSetting(r.Context())
	management.RespondStore(w, out, err)
}

func (h Handler) UpsertRuntime(w http.ResponseWriter, r *http.Request) {
	input, err := management.DecodeTypedJSONBody[management.RuntimeSettingUpsertRequest](r.Body, h.BodyMax)
	if err == nil {
		err = config.ValidateSharedRuntime(input.Config)
	}
	if err == nil {
		input.Config, err = config.NormalizeSharedRuntime(input.Config)
	}
	// Management CIDRs are node-local configuration and are deliberately not
	// persisted through the shared runtime endpoint.
	var out *management.RuntimeSetting
	if err == nil {
		out, err = h.Store.SetRuntimeSetting(r.Context(), input)
	}
	management.RespondStore(w, out, err)
}

func (h Handler) RuntimeAccessContext(w http.ResponseWriter, r *http.Request) {
	setting, err := h.Store.GetRuntimeSetting(r.Context())
	if err != nil {
		var out *management.RuntimeAccessContext
		management.RespondStore(w, out, err)
		return
	}
	var cidrs []string
	if setting != nil {
		cidrs = runtimeCIDRs(setting.Config)
	}
	if len(cidrs) == 0 {
		cidrs = config.DefaultManagementCIDRs()
	}
	clientIP, err := requestClientIP(r)
	if err != nil {
		var out *management.RuntimeAccessContext
		management.RespondStore(w, out, err)
		return
	}
	allowed, err := config.ClientIPAllowed(clientIP, cidrs)
	if err != nil {
		var out *management.RuntimeAccessContext
		management.RespondStore(w, out, err)
		return
	}
	management.RespondStore(w, management.RuntimeAccessContext{ClientIP: clientIP, AllowedCIDRs: cidrs, ClientAllowed: allowed}, nil)
}

func requestClientIP(r *http.Request) (string, error) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil || net.ParseIP(host) == nil {
		return "", fmt.Errorf("invalid request client IP")
	}
	return host, nil
}

func runtimeCIDRs(document map[string]any) []string {
	management, ok := document["management"].(map[string]any)
	if !ok {
		return config.DefaultManagementCIDRs()
	}
	values, ok := management["allowedCIDRs"].([]any)
	if ok {
		cidrs := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				cidrs = append(cidrs, text)
			}
		}
		return cidrs
	}
	if values, ok := management["allowedCIDRs"].([]string); ok {
		return append([]string(nil), values...)
	}
	return config.DefaultManagementCIDRs()
}

func ensureCurrentClientAllowed(r *http.Request, document map[string]any) error {
	clientIP, err := requestClientIP(r)
	if err != nil {
		return err
	}
	allowed, err := config.ClientIPAllowed(clientIP, runtimeCIDRs(document))
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("current client IP %s is not included in management.allowedCIDRs", clientIP)
	}
	return nil
}

func (h Handler) RuntimeStatus(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.RuntimeSettingStatus(r.Context())
	management.RespondStore(w, out, err)
}
