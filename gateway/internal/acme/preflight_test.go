package acme

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

type fixedResolver struct{ ips []net.IP }

func (r fixedResolver) LookupIP(context.Context, string) ([]net.IP, error) { return r.ips, nil }

type preflightRoundTripper struct {
	value    string
	mismatch bool
}

func (t preflightRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Path, "/api/key_value/set") {
		return response(http.StatusOK, `{"code":200}`), nil
	}
	if strings.HasSuffix(r.URL.Path, "/api/key_value/get") {
		return response(http.StatusOK, `{"code":200,"data":"`+t.value+`"}`), nil
	}
	if strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
		value := t.value
		if t.mismatch {
			value = "wrong"
		}
		return response(http.StatusOK, value), nil
	}
	return response(http.StatusNotFound, ""), nil
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestPreflightDomainUsesProviderAndHTTPChallenge(t *testing.T) {
	provider := KeyValueProvider{BaseURL: "http://provider.invalid", SetPath: "/api/key_value/set", GetPath: "/api/key_value/get", Client: &http.Client{Transport: preflightRoundTripper{value: "ignored"}}}
	// The provider response is dynamically expected, so use a transport that
	// echoes the token's deterministic preflight suffix for the read and probe.
	provider.Client.Transport = preflightEchoTransport{}
	err := PreflightDomain(context.Background(), "example.test", provider, []IPResolver{fixedResolver{ips: []net.IP{net.ParseIP("192.0.2.1")}}}, &http.Client{Transport: preflightEchoTransport{}})
	if err != nil {
		t.Fatal(err)
	}
}

type preflightEchoTransport struct{}

func (preflightEchoTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Path, "/api/key_value/set") {
		return response(http.StatusOK, `{"code":200}`), nil
	}
	if strings.HasSuffix(r.URL.Path, "/api/key_value/get") {
		token := r.URL.Query().Get("key")
		return response(http.StatusOK, `{"code":200,"data":"`+token+`.preflight"}`), nil
	}
	if strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
		token := strings.TrimPrefix(r.URL.Path, "/.well-known/acme-challenge/")
		return response(http.StatusOK, token+".preflight"), nil
	}
	return response(http.StatusNotFound, ""), nil
}

func TestPreflightDomainRejectsChallengeMismatch(t *testing.T) {
	provider := KeyValueProvider{BaseURL: "http://provider.invalid", SetPath: "/api/key_value/set", GetPath: "/api/key_value/get", Client: &http.Client{Transport: preflightEchoTransport{}}}
	err := PreflightDomain(context.Background(), "example.test", provider, []IPResolver{fixedResolver{ips: []net.IP{net.ParseIP("192.0.2.1")}}}, &http.Client{Transport: challengeMismatchTransport{}})
	if err == nil || !strings.Contains(err.Error(), "challenge_mismatch") {
		t.Fatalf("expected challenge failure, got %v", err)
	}
}

type challengeMismatchTransport struct{}

func (challengeMismatchTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
		return response(http.StatusOK, "wrong"), nil
	}
	return preflightEchoTransport{}.RoundTrip(r)
}
