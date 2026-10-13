package app

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagementCIDRGuardAllowsIPv4AndMappedIPv6(t *testing.T) {
	for _, remote := range []string{"192.0.2.10:1234", "[::ffff:192.0.2.10]:1234"} {
		handler, err := managementCIDRGuardWithLogger([]string{"0.0.0.0/0"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}), slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
		if err != nil {
			t.Fatalf("build guard: %v", err)
		}
		request := httptest.NewRequest(http.MethodGet, "http://example.test/health/live", nil)
		request.RemoteAddr = remote
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Errorf("remote %q: got status %d, want %d", remote, response.Code, http.StatusNoContent)
		}
	}
}

func TestManagementCIDRGuardAllowsIPv6(t *testing.T) {
	handler, err := managementCIDRGuardWithLogger([]string{"2001:db8::/32"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), nil)
	if err != nil {
		t.Fatalf("build guard: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://example.test/health/live", nil)
	request.RemoteAddr = "[2001:db8::10]:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestManagementCIDRGuardDeniesAndLogsDiagnostics(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler, err := managementCIDRGuardWithLogger([]string{"203.0.113.0/24"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), logger)
	if err != nil {
		t.Fatalf("build guard: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://example.test/health/live", nil)
	request.RemoteAddr = "[::ffff:192.0.2.10]:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusForbidden)
	}
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode diagnostic log: %v", err)
	}
	for _, key := range []string{"remoteAddr", "parsedHost", "parsedIP", "configuredCIDRs", "matchedCIDR", "reason"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("diagnostic log missing %q: %s", key, logs.String())
		}
	}
	if strings.Contains(logs.String(), "Cookie") || strings.Contains(logs.String(), "Authorization") {
		t.Error("diagnostic log contains sensitive request fields")
	}
}

func TestManagementCIDRGuardRejectsInvalidCIDR(t *testing.T) {
	if _, err := managementCIDRGuard([]string{"not-a-cidr"}, http.NotFoundHandler()); err == nil {
		t.Fatal("expected invalid CIDR error")
	}
}
