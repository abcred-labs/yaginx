package management

import (
	"context"
	"net/http"
)

// DatabaseGuard makes a live process fail closed when PostgreSQL disappears.
// Store methods keep their legacy business-error semantics; only an actual
// availability check produces the contractual 503 response.
func DatabaseGuard(next http.Handler, check func(context.Context) error) http.Handler {
	if next == nil || check == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := check(request.Context()); err != nil {
			WriteUnavailable(w)
			return
		}
		next.ServeHTTP(w, request)
	})
}
