package settings

import (
	"net/http/httptest"
	"testing"
)

func TestEnsureCurrentClientAllowed(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/system_setting/runtime", nil)
	request.RemoteAddr = "203.0.113.24:44321"
	document := map[string]any{"management": map[string]any{"allowedCIDRs": []any{"203.0.113.0/24"}}}
	if err := ensureCurrentClientAllowed(request, document); err != nil {
		t.Fatalf("current client should be allowed: %v", err)
	}
	document["management"].(map[string]any)["allowedCIDRs"] = []any{"192.0.2.0/24"}
	if err := ensureCurrentClientAllowed(request, document); err == nil {
		t.Fatal("save that excludes current client was accepted")
	}
}

func TestEnsureCurrentClientAllowedSupportsIPv6(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/system_setting/runtime", nil)
	request.RemoteAddr = "[2001:db8::24]:44321"
	document := map[string]any{"management": map[string]any{"allowedCIDRs": []any{"2001:db8::/64"}}}
	if err := ensureCurrentClientAllowed(request, document); err != nil {
		t.Fatalf("current IPv6 client should be allowed: %v", err)
	}
}
