// Package nodes keeps this gateway node's membership in the control plane
// current: it heartbeats, and it makes an explicit disable, drain or rolling
// restart authoritative on the data plane.
package nodes

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"yaginx-gateway/internal/management"
	"yaginx-gateway/internal/observability"
	"yaginx-gateway/internal/repository"
	"yaginx-gateway/internal/sysevent"
)

// Revisions is the state the heartbeat exchanges with the control plane. The
// data plane owns these counters, so the monitor reads and writes them rather
// than keeping copies.
type Revisions struct {
	Applied           *atomic.Int64
	RestartGeneration *atomic.Int64
	ObservedRestart   *atomic.Int64
	RestartRequested  *atomic.Bool
}

// Monitor records liveness and reacts to control-plane state changes. A
// transient database error is deliberately not treated as disable: an active
// node may continue serving its last known good configuration.
type Monitor struct {
	Store     *repository.Postgres
	Logger    *slog.Logger
	Metrics   *observability.Metrics
	Events    sysevent.Recorder
	NodeID    string
	Interval  time.Duration
	Timeout   time.Duration
	Revisions Revisions

	// Drain quiesces the business listeners. Stop ends the data plane.
	Drain func(context.Context) error
	Stop  context.CancelFunc
}

func (m Monitor) Run(ctx context.Context) {
	interval := m.Interval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	drainTimeout := m.Timeout
	if drainTimeout <= 0 {
		drainTimeout = 5 * time.Minute
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	lastSuccess := time.Now()
	for {
		out, err := m.Store.HeartbeatNode(ctx, management.NodeHeartbeatRequest{
			NodeID:                    m.NodeID,
			AppliedRevision:           m.Revisions.Applied.Load(),
			RestartGeneration:         m.Revisions.RestartGeneration.Load(),
			ObservedRestartGeneration: m.Revisions.ObservedRestart.Load(),
		})
		if err == nil {
			lastSuccess = time.Now()
			m.Metrics.SetNodeHeartbeatAge(0)
			m.Revisions.RestartGeneration.Store(out.RestartGeneration)
			m.Revisions.ObservedRestart.Store(out.ObservedRestartGeneration)
			if out.Status == "active" && strings.HasPrefix(out.StateReason, "restart:") {
				claimed, claimErr := m.Store.ClaimNodeRestart(ctx, m.NodeID)
				if claimErr != nil {
					m.Logger.Warn("claim rolling restart", "node", m.NodeID, "error", claimErr)
				} else if claimed {
					m.Logger.Info("rolling restart claimed", "node", m.NodeID)
					m.drain(drainTimeout)
					m.Revisions.RestartRequested.Store(true)
					m.Stop()
					return
				}
			}
			if status := out.Status; status != "active" {
				if status == "draining" {
					m.drain(drainTimeout)
					if strings.HasPrefix(out.StateReason, "restart:") {
						m.Revisions.RestartRequested.Store(true)
					} else if completeErr := m.Store.CompleteNodeDrain(context.Background(), m.NodeID); completeErr != nil {
						m.Logger.Error("complete node drain", "node", m.NodeID, "error", completeErr)
					}
				}
				m.Logger.Warn("node is no longer active; stopping data plane", "node", m.NodeID, "status", status)
				m.Stop()
				return
			}
		} else if ctx.Err() == nil {
			m.Metrics.SetNodeHeartbeatAge(time.Since(lastSuccess))
			m.Logger.Warn("node heartbeat failed; retaining active LKG until control plane recovers", "node", m.NodeID, "error", err)
			m.Events.Record(ctx, "database", "heartbeat", "warning", "NODE_HEARTBEAT_FAILED", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// drain runs on a fresh context: the lifecycle context may already be done by
// the time the control plane asks for a graceful stop.
func (m Monitor) drain(timeout time.Duration) {
	if m.Drain == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := m.Drain(ctx); err != nil {
		m.Logger.Error("drain business listeners", "node", m.NodeID, "error", err)
	}
}

// WaitForApproved blocks until the control plane approves this node. The
// management listener is already serving by the time this runs, which is what
// lets an operator approve a freshly registered node; the business listeners
// and ACME stay withheld from pending, draining and disabled nodes until it
// returns true.
func WaitForApproved(ctx context.Context, store *repository.Postgres, nodeID string, logger *slog.Logger) bool {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	last := ""
	for {
		status, err := store.NodeStatus(ctx, nodeID)
		if err == nil && status != last {
			logger.Info("node state observed", "node", nodeID, "status", status)
			last = status
		}
		if status == "active" {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}
