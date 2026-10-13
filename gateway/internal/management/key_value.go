package management

import (
	"context"
	"net/http"
	"time"
)

// ACMEKeyValueStore is the small repository surface needed by the internal
// ACME key-value compatibility challenge protocol.
type ACMEKeyValueStore interface {
	SetACMEKeyValue(context.Context, string, string, time.Time) error
	GetACMEKeyValue(context.Context, string) (string, bool, error)
}

const acmeKeyValueTTL = 10 * time.Minute

// KeyValueHandler serves the anonymous, management-port-only ACME compatibility
// endpoints. Authentication is intentionally absent; the outer management
// listener CIDR guard remains the access-control boundary.
func KeyValueHandler(store ACMEKeyValueStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			Fail(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		switch r.URL.Path {
		case "/api/key_value/set":
			key, value := r.URL.Query().Get("key"), r.URL.Query().Get("value")
			if !validACMEKey(key) || value == "" || len(value) > 64<<10 {
				Fail(w, http.StatusBadRequest, "invalid query")
				return
			}
			if err := store.SetACMEKeyValue(r.Context(), key, value, time.Now().UTC().Add(acmeKeyValueTTL)); err != nil {
				FailStoreError(w, err)
				return
			}
			OK[any](w, nil)
		case "/api/key_value/get":
			key := r.URL.Query().Get("key")
			if !validACMEKey(key) {
				Fail(w, http.StatusBadRequest, "invalid query")
				return
			}
			value, found, err := store.GetACMEKeyValue(r.Context(), key)
			if err != nil {
				FailStoreError(w, err)
				return
			}
			if !found {
				OK(w, (*string)(nil))
				return
			}
			OK(w, value)
		default:
			http.NotFound(w, r)
		}
	})
}

func validACMEKey(value string) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
