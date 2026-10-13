// Package analytics owns the background half of the analytics read models: it
// turns the proxy's raw contributions into the minutely, hourly and daily
// reports that the /api/analytics operations read back. The queries themselves
// live in internal/repository; what lives here is the schedule.
package analytics

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"yaginx-gateway/internal/observability"
	"yaginx-gateway/internal/repository"
	"yaginx-gateway/internal/telemetry"
)

// Schedule decides which report cycles are due. The delay of each cycle holds
// materialization back past its boundary so late-arriving events of the period
// just closed are included.
type Schedule struct {
	MinutelyDelay, HourlyDelay, DailyDelay time.Duration
	lastMinute, lastHour, lastDay          int64
}

// Due reports which cycles a single tick has to materialize.
type Due struct {
	Minutely bool
	Hourly   bool
	Daily    bool
}

func (d Due) Any() bool {
	return d.Minutely || d.Hourly || d.Daily
}

// Settings bounds how far back each reconciliation pass rewrites, and how long
// a single materialization statement may run.
type Settings struct {
	MinutelyLookback       time.Duration
	ReconciliationLookback time.Duration
	StatementTimeout       time.Duration
}

func (s *Schedule) Due(now time.Time) Due {
	now = now.UTC()
	return Due{
		Minutely: s.duePeriod(now, time.Minute, s.MinutelyDelay, &s.lastMinute),
		Hourly:   s.duePeriod(now, time.Hour, s.HourlyDelay, &s.lastHour),
		Daily:    s.duePeriod(now, 24*time.Hour, s.DailyDelay, &s.lastDay),
	}
}

func (s *Schedule) MarkCompleted(now time.Time, completed Due) {
	now = now.UTC()
	if completed.Minutely {
		s.lastMinute = now.Truncate(time.Minute).Unix()
	}
	if completed.Hourly {
		s.lastHour = now.Truncate(time.Hour).Unix()
	}
	if completed.Daily {
		s.lastDay = now.Truncate(24 * time.Hour).Unix()
	}
}

func (s *Schedule) duePeriod(now time.Time, period, delay time.Duration, last *int64) bool {
	if delay < 0 {
		delay = 0
	}
	boundary := now.Truncate(period)
	if now.Before(boundary.Add(delay)) {
		return false
	}
	key := boundary.Unix()
	if *last == key {
		return false
	}
	*last = key
	return true
}

// Materializer runs the report cycles. A hint on DirtyPeriods materializes
// that one period immediately; otherwise the Schedule governs.
type Materializer struct {
	Store        *repository.Postgres
	Logger       *slog.Logger
	Metrics      *observability.Metrics
	Interval     time.Duration
	Schedule     Schedule
	Settings     Settings
	UAParsing    bool
	DirtyPeriods <-chan telemetry.PeriodHint
}

// Catchup materializes every cycle once before the periodic loop starts, so a
// node that was down does not wait for the next boundary to close the gap. The
// schedule is marked for whatever succeeded, which is what keeps Run from
// immediately repeating the same work.
func (m *Materializer) Catchup(ctx context.Context) {
	now := time.Now()
	minuteBegin, minuteEnd := MinutelyReconciliationWindow(now, m.Settings.MinutelyLookback)
	reportBegin, reportEnd := ReconciliationWindow(now, m.Settings.ReconciliationLookback)
	completed := Due{
		Minutely: m.materialize(ctx, minuteBegin, minuteEnd, repository.TrafficMaterializationOptions{Minutely: true, StatementTimeout: m.Settings.StatementTimeout}),
	}
	completed.Hourly = m.materialize(ctx, reportBegin, reportEnd, repository.TrafficMaterializationOptions{Hourly: true, StatementTimeout: m.Settings.StatementTimeout})
	if completed.Hourly {
		m.updateStatisticsHealth(ctx, reportBegin, reportEnd)
	}
	completed.Daily = m.materialize(ctx, reportBegin, reportEnd, repository.TrafficMaterializationOptions{Daily: true, StatementTimeout: m.Settings.StatementTimeout})
	m.Schedule.MarkCompleted(now, completed)
}

