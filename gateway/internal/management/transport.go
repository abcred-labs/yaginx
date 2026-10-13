package management

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

var ErrNotFound = errors.New("not found")

var ErrConflict = errors.New("conflict")

// RequestError keeps the legacy wording for a request that failed to decode.
func RequestError(err error) string {
	if err == nil {
		return "invalid request"
	}
	return err.Error()
}

// RespondStore is the shape every domain handler ends in: one envelope on
// success, the mapped store error otherwise.
func RespondStore[T any](w http.ResponseWriter, data T, err error) {
	if err == nil {
		OK(w, data)
		return
	}
	FailStoreError(w, err)
}

// RespondMissingAsNull answers a lookup miss with a null data field instead of
// a 404, which is what the console expects from every get-by-key operation.
func RespondMissingAsNull[T any](w http.ResponseWriter, data T, err error) {
	if errors.Is(err, ErrNotFound) {
		OK(w, (*EmptyResponse)(nil))
		return
	}
	RespondStore(w, data, err)
}

func FailStoreError(w http.ResponseWriter, err error) {
	// The response trace ID matches the structured journal entry so callers can
	// correlate the detailed error with the server-side request.
	tid := TransactionID()
	if reporter, ok := w.(*journalResponseWriter); ok {
		tid = reporter.logStoreError(tid, err)
	}
	status := http.StatusInternalServerError
	if errors.Is(err, ErrNotFound) {
		status = http.StatusNotFound
	} else if errors.Is(err, ErrConflict) {
		status = http.StatusConflict
	}
	FailWithTransactionID(w, status, tid, err.Error())
}

// RequestJournal carries the per-request logging and system-error reporting
// that the envelope writers reach for when a store call fails. It also sets the
// no-store policy that keeps the console from caching management responses.
func RequestJournal(logger *slog.Logger, recordSystemError func(context.Context, string, string, string, string, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			next.ServeHTTP(&journalResponseWriter{
				ResponseWriter:    w,
				logger:            logger,
				recordSystemError: recordSystemError,
				method:            r.Method,
				path:              r.URL.Path,
				requestContext:    r.Context(),
			}, r)
		})
	}
}

type journalResponseWriter struct {
	http.ResponseWriter
	logger            *slog.Logger
	recordSystemError func(context.Context, string, string, string, string, error)
	method, path      string
	requestContext    context.Context
}

func (w *journalResponseWriter) logStoreError(tid string, err error) string {
	if w.recordSystemError != nil && isManagementSystemError(err) {
		requestContext := w.requestContext
		if requestContext == nil {
			requestContext = context.Background()
		}
		w.recordSystemError(requestContext, "management", "store_request", "error", "MANAGEMENT_STORE_REQUEST_FAILED", err)
	}
	if w.logger == nil {
		return tid
	}
	attributes := []any{
		"traceId", tid,
		"method", w.method,
		"path", w.path,
		"error", err,
	}
	if operation, ok := strings.CutPrefix(w.path, "/api/analytics/"); ok && operation != "" {
		attributes = append(attributes, "operation", operation)
	}
	w.logger.Error("management store request failed", attributes...)
	return tid
}

func isManagementSystemError(err error) bool {
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, context.Canceled) {
		return false
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) || errors.Is(err, context.DeadlineExceeded)
}
