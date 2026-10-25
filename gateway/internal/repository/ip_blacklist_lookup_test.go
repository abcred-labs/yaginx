package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func lookupServer(t *testing.T, wantIP, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ip/"+wantIP || r.URL.Query().Get("format") != "json" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestLookupIPUsesToolsIPEndpoint(t *testing.T) {
	server := lookupServer(t, "8.8.8.8", `{"ipAddress":"8.8.8.8","country":{"name":"United States","isoCode":"US"},"asn":{"number":15169,"organization":"Google LLC","network":"8.8.8.8/24"},"location":{"latitude":37.7,"longitude":-97.8,"city":"Mountain View","timeZone":"America/Chicago"}}`, http.StatusOK)
	defer server.Close()

	store := &Postgres{ipProfileEndpoint: server.URL, ipProfileClient: &http.Client{Timeout: time.Second}}
	profile, err := store.LookupIP(context.Background(), " 8.8.8.8 ")
	if err != nil {
		t.Fatal(err)
	}
	if profile.IP != "8.8.8.8" || profile.Country == nil || profile.Country.ISOCode != "US" || profile.City == nil || profile.City.Name != "Mountain View" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	if profile.ASN == nil || profile.ASN.Number != 15169 || profile.ASN.Organization != "Google LLC" || profile.ASN.Network != "8.8.8.0/24" {
		t.Fatalf("expected masked ASN network, got: %+v", profile.ASN)
	}
}

func TestLookupIPHandlesNullsAndBareNetwork(t *testing.T) {
	server := lookupServer(t, "127.0.0.1", `{"ipAddress":"127.0.0.1","country":{"name":null,"isoCode":null},"asn":{"number":0,"organization":null,"network":"127.0.0.1"},"location":{"latitude":null,"longitude":null,"city":null,"timeZone":null}}`, http.StatusOK)
	defer server.Close()

	store := &Postgres{ipProfileEndpoint: server.URL, ipProfileClient: server.Client()}
	profile, err := store.LookupIP(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if profile.ASN != nil || profile.City != nil {
		t.Fatalf("expected empty asn and city, got: %+v", profile)
	}
}

func TestLookupIPSupportsIPv6(t *testing.T) {
	server := lookupServer(t, "2001:4860:4860::8888", `{"ipAddress":"2001:4860:4860::8888","asn":{"number":15169,"organization":"Google LLC","network":"2001:4860:4840::/42"}}`, http.StatusOK)
	defer server.Close()

	store := &Postgres{ipProfileEndpoint: server.URL, ipProfileClient: server.Client()}
	profile, err := store.LookupIP(context.Background(), "2001:4860:4860::8888")
	if err != nil {
		t.Fatal(err)
	}
	if profile.ASN == nil || profile.ASN.Network != "2001:4860:4840::/42" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
}

func TestLookupIPRejectsInvalidAddress(t *testing.T) {
	store := &Postgres{ipProfileEndpoint: "http://127.0.0.1"}
	if _, err := store.LookupIP(context.Background(), "not-an-ip"); err == nil {
		t.Fatal("expected invalid IP error")
	}
}

func TestLookupIPPropagatesBadJSON(t *testing.T) {
	server := lookupServer(t, "8.8.8.8", "not-json", http.StatusOK)
	defer server.Close()
	store := &Postgres{ipProfileEndpoint: server.URL, ipProfileClient: server.Client()}
	if _, err := store.LookupIP(context.Background(), "8.8.8.8"); err == nil {
		t.Fatal("expected JSON decode error")
	}
}

func TestLookupIPReportsNonOKStatus(t *testing.T) {
	server := lookupServer(t, "8.8.8.8", "{}", http.StatusBadGateway)
	defer server.Close()
	store := &Postgres{ipProfileEndpoint: server.URL, ipProfileClient: server.Client()}
	if _, err := store.LookupIP(context.Background(), "8.8.8.8"); err == nil {
		t.Fatal("expected status error")
	}
}
