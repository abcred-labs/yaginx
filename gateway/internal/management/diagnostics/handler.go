// Package diagnostics serves infrastructure health readouts. It is separate
// from analytics because it reports on the database engine, not on traffic.
package diagnostics

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/management"
)

// Store is the diagnostics slice of the management repository.
type Store interface {
	DatabasePerformance(context.Context) (management.DatabasePerformance, error)
	GatewayNodeHealthSummary(context.Context, int64) (management.GatewayHealthSummary, error)
}

type Handler struct {
	Store   Store
	Observe management.ObserveRequest
	Runtime func() management.GatewayRuntimeSnapshot
}

func (h Handler) Routes(r chi.Router) {
	r.Get("/diagnostics/database", h.Database)
	r.Get("/diagnostics/overview", h.Overview)
}

func (h Handler) Overview(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	out := management.DiagnosticsOverview{AsOf: time.Now().Unix(), Runtime: management.GatewayRuntimeSnapshot{AsOf: time.Now().Unix(), Queues: map[string]management.GatewayRuntimeQueue{}}}
	if h.Runtime != nil {
		out.Runtime = h.Runtime()
		out.AsOf = out.Runtime.AsOf
	}
	cutoff := time.Now().Add(-45 * time.Second).Unix()
	health, err := h.Store.GatewayNodeHealthSummary(r.Context(), cutoff)
	if err != nil {
		h.Observe.Observe("overview", err, started)
		management.RespondStore(w, out, err)
		return
	}
	out.Health = health
	if out.Health.StaleNodes > 0 {
		out.Health.Level = "warning"
		out.Health.Reasons = append(out.Health.Reasons, "存在心跳过期节点")
	} else if out.Health.DrainingNodes > 0 {
		out.Health.Level = "warning"
		out.Health.Reasons = append(out.Health.Reasons, "存在排空中的节点")
	} else {
		out.Health.Level = "healthy"
	}
	h.Observe.Observe("overview", nil, started)
	management.RespondStore(w, out, nil)
}

func (h Handler) Database(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	out, err := h.Store.DatabasePerformance(r.Context())
	h.Observe.Observe("database", err, started)
	management.RespondStore(w, out, err)
}
