package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type keyValueFixture struct {
	values map[string]string
	expiry map[string]time.Time
}

func (f *keyValueFixture) SetACMEKeyValue(_ context.Context, key, value string, expiresAt time.Time) error {
	if f.values == nil {
		f.values = map[string]string{}
		f.expiry = map[string]time.Time{}
	}
	f.values[key], f.expiry[key] = value, expiresAt
	return nil
}

func (f *keyValueFixture) GetACMEKeyValue(_ context.Context, key string) (string, bool, error) {
	value, ok := f.values[key]
	if !ok || !f.expiry[key].After(time.Now()) {
		return "", false, nil
	}
	return value, true, nil
}

func decodeKeyValueResponse(t *testing.T, response *httptest.ResponseRecorder) (int, any) {
	t.Helper()
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var data any
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	return envelope.Code, data
}

func TestKeyValueHandlerSetGetOverwriteAndMissWithoutBearer(t *testing.T) {
	store := &keyValueFixture{}
	handler := KeyValueHandler(store)

	request := httptest.NewRequest(http.MethodGet, "/api/key_value/set?key=token_1&value=key-auth", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if code, data := decodeKeyValueResponse(t, response); code != http.StatusOK || data != nil {
		t.Fatalf("set response code=%d data=%v", code, data)
	}
	if got := store.values["token_1"]; got != "key-auth" {
		t.Fatalf("stored value=%q", got)
	}
	if remaining := time.Until(store.expiry["token_1"]); remaining < 9*time.Minute {
		t.Fatalf("TTL remaining=%s, want at least 9m", remaining)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/key_value/set?key=token_1&value=replaced", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	request = httptest.NewRequest(http.MethodGet, "/api/key_value/get?key=token_1", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if code, data := decodeKeyValueResponse(t, response); code != http.StatusOK || data != "replaced" {
		t.Fatalf("get response code=%d data=%v", code, data)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/key_value/get?key=missing", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if code, data := decodeKeyValueResponse(t, response); code != http.StatusOK || data != nil {
		t.Fatalf("missing response code=%d data=%v", code, data)
	}
}

func TestKeyValueHandlerTreatsExpiredValueAsMiss(t *testing.T) {
	store := &keyValueFixture{values: map[string]string{"expired": "value"}, expiry: map[string]time.Time{"expired": time.Now().Add(-time.Second)}}
	response := httptest.NewRecorder()
	KeyValueHandler(store).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/key_value/get?key=expired", nil))
	if code, data := decodeKeyValueResponse(t, response); code != http.StatusOK || data != nil {
		t.Fatalf("expired response code=%d data=%v", code, data)
	}
}