func (m *Materializer) Run(ctx context.Context) {
	interval := m.Interval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case hint := <-m.DirtyPeriods:
			begin := time.Unix(hint.Period, 0).UTC()
			end := begin.Add(time.Minute)
			options := repository.TrafficMaterializationOptions{Minutely: hint.Cycle == 5, Hourly: hint.Cycle == 10, StatementTimeout: m.Settings.StatementTimeout}
			if hint.Cycle == 10 {
				end = begin.Add(time.Hour)
			}
			m.materialize(ctx, begin, end, options)
			continue
		default:
		}
		now := time.Now()
		due := m.Schedule.Due(now)
		if due.Any() {
			minuteBegin, minuteEnd := MinutelyReconciliationWindow(now, m.Settings.MinutelyLookback)
			reportBegin, reportEnd := ReconciliationWindow(now, m.Settings.ReconciliationLookback)
			if due.Minutely {
				m.materialize(ctx, minuteBegin, minuteEnd, repository.TrafficMaterializationOptions{Minutely: true, StatementTimeout: m.Settings.StatementTimeout})
			}
			if due.Hourly && m.materialize(ctx, reportBegin, reportEnd, repository.TrafficMaterializationOptions{Hourly: true, StatementTimeout: m.Settings.StatementTimeout}) {
				m.updateStatisticsHealth(ctx, reportBegin, reportEnd)
			}
			if due.Daily {
				m.materialize(ctx, reportBegin, reportEnd, repository.TrafficMaterializationOptions{Daily: true, StatementTimeout: m.Settings.StatementTimeout})
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// materialize answers whether this node did the work: another node holding the
// materializer lock is the normal case in a cluster, not a failure.
func (m *Materializer) materialize(ctx context.Context, begin, end time.Time, options repository.TrafficMaterializationOptions) bool {
	err := m.Store.MaterializeTrafficReports(ctx, begin, end, options)
	if errors.Is(err, repository.ErrTrafficMaterializerBusy) {
		m.Logger.Debug("traffic materializer owned by another node")
		return false
	}
	if err != nil {
		if ctx.Err() == nil {
			m.Logger.Error("materialize traffic contributions", "error", err)
			m.Metrics.ObserveFlush("report", "failure")
		}
		return false
	}
	m.Metrics.ObserveFlush("report", "success")
	return true
}

func (m *Materializer) updateStatisticsHealth(ctx context.Context, begin, end time.Time) {
	health, err := m.Store.TrafficStatisticsHealth(ctx, begin, end, m.UAParsing)
	if err != nil {
		m.Logger.Error("check traffic statistics integrity", "error", err)
		return
	}
	for check, value := range health {
		m.Metrics.SetTrafficIntegrity(check, value)
		if value > 0 {
			m.Logger.Warn("traffic statistics integrity alert", "check", check, "value", value, "from", begin.UTC(), "to", end.UTC())
		}
	}
}

func ReconciliationWindow(now time.Time, lookback time.Duration) (time.Time, time.Time) {
	if lookback <= 0 {
		lookback = 48 * time.Hour
	}
	now = now.UTC()
	return now.Add(-lookback).Truncate(24 * time.Hour), now.Truncate(24 * time.Hour).Add(24 * time.Hour)
}

func MinutelyReconciliationWindow(now time.Time, lookback time.Duration) (time.Time, time.Time) {
	if lookback <= 0 {
		lookback = 10 * time.Minute
	}
	now = now.UTC()
	return now.Add(-lookback).Truncate(time.Minute), now.Truncate(time.Minute).Add(time.Minute)
}

// EffectiveDailyRetention preserves the former single report-retention setting
// as a lower bound while the new minute/hour/day settings govern their own
// cycles. Existing installations therefore never lose daily history by
// upgrading before they explicitly migrate their configuration.
func EffectiveDailyRetention(dailyDays, legacyReportDays int) int {
	if dailyDays < 1 {
		dailyDays = 730
	}
	if legacyReportDays > dailyDays {
		return legacyReportDays
	}
	return dailyDays
}
