// Package server assembles the management API from its domain packages. It is
// the only place that knows the full operation surface; each domain package
// knows only its own.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/auth"
	"yaginx-gateway/internal/management"
	"yaginx-gateway/internal/management/account"
	"yaginx-gateway/internal/management/acmeadmin"
	"yaginx-gateway/internal/management/analytics"
	"yaginx-gateway/internal/management/credentials"
	"yaginx-gateway/internal/management/diagnostics"
	"yaginx-gateway/internal/management/history"
	"yaginx-gateway/internal/management/ipblacklist"
	"yaginx-gateway/internal/management/nodes"
	"yaginx-gateway/internal/management/settings"
	"yaginx-gateway/internal/management/systemerrors"
	"yaginx-gateway/internal/management/webdomain"
	"yaginx-gateway/internal/management/website"
)

// Store is the whole management repository, expressed as the union of the
// per-domain interfaces. A repository satisfies it by satisfying each slice,
// and a test can satisfy one slice at a time.
type Store interface {
	account.Store
	website.Store
	webdomain.Store
	settings.Store
	credentials.Store
	nodes.Store
	ipblacklist.Store
	history.Store
	diagnostics.Store
	analytics.Store
}

type Options struct {
	Store Store
	// SystemErrors is optional. A deployment whose repository does not record
	// system errors answers the search operation with 501.
	SystemErrors systemerrors.Store
	Keys         auth.Keys
	KeysSource   func() auth.Keys
	BodyMax      int64
	// Refresh-session idle/absolute durations. See account.Handler for the
	// fallback defaults used when these are left zero (e.g. in tests).
	SessionIdleTimeout       time.Duration
	SessionAbsoluteTimeout   time.Duration
	SessionRememberIdle      time.Duration
	SessionRememberAbsolute  time.Duration
	ActiveProxyRequests      func() int64
	RuntimeSnapshot          func() management.GatewayRuntimeSnapshot
	ObserveManagementRequest management.ObserveRequest
	Logger                   *slog.Logger
	RecordSystemError        func(context.Context, string, string, string, string, error)
	// ACME is optional. A deployment that does not wire one up answers the
	// console's certificate-request/status operations with 501.
	ACME webdomain.ACME
}

// New builds the management API router.
func New(options Options) http.Handler {
	if options.BodyMax <= 0 {
		options.BodyMax = management.DefaultBodyMax
	}
	keys := func() auth.Keys {
		if options.KeysSource != nil {
			return options.KeysSource()
		}
		return options.Keys
	}

	accounts := account.Handler{
		Store: options.Store, Keys: keys, BodyMax: options.BodyMax,
		IdleTimeout: options.SessionIdleTimeout, AbsoluteTimeout: options.SessionAbsoluteTimeout,
		RememberIdle: options.SessionRememberIdle, RememberAbsolute: options.SessionRememberAbsolute,
	}
	websites := &website.Handler{Store: options.Store, BodyMax: options.BodyMax}
	domains := webdomain.Handler{Store: options.Store, ACME: options.ACME, BodyMax: options.BodyMax}
	systemSettings := settings.Handler{Store: options.Store, BodyMax: options.BodyMax}
	apiCredentials := credentials.Handler{Store: options.Store, BodyMax: options.BodyMax}
	fleet := nodes.Handler{Store: options.Store, BodyMax: options.BodyMax}
	blacklist := ipblacklist.Handler{Store: options.Store, BodyMax: options.BodyMax}
	requests := history.Handler{Store: options.Store, BodyMax: options.BodyMax}
	errorJournal := systemerrors.Handler{Store: options.SystemErrors, BodyMax: options.BodyMax}
	infra := diagnostics.Handler{Store: options.Store, Observe: options.ObserveManagementRequest, Runtime: options.RuntimeSnapshot}
	traffic := analytics.Handler{
		Store:               options.Store,
		BodyMax:             options.BodyMax,
		ActiveProxyRequests: options.ActiveProxyRequests,
		Observe:             options.ObserveManagementRequest,
	}

	r := chi.NewRouter()
	r.Use(management.RequestJournal(options.Logger, options.RecordSystemError))
	r.Route("/api", func(r chi.Router) {
		accounts.Routes(r)
		// A gateway node calls these two before it holds any console
		// credential, so they sit outside the bearer-guarded group.
		fleet.PublicRoutes(r)
		r.Group(func(r chi.Router) {
			r.Use(management.RequireBearerSource(keys))
			accounts.AuthenticatedRoutes(r)
			websites.Routes(r)
			domains.Routes(r)
			systemSettings.Routes(r)
			if acmeStore, ok := options.Store.(acmeadmin.Store); ok {
				acmeHandler := acmeadmin.Handler{Store: acmeStore, BodyMax: options.BodyMax}
				if logStore, logOK := options.Store.(interface {
					SearchSystemErrors(context.Context, management.SystemErrorSearchQuery) (management.SystemErrorPage, error)
				}); logOK {
					acmeHandler.Logs = logStore
				}
				acmeHandler.Routes(r)
			}
			apiCredentials.Routes(r)
			fleet.Routes(r)
			blacklist.Routes(r)
			requests.Routes(r)
			errorJournal.Routes(r)
			infra.Routes(r)
			traffic.Routes(r)
		})
	})
	return r
}
