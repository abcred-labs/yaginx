package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestVerifyAcceptsPreviousKeyDuringRotation(t *testing.T) {
	now := time.Unix(1000, 0)
	old := Keys{Current: []byte("01234567890123456789012345678901"), Issuer: "i", Audience: "a", Session: time.Minute}
	token, err := old.Issue("user@example.test", now)
	if err != nil {
		t.Fatal(err)
	}
	rotated := Keys{Current: []byte("abcdefghijklmnopqrstuvwxyz123456"), Previous: old.Current, Issuer: "i", Audience: "a", Session: time.Minute}
	claims, err := rotated.Verify(token, now.Add(time.Second))
	if err != nil || claims.Subject != "user@example.test" {
		t.Fatalf("verify: %#v %v", claims, err)
	}
}

func TestIssueIdentityUsesLegacyClaimsAndClockWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	keys := Keys{Current: []byte("01234567890123456789012345678901"), Issuer: "Yaginx Proxy Server", Audience: "Yaingx Clients", Session: 10 * time.Minute}
	token, err := keys.IssueIdentity("42", "admin@example.invalid", "admin@example.invalid", "", now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["uid"] != "42" || wire["uname"] != "admin@example.invalid" || wire["email"] != "admin@example.invalid" || wire["iat"] != float64(now.Unix()) || wire["nbf"] != float64(now.Add(-30*time.Second).Unix()) || wire["exp"] != float64(now.Add(10*time.Minute+30*time.Second).Unix()) {
		t.Fatalf("legacy claim payload = %#v", wire)
	}
	if _, ok := wire["sub"]; ok {
		t.Fatalf("standard sub claim must not replace legacy uid/uname: %#v", wire)
	}
	claims, err := keys.Verify(token, now.Add(10*time.Minute+29*time.Second))
	if err != nil || claims.Subject != "admin@example.invalid" || claims.UID != "42" {
		t.Fatalf("verify = %#v, %v", claims, err)
	}
	if _, err := keys.Verify(token, now.Add(10*time.Minute+31*time.Second)); err == nil {
		t.Fatal("token beyond legacy 30 second expiry grace was accepted")
	}
}
