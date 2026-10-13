// Package sysevent journals Yaginx-level system errors: failures of the node
// itself rather than of a proxied request. Background components take the
// Recorder interface so they neither reach for the store nor decide for
// themselves whether a failure deserves a metric.
package sysevent

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
	"yaginx-gateway/internal/observability"
	"yaginx-gateway/internal/repository"
)

// Recorder is the narrow dependency a component needs in order to report that
// it failed. The bool answer says whether the failure reached durable storage,
// which lets a caller decide whether to retry reporting it.
type Recorder interface {
	Record(ctx context.Context, component, operation, severity, code string, err error) bool
}

// StoreRecorder writes to the system_error journal. A nil Metrics records the
// failure without counting it, which is how the periodic maintenance workers
// report their own trouble: the record is the signal, not a rate.
type StoreRecorder struct {
	Store   *repository.Postgres
	Logger  *slog.Logger
	Metrics *observability.Metrics
}

// WithoutMetrics returns the same journal with metric counting turned off.
func (r StoreRecorder) WithoutMetrics() StoreRecorder {
	r.Metrics = nil
	return r
}

func (r StoreRecorder) Record(ctx context.Context, component, operation, severity, code string, err error) bool {
	if r.Store == nil || err == nil {
		return false
	}
	if r.Metrics != nil {
		r.Metrics.ObserveSystemError(component, code, severity)
	}
	traceID := ""
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		traceID = span.TraceID().String()
	}
	eventID, recordErr := r.Store.RecordSystemError(ctx, repository.SystemError{Component: component, Operation: operation, Severity: severity, ErrorCode: code, Message: err.Error(), TraceID: traceID})
	if recordErr != nil {
		r.Logger.Error("record system error failed", "error", recordErr, "original_error_code", code)
		return false
	}
	r.Logger.Error("yaginx system error", "system_event_id", eventID, "component", component, "operation", operation, "error_code", code, "error", err)
	return true
}

// Discard drops every system error. Components under test take it so they do
// not need a database.
type Discard struct{}

func (Discard) Record(context.Context, string, string, string, string, error) bool { return false }
