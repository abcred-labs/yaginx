package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLookupIPUsesToolsQueryEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ip") != "8.8.8.8" {
			t.Fatalf("unexpected ip query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/Query/QueryCity":
			_, _ = w.Write([]byte(`{"ipAddress":"8.8.8.8","city":{"name":"Mountain View","geoNameId":5375480},"traits":{"isp":"Google","isHostingProvider":true}}`))
		case "/Query/QueryCountry":
			_, _ = w.Write([]byte(`{"ipAddress":"8.8.8.8","country":{"name":"United States","isoCode":"US"}}`))
		case "/Query/QueryAsn":
			_, _ = w.Write([]byte(`{"ipAddress":"8.8.8.8","systemNumber":15169,"systemOrganization":"Google LLC","network":"8.8.8.0/24"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := &Postgres{ipProfileEndpoint: server.URL, ipProfileClient: &http.Client{Timeout: time.Second}}
	profile, err := store.LookupIP(context.Background(), " 8.8.8.8 ")
	if err != nil {
		t.Fatal(err)
	}
	if profile.IP != "8.8.8.8" || profile.Country == nil || profile.Country.ISOCode != "US" || profile.City == nil || profile.City.Name != "Mountain View" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	if profile.ASN == nil || profile.ASN.Number != 15169 || profile.ASN.Network != "8.8.8.0/24" || profile.Network.ISP != "Google" || !profile.Traits["isHostingProvider"] {
		t.Fatalf("unexpected network profile: %+v", profile)
	}
}

func TestLookupIPRejectsInvalidAddress(t *testing.T) {
	store := &Postgres{ipProfileEndpoint: "http://127.0.0.1"}
	if _, err := store.LookupIP(context.Background(), "not-an-ip"); err == nil {
		t.Fatal("expected invalid IP error")
	}
}

func TestLookupIPPropagatesBadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/Query/QueryCity" {
			_, _ = w.Write([]byte("{}"))
			return
		}
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()
	store := &Postgres{ipProfileEndpoint: server.URL, ipProfileClient: server.Client()}
	if _, err := store.LookupIP(context.Background(), "8.8.8.8"); err == nil {
		t.Fatal("expected JSON decode error")
	}
}
