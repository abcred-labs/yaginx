package acme

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateRemoteEndpointRequiresSameClusterOtherNode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/identity" {
			t.Fatalf("identity path=%q", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"service":"yaginx","clusterId":"cluster-1","nodeId":"node-2","version":1}`)
	}))
	defer server.Close()
	if err := ValidateRemoteEndpoint(context.Background(), server.URL, "cluster-1", "node-1", server.Client()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRemoteEndpointRejectsCurrentOrForeignNode(t *testing.T) {
	for _, body := range []string{
		`{"service":"yaginx","clusterId":"cluster-1","nodeId":"node-1"}`,
		`{"service":"yaginx","clusterId":"cluster-2","nodeId":"node-2"}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, body) }))
		if err := ValidateRemoteEndpoint(context.Background(), server.URL, "cluster-1", "node-1", server.Client()); err == nil {
			t.Fatalf("accepted invalid identity %s", body)
		}
		server.Close()
	}
}

func TestKeyValueProviderEncodesToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "a_b-1" {
			t.Error("token was not encoded")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":"value"}`))
	}))
	defer server.Close()
	p := KeyValueProvider{BaseURL: server.URL, SetPath: "/set", GetPath: "/get"}
	if err := p.Present("example.test", "a_b-1", "key-auth"); err != nil {
		t.Fatal(err)
	}
	if value, err := p.Resolve("a_b-1"); err != nil || value != "value" {
		t.Fatalf("%q %v", value, err)
	}
}
