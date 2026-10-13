package config

import "testing"

func TestClientIPAllowedSupportsDualStackDefaults(t *testing.T) {
	for _, tc := range []struct {
		ip      string
		allowed bool
	}{
		{"203.0.113.10", true},
		{"2001:db8::10", true},
		{"203.0.113.10", false},
	} {
		cidrs := []string{"0.0.0.0/0", "::/0"}
		if !tc.allowed {
			cidrs = []string{"192.0.2.0/24"}
		}
		got, err := ClientIPAllowed(tc.ip, cidrs)
		if err != nil || got != tc.allowed {
			t.Fatalf("ClientIPAllowed(%q, %#v) = %v, %v; want %v", tc.ip, cidrs, got, err, tc.allowed)
		}
	}
}

func TestClientIPAllowedRejectsMalformedCIDR(t *testing.T) {
	if _, err := ClientIPAllowed("203.0.113.10", []string{"not-a-cidr"}); err == nil {
		t.Fatal("malformed CIDR was accepted")
	}
}
