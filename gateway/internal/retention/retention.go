// Package retention enforces how long Yaginx keeps what it writes. Each worker
// prunes once at startup and then daily, because an installation that has been
// down past its retention window must not wait a day to come back into policy.
package retention

import (
	"context"
	"log/slog"
	"time"

	"yaginx-gateway/internal/repository"
	"yaginx-gateway/internal/sysevent"
)

// Telemetry prunes the traffic, report and request-history tables.
type Telemetry struct {
	Store  *repository.Postgres
	Logger *slog.Logger
	Events sysevent.Recorder

	ContributionRetention time.Duration
	MonitorInfoRetention  time.Duration
	TrafficDays           int
	ReportDays            int
	MinuteReportDays      int
	HourlyReportDays      int
	HistoryHours          int
	BatchSize             int
}

func (t Telemetry) Run(ctx context.Context) {
	runDaily(ctx, func() {
		now := time.Now().UTC()
		if err := t.Store.PruneACMEKeyValues(ctx, now); err != nil && ctx.Err() == nil {
			t.Logger.Error("prune ACME key-value retention", "error", err)
			t.Events.Record(ctx, "certificate", "key_value_prune", "warning", "ACME_KEY_VALUE_RETENTION_FAILED", err)
		}
		if err := t.Store.PruneTelemetry(ctx, now, t.ContributionRetention, t.MonitorInfoRetention, t.TrafficDays, t.ReportDays, t.HistoryHours, t.BatchSize); err != nil && ctx.Err() == nil {
			t.Logger.Error("prune telemetry retention", "error", err)
			t.Events.Record(ctx, "telemetry", "prune", "warning", "TELEMETRY_RETENTION_FAILED", err)
		}
		if err := t.Store.PruneReportCycles(ctx, now, t.MinuteReportDays, t.HourlyReportDays, t.BatchSize); err != nil && ctx.Err() == nil {
			t.Logger.Error("prune detailed report retention", "error", err)
			t.Events.Record(ctx, "telemetry", "report_prune", "warning", "REPORT_RETENTION_FAILED", err)
		}
	})
}

// ConfigEvents prunes the configuration event log, keeping whatever an
// inactive node might still need to replay.
type ConfigEvents struct {
	Store         *repository.Postgres
	Logger        *slog.Logger
	Retention     time.Duration
	InactiveAfter time.Duration
}

func (c ConfigEvents) Run(ctx context.Context) {
	runDaily(ctx, func() {
		if err := c.Store.PruneConfigEvents(ctx, time.Now().UTC(), c.Retention, c.InactiveAfter); err != nil && ctx.Err() == nil {
			c.Logger.Error("prune configuration events", "error", err)
		}
	})
}

// AccountSessions prunes refresh sessions that can no longer authenticate
// anything: past either deadline, or revoked long enough ago that a
// near-simultaneous reuse attempt no longer needs a row to reject against.
type AccountSessions struct {
	Store  *repository.Postgres
	Logger *slog.Logger
}

func (a AccountSessions) Run(ctx context.Context) {
	runDaily(ctx, func() {
		if err := a.Store.PruneExpiredAccountSessions(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			a.Logger.Error("prune expired account sessions", "error", err)
		}
	})
}

func runDaily(ctx context.Context, prune func()) {
	prune()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prune()
		}
	}
}
