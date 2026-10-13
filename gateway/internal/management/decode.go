package management

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// DefaultBodyMax bounds every management request body. Domain handlers receive
// it through their options rather than reading it from a global.
const DefaultBodyMax int64 = 1 << 20

// DecodeTypedJSONBody accepts forward-compatible JSON objects while still
// rejecting malformed input and trailing JSON values at every request
// boundary. Unknown fields are intentionally ignored by encoding/json.
func DecodeTypedJSONBody[T any](body io.ReadCloser, maximum int64) (T, error) {
	var value T
	if maximum <= 0 {
		maximum = DefaultBodyMax
	}
	limited := http.MaxBytesReader(nil, body, maximum)
	defer limited.Close()
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return value, errors.New("request body contains multiple JSON values")
		}
		return value, err
	}
	return value, nil
}

// QueryValues preserves the legacy map binding behavior for the operations that
// still bind their query string dynamically.
func QueryValues(r *http.Request) map[string]any {
	values := make(map[string]any, len(r.URL.Query()))
	for key, raw := range r.URL.Query() {
		if len(raw) > 0 {
			values[key] = raw[len(raw)-1]
		}
	}
	return values
}

// QueryValue preserves the legacy map binding behavior: repeated parameters
// use the final value, and textual values are trimmed before repository input.
func QueryValue(r *http.Request, name string) string {
	values := r.URL.Query()[name]
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[len(values)-1])
}

// RequiredIDQuery rejects empty, non-decimal, and out-of-range IDs before a
// repository can interpret an ID endpoint as a legacy name lookup.
func RequiredIDQuery(r *http.Request, name string) (string, error) {
	values := r.URL.Query()[name]
	if len(values) == 0 {
		return "", errors.New("id is required")
	}
	raw := values[len(values)-1]
	if !ValidIDString(raw) {
		return "", errors.New("id is required")
	}
	return raw, nil
}

// ValidIDString reports whether the value is a decimal int64, which is how the
// contract carries IDs across JSON to protect JavaScript precision.
func ValidIDString(value string) bool {
	if value == "" {
		return false
	}
	start := 0
	if value[0] == '-' {
		if len(value) == 1 {
			return false
		}
		start = 1
	}
	for index := start; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	_, err := strconv.ParseInt(value, 10, 64)
	return err == nil
}

// OptionalInt64Query reads an absent parameter as zero and rejects a present
// but unparseable one.
func OptionalInt64Query(r *http.Request, name string) (int64, error) {
	raw := QueryValue(r, name)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s is invalid: %w", name, err)
	}
	return value, nil
}
