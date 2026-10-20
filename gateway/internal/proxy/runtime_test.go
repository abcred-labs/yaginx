package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"yaginx-gateway/internal/proxy/event"
	"yaginx-gateway/internal/telemetry"
)

func TestSimpleResponseIsServedBeforeUpstream(t *testing.T) {
	r := New(&Snapshot{Sites: []Site{{Hosts: []string{"example.test"}, Simple: map[string]Response{"/health": {Status: 218, Body: "ok"}}}}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.test/health", nil))
	if w.Code != 218 || w.Body.String() != "ok" {
		t.Fatalf("%d %q", w.Code, w.Body.String())
	}
}

func TestResponseCaptureClassifiesStreamAndMeasuresFirstBytes(t *testing.T) {
	started := time.Now().Add(-25 * time.Millisecond)
	recorder := httptest.NewRecorder()
	capture := &responseCapture{ResponseWriter: recorder, started: started}
	capture.Header().Set("Content-Type", "text/event-stream")
	capture.Flush()
	if capture.TTFBMS() != 0 || capture.TTFTMS() != 0 || capture.ResponseMode() != "stream" {
		t.Fatalf("header flush timing=%d/%d mode=%s", capture.TTFBMS(), capture.TTFTMS(), capture.ResponseMode())
	}
	if _, err := capture.Write([]byte("data: first\n\n")); err != nil {
		t.Fatal(err)
	}
	if capture.ResponseMode() != "stream" || capture.TTFBMS() <= 0 || capture.TTFTMS() <= 0 {
		t.Fatalf("stream timing=%s ttfb=%d ttft=%d", capture.ResponseMode(), capture.TTFBMS(), capture.TTFTMS())
	}
	nonStream := &responseCapture{ResponseWriter: httptest.NewRecorder(), started: started}
	if _, err := nonStream.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if nonStream.ResponseMode() != "non_stream" {
		t.Fatalf("non-stream mode=%q", nonStream.ResponseMode())
	}
}

func TestHostBindingRedirectPreservesRequestURI(t *testing.T) {
	r := New(&Snapshot{Sites: []Site{{RedirectHost: "www.example.test", HostBindings: []HostBinding{{Hostname: "old.example.test", Enabled: true, Role: "alias", AccessMode: "redirect", RedirectStatusCode: http.StatusMovedPermanently}}}}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://old.example.test/api?a=1", nil))
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "https://www.example.test/api?a=1" {
		t.Fatalf("status=%d location=%q", w.Code, w.Header().Get("Location"))
	}
}

func TestUpstreamPoolUsesDefaultWeightOne(t *testing.T) {
	r := New(&Snapshot{})
	pool := UpstreamPool{Strategy: "weightedRoundRobin", Endpoints: []UpstreamEndpoint{{Address: "http://one", Enabled: true}, {Address: "http://two", Enabled: true, Weight: 2}}}
	counts := map[string]int{}
	for i := 0; i < 3; i++ {
		endpoint, _, ok := r.pickEndpoint("pool", pool)
		if !ok {
			t.Fatal("expected endpoint")
		}
		counts[endpoint.Address]++
	}
	if counts["http://one"] != 1 || counts["http://two"] != 2 {
		t.Fatalf("counts=%v", counts)
	}
}

func TestUpstreamPoolStrategies(t *testing.T) {
	r := New(&Snapshot{})
	failover := UpstreamPool{Strategy: "failover", Endpoints: []UpstreamEndpoint{{Address: "http://backup", Enabled: true, Priority: 2}, {Address: "http://primary", Enabled: true, Priority: 1}}}
	endpoint, release, ok := r.pickEndpoint("failover", failover)
	if !ok || endpoint.Address != "http://primary" {
		t.Fatalf("failover endpoint=%#v ok=%v", endpoint, ok)
	}
	release()
	least := UpstreamPool{Strategy: "leastConnections", Endpoints: []UpstreamEndpoint{{Address: "http://one", Enabled: true}, {Address: "http://two", Enabled: true}}}
	first, releaseFirst, ok := r.pickEndpoint("least", least)
	if !ok {
		t.Fatal("expected least-connections endpoint")
	}
	second, releaseSecond, ok := r.pickEndpoint("least", least)
	if !ok || second.Address == first.Address {
		t.Fatalf("least-connections endpoints=%q,%q", first.Address, second.Address)
	}
	releaseFirst()
	releaseSecond()
}

func TestMaintenanceModeStopsSiteProcessingBeforeCORSAndUpstream(t *testing.T) {
	var upstreamCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	runtime := New(&Snapshot{Sites: []Site{{
		Hosts:           []string{"maintenance.test"},
		Destination:     upstream.URL,
		MaintenanceMode: true,
		Simple:          map[string]Response{"/maintenance": {Status: http.StatusOK, Body: "simple"}},
	}}})
	var event telemetry.Event
	runtime.Record = func(value telemetry.Event) { event = value }

	request := httptest.NewRequest(http.MethodOptions, "http://maintenance.test/maintenance", nil)
	request.Header.Set("Origin", "https://console.example.invalid")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" || response.Body.String() != "当前站点正在维护，暂时停止访问。" {
		t.Fatalf("maintenance response=%d headers=%#v body=%q", response.Code, response.Header(), response.Body.String())
	}
	if upstreamCalls != 0 || response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("maintenance must not reach upstream or CORS: upstream=%d headers=%#v", upstreamCalls, response.Header())
	}
	if event.HistoryStatus != http.StatusServiceUnavailable || event.StatusSource != "YaginxPipeline" || event.ErrorCategory != "SiteMaintenance" {
		t.Fatalf("maintenance event=%#v", event)
	}

	runtime.Replace(&Snapshot{Sites: []Site{{Hosts: []string{"maintenance.test"}, Destination: upstream.URL}}})
	response = httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://maintenance.test/", nil))
	if response.Code != http.StatusNoContent || upstreamCalls != 1 {
		t.Fatalf("disabled maintenance response=%d upstream=%d", response.Code, upstreamCalls)
	}
}

func TestMaintenanceModeDoesNotBlockACMEChallenge(t *testing.T) {
	runtime := New(&Snapshot{Sites: []Site{{Hosts: []string{"maintenance.test"}, MaintenanceMode: true}}})
	runtime.ACMEChallenge = func(token string) (string, error) {
		if token != "token" {
			t.Fatalf("token=%q", token)
		}
		return "challenge", nil
	}
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://maintenance.test/.well-known/acme-challenge/token", nil))
	if response.Code != http.StatusOK || response.Body.String() != "challenge" {
		t.Fatalf("ACME response=%d body=%q", response.Code, response.Body.String())
	}
}

func TestUnknownHostFailsClosed(t *testing.T) {
	r := New(nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://unknown.test/", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("%d", w.Code)
	}
}

func TestDefaultSettingsUseConfiguredProxyTimeouts(t *testing.T) {
	settings := DefaultSettings()
	if settings.ActivityTimeout != 5*time.Minute || settings.ResponseHeaderTimeout != 30*time.Second || settings.MaxConcurrentRequests != 256 || settings.MaxRequestHeaderBytes != 65536 || settings.MaxRequestBodyBytes != 0 {
		t.Fatalf("proxy defaults = %#v", settings)
	}
}

func TestConfiguredRuntimeAppliesTransportLimitsAndSelectiveExpectContinue(t *testing.T) {
	var observedExpect string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		observedExpect = request.Header.Get("Expect")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	settings := DefaultSettings()
	settings.ResponseHeaderTimeout = 7 * time.Second
	settings.MaxIdleConnections = 12
	settings.MaxIdleConnectionsPerHost = 5
	settings.IdleConnectionTimeout = 8 * time.Second
	settings.ExpectContinueTimeout = 2 * time.Second
	settings.UpstreamExpectContinue = ExpectContinuePolicy{Enabled: true, MinContentLengthBytes: 3, IncludedHosts: []string{"https://Ai.Example.Test"}, IncludedPathPrefixes: []string{"/api/image-gen"}}
	runtime, err := NewConfigured(&Snapshot{Sites: []Site{{Hosts: []string{"ai.example.test"}, Destination: upstream.URL}}}, settings)
	if err != nil {
		t.Fatal(err)
	}
	if got := runtime.transport; got.ResponseHeaderTimeout != 7*time.Second || got.MaxIdleConns != 12 || got.MaxIdleConnsPerHost != 5 || got.IdleConnTimeout != 8*time.Second || got.ExpectContinueTimeout != 2*time.Second {
		t.Fatalf("transport=%#v", got)
	}
	request := httptest.NewRequest(http.MethodPost, "http://ai.example.test/api/image-gen/item", strings.NewReader("1234"))
	request.Host = "AI.EXAMPLE.TEST"
	runtime.ServeHTTP(httptest.NewRecorder(), request)
	if observedExpect != "100-continue" {
		t.Fatalf("policy did not add Expect header: %q", observedExpect)
	}

	for _, item := range []struct {
		name    string
		method  string
		path    string
		header  string
		upgrade bool
	}{
		{name: "wrong path", method: http.MethodPost, path: "/api/image-generation"},
		{name: "wrong method", method: http.MethodGet, path: "/api/image-gen"},
		{name: "existing client expectation", method: http.MethodPost, path: "/api/image-gen", header: "100-continue"},
		{name: "upgrade", method: http.MethodPost, path: "/api/image-gen", upgrade: true},
	} {
		t.Run(item.name, func(t *testing.T) {
			observedExpect = ""
			req := httptest.NewRequest(item.method, "http://ai.example.test"+item.path, strings.NewReader("1234"))
			req.Host = "ai.example.test"
			if item.header != "" {
				req.Header.Set("Expect", item.header)
			}
			if item.upgrade {
				req.Header.Set("Upgrade", "websocket")
				req.Header.Set("Connection", "Upgrade")
			}
			runtime.ServeHTTP(httptest.NewRecorder(), req)
			if observedExpect != item.header {
				t.Fatalf("Expect=%q, want %q", observedExpect, item.header)
			}
		})
	}
}

func TestConfiguredRuntimeBlacklistUsesLegacyFirstForwardedCandidate(t *testing.T) {
	var warnings []string
	runtime, err := NewConfigured(&Snapshot{Sites: []Site{{Hosts: []string{"blocked.test"}, Simple: map[string]Response{"/": {Status: http.StatusOK, Body: "ok"}}}}}, Settings{
		ActivityTimeout: 15 * time.Minute, ResponseHeaderTimeout: 15 * time.Minute, MaxConcurrentRequests: 1, MaxRequestHeaderBytes: 1 << 20, MaxRequestBodyBytes: 1, MaxIdleConnections: 1, MaxIdleConnectionsPerHost: 1, IdleConnectionTimeout: time.Second, ExpectContinueTimeout: time.Second,
		IPBlacklistCIDRs:          []string{"198.51.100.0/24"},
		BlacklistCandidateWarning: func(candidate, remote, host string) { warnings = append(warnings, candidate+"|"+remote+"|"+host) },
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://blocked.test/", nil)
	req.RemoteAddr = "203.0.113.8:42"
	req.Header.Set("X-Forwarded-For", "198.51.100.9, 203.0.113.8")
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden || response.Body.String() != "Forbidden" {
		t.Fatalf("response=%d %q", response.Code, response.Body.String())
	}
	if len(warnings) != 1 || warnings[0] != "198.51.100.9|203.0.113.8|blocked.test" {
		t.Fatalf("warnings=%v", warnings)
	}

	challenge, err := NewConfigured(nil, Settings{ActivityTimeout: 15 * time.Minute, ResponseHeaderTimeout: 15 * time.Minute, MaxConcurrentRequests: 1, MaxRequestHeaderBytes: 1 << 20, MaxRequestBodyBytes: 1, MaxIdleConnections: 1, MaxIdleConnectionsPerHost: 1, IdleConnectionTimeout: time.Second, ExpectContinueTimeout: time.Second, IPBlacklistCIDRs: []string{"0.0.0.0/0"}})
	if err != nil {
		t.Fatal(err)
	}
	challenge.ACMEChallenge = func(string) (string, error) { return "challenge", nil }
	response = httptest.NewRecorder()
	challengeRequest := httptest.NewRequest(http.MethodGet, "http://blocked.test/.well-known/acme-challenge/token", nil)
	challengeRequest.RemoteAddr = "198.51.100.9:42"
	challenge.ServeHTTP(response, challengeRequest)
	if response.Code != http.StatusOK || response.Body.String() != "challenge" {
		t.Fatalf("ACME response=%d %q", response.Code, response.Body.String())
	}
}

func TestSnapshotBlacklistSelectsMostSpecificRuleAndRecordsIt(t *testing.T) {
	runtime := New(&Snapshot{
		Sites: []Site{{Hosts: []string{"blocked.test"}, Simple: map[string]Response{"/": {Status: http.StatusOK, Body: "ok"}}}},
		Blacklist: []BlacklistRule{
			{ID: 11, CIDR: "198.51.100.0/24"},
			{ID: 12, CIDR: "198.51.100.0/25"},
			{ID: 13, CIDR: "2001:db8::/32"},
		},
	})
	var events []telemetry.Event
	runtime.Record = func(event telemetry.Event) { events = append(events, event) }

	ipv4 := httptest.NewRequest(http.MethodGet, "http://blocked.test/", nil)
	ipv4.RemoteAddr = "198.51.100.9:42"
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, ipv4)
	if response.Code != http.StatusForbidden || len(events) != 1 || events[0].BlacklistRuleID != 12 || events[0].ClientIP != "198.51.100.9" {
		t.Fatalf("IPv4 blacklist response=%d events=%#v", response.Code, events)
	}

	ipv6 := httptest.NewRequest(http.MethodGet, "http://blocked.test/", nil)
	ipv6.RemoteAddr = "[2001:db8::42]:42"
	response = httptest.NewRecorder()
	runtime.ServeHTTP(response, ipv6)
	if response.Code != http.StatusForbidden || len(events) != 2 || events[1].BlacklistRuleID != 13 || events[1].ClientIP != "2001:db8::42" {
		t.Fatalf("IPv6 blacklist response=%d events=%#v", response.Code, events)
	}
}

func TestSiteIPAccessPolicies(t *testing.T) {
	newRequest := func(ip string) *http.Request {
		request := httptest.NewRequest(http.MethodGet, "http://site.test/", nil)
		request.RemoteAddr = net.JoinHostPort(ip, "42")
		return request
	}
	serve := func(site Site, request *http.Request) int {
		runtime := New(&Snapshot{Sites: []Site{site}})
		response := httptest.NewRecorder()
		runtime.ServeHTTP(response, request)
		return response.Code
	}

	blacklist := Site{Hosts: []string{"site.test"}, IPAccessMode: "blacklist", IPAccessCIDRs: []string{"198.51.100.0/24"}, Simple: map[string]Response{"/": {Status: http.StatusOK, Body: "ok"}}}
	if got := serve(blacklist, newRequest("198.51.100.9")); got != http.StatusForbidden {
		t.Fatalf("site blacklist status=%d", got)
	}
	if got := serve(blacklist, newRequest("203.0.113.9")); got != http.StatusOK {
		t.Fatalf("site blacklist non-match status=%d", got)
	}

	whitelist := Site{Hosts: []string{"site.test"}, IPAccessMode: "whitelist", IPAccessCIDRs: []string{"2001:db8::42"}, Simple: map[string]Response{"/": {Status: http.StatusOK, Body: "ok"}}}
	ipv6 := newRequest("2001:db8::42")
	if got := serve(whitelist, ipv6); got != http.StatusOK {
		t.Fatalf("site whitelist match status=%d", got)
	}
	if got := serve(whitelist, newRequest("2001:db8::43")); got != http.StatusForbidden {
		t.Fatalf("site whitelist non-match status=%d", got)
	}

	disabled := Site{Hosts: []string{"site.test"}, IPAccessMode: "disabled", IPAccessCIDRs: []string{"198.51.100.0/24"}, Simple: map[string]Response{"/": {Status: http.StatusOK, Body: "ok"}}}
	if got := serve(disabled, newRequest("198.51.100.9")); got != http.StatusOK {
		t.Fatalf("disabled site access status=%d", got)
	}
}

func TestGlobalBlacklistOverridesSiteWhitelist(t *testing.T) {
	runtime := New(&Snapshot{
		Sites:     []Site{{Hosts: []string{"site.test"}, IPAccessMode: "whitelist", IPAccessCIDRs: []string{"198.51.100.9/32"}, Simple: map[string]Response{"/": {Status: http.StatusOK}}}},
		Blacklist: []BlacklistRule{{ID: 7, CIDR: "198.51.100.0/24"}},
	})
	request := httptest.NewRequest(http.MethodGet, "http://site.test/", nil)
	request.RemoteAddr = "198.51.100.9:42"
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("global blacklist override status=%d", response.Code)
	}
}

func TestSiteIPAccessDoesNotApplyToACMEChallenge(t *testing.T) {
	runtime := New(&Snapshot{Sites: []Site{{Hosts: []string{"site.test"}, IPAccessMode: "whitelist", IPAccessCIDRs: []string{"203.0.113.9/32"}}}})
	runtime.ACMEChallenge = func(string) (string, error) { return "challenge", nil }
	request := httptest.NewRequest(http.MethodGet, "http://site.test/.well-known/acme-challenge/token", nil)
	request.RemoteAddr = "198.51.100.9:42"
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "challenge" {
		t.Fatalf("ACME bypass response=%d %q", response.Code, response.Body.String())
	}
}

func TestConfiguredRuntimeRejectsUnsafeLimits(t *testing.T) {
	settings := DefaultSettings()
	settings.IPBlacklistCIDRs = []string{"not-a-cidr"}
	if _, err := NewConfigured(nil, settings); err == nil {
		t.Fatal("invalid CIDR accepted")
	}
	settings = DefaultSettings()
	settings.MaxRequestHeaderBytes = 1
	if _, err := NewConfigured(nil, settings); err == nil {
		t.Fatal("unsafe header limit accepted")
	}
	settings = DefaultSettings()
	settings.MaxRequestBodyBytes = -1
	if _, err := NewConfigured(nil, settings); err == nil {
		t.Fatal("negative request body limit accepted")
	}
}
func TestTelemetryCapturesResponseFacts(t *testing.T) {
	var got telemetry.Event
	r := New(&Snapshot{Sites: []Site{{Hosts: []string{"example.test"}, Simple: map[string]Response{"/health": {Status: 201, Body: "hello"}}}}})
	r.Record = func(event telemetry.Event) { got = event }
	req := httptest.NewRequest(http.MethodPost, "http://example.test/health", strings.NewReader("body"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got.Status != http.StatusCreated || got.Inbound != 4 || got.Outbound != int64(len("hello")) || got.Scheme != "http" {
		t.Fatalf("unexpected telemetry: %#v", got)
	}
}

func TestTelemetryClassifiesProxyHistoryWithoutChangingHTTPStatus(t *testing.T) {
	var events []telemetry.Event
	record := func(event telemetry.Event) { events = append(events, event) }
	noDestination := New(&Snapshot{Sites: []Site{{Hosts: []string{"no-destination.test"}}}})
	noDestination.Record = record
	noDestination.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://no-destination.test/", nil))
	if len(events) != 1 || events[0].HistoryStatus != http.StatusNotFound || events[0].StatusSource != "YaginxPipeline" || events[0].ErrorCategory != "NoRoute" {
		t.Fatalf("no destination event=%#v", events)
	}
	events = nil
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer upstream.Close()
	runtime := New(&Snapshot{Sites: []Site{{Hosts: []string{"upstream.test"}, Destination: upstream.URL}}})
	runtime.Record = record
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://upstream.test/", nil))
	if response.Code != http.StatusBadGateway || len(events) != 1 || events[0].HistoryStatus != http.StatusBadGateway || events[0].StatusSource != "Upstream" || events[0].ErrorCategory != "UpstreamReturnedError" {
		t.Fatalf("upstream response=%d events=%#v", response.Code, events)
	}
	events = nil
	refused := New(&Snapshot{Sites: []Site{{Hosts: []string{"refused.test"}, Destination: "http://127.0.0.1:1"}}})
	refused.Record = record
	response = httptest.NewRecorder()
	refused.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://refused.test/", nil))
	if response.Code != http.StatusBadGateway || len(events) != 1 || events[0].HistoryStatus != 604 || events[0].StatusSource != "YaginxProxy" || events[0].ErrorCategory != "UpstreamRequestFailure" {
		t.Fatalf("refused response=%d events=%#v", response.Code, events)
	}
}

func TestClassifyProxyErrorRecognizesCancellationAndTimeout(t *testing.T) {
	contextValue, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil).WithContext(contextValue)
	if status, source, category := classifyProxyError(request, context.Canceled); status != 601 || source != "ClientCanceled" || category != "ClientCanceled" {
		t.Fatalf("canceled classification=%d/%s/%s", status, source, category)
	}
	bodyLimitRequest := httptest.NewRequest(http.MethodPost, "http://example.test/upload", nil)
	limited := http.MaxBytesReader(nil, io.NopCloser(strings.NewReader("body")), 1)
	_, readErr := io.ReadAll(limited)
	if status, source, category := classifyProxyError(bodyLimitRequest, readErr); status != http.StatusRequestEntityTooLarge || source != "YaginxProxy" || category != "RequestBodyTooLarge" {
		t.Fatalf("body limit classification=%d/%s/%s err=%v", status, source, category, readErr)
	}
	if status, source, category := classifyProxyError(httptest.NewRequest(http.MethodGet, "http://example.test/", nil), &net.DNSError{IsTimeout: true}); status != 602 || source != "YaginxProxy" || category != "UpstreamTimeout" {
		t.Fatalf("timeout classification=%d/%s/%s", status, source, category)
	}
	if status, source, category := classifyProxyError(httptest.NewRequest(http.MethodGet, "http://example.test/", nil), errors.New("unknown transport error")); status != 610 || source != "Unknown" || category != "UnknownProxyError" {
		t.Fatalf("unknown classification=%d/%s/%s", status, source, category)
	}
}

func TestRuntimeReturns413ForRequestBodyLimit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	settings := DefaultSettings()
	settings.MaxRequestBodyBytes = 3
	runtime, err := NewConfigured(&Snapshot{Sites: []Site{{Hosts: []string{"limited.test"}, Destination: upstream.URL}}}, settings)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://limited.test/upload", strings.NewReader("1234"))
	request.Body = http.MaxBytesReader(nil, request.Body, settings.MaxRequestBodyBytes)
	request.ContentLength = -1
	request.TransferEncoding = []string{"chunked"}
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "request body too large") {
		t.Fatalf("response=%d body=%q", response.Code, response.Body.String())
	}
}

func TestResponseCaptureClassifiesClientWriteFailure(t *testing.T) {
	capture := &responseCapture{ResponseWriter: failingResponseWriter{header: make(http.Header)}}
	if _, err := capture.Write([]byte("body")); err == nil {
		t.Fatal("expected client write error")
	}
	if capture.HistoryStatus() != 608 || capture.StatusSource() != "YaginxProxy" || capture.ErrorCategory() != "ClientWriteFailure" {
		t.Fatalf("capture=%#v", capture)
	}
}

func TestErrorClassifyingBodyRecordsUpstreamBodyFailure(t *testing.T) {
	called := false
	body := &errorClassifyingBody{ReadCloser: errorReadCloser{}, classify: func() { called = true }}
	if _, err := body.Read(make([]byte, 1)); err == nil || !called {
		t.Fatalf("body error=%v classified=%t", err, called)
	}
}

func TestRuntimeClassifiesRawUpstreamHeaderAndBodyFailures(t *testing.T) {
	headerUpstream := rawUpstream(t, func(connection net.Conn) {
		_, _ = http.ReadRequest(bufio.NewReader(connection))
	})
	defer headerUpstream.Close()
	var headerEvent telemetry.Event
	runtime := New(&Snapshot{Sites: []Site{{Hosts: []string{"header-failure.test"}, Destination: "http://" + headerUpstream.Addr().String()}}})
	runtime.Record = func(event telemetry.Event) { headerEvent = event }
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://header-failure.test/", nil))
	if response.Code != http.StatusBadGateway || headerEvent.HistoryStatus != 605 || headerEvent.StatusSource != "YaginxProxy" || headerEvent.ErrorCategory != "UpstreamResponseHeaderFailure" {
		t.Fatalf("header failure response=%d event=%#v", response.Code, headerEvent)
	}

	bodyUpstream := rawUpstream(t, func(connection net.Conn) {
		_, _ = http.ReadRequest(bufio.NewReader(connection))
		_, _ = io.WriteString(connection, "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 20\r\nConnection: close\r\n\r\nshort-body")
	})
	defer bodyUpstream.Close()
	var bodyEvent telemetry.Event
	runtime = New(&Snapshot{Sites: []Site{{Hosts: []string{"body-failure.test"}, Destination: "http://" + bodyUpstream.Addr().String()}}})
	runtime.Record = func(event telemetry.Event) { bodyEvent = event }
	response = httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://body-failure.test/", nil))
	if response.Code != http.StatusOK || response.Body.String() != "short-body" || bodyEvent.HistoryStatus != 606 || bodyEvent.StatusSource != "YaginxProxy" || bodyEvent.ErrorCategory != "UpstreamResponseBodyFailure" {
		t.Fatalf("body failure response=%d/%q event=%#v", response.Code, response.Body.String(), bodyEvent)
	}
}

func TestRuntimeClassifiesRequestBodyReadFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer upstream.Close()
	var event telemetry.Event
	runtime := New(&Snapshot{Sites: []Site{{Hosts: []string{"request-body-failure.test"}, Destination: upstream.URL}}})
	runtime.Record = func(value telemetry.Event) { event = value }
	request := httptest.NewRequest(http.MethodPost, "http://request-body-failure.test/upload", nil)
	request.Body = errorReadCloser{}
	request.ContentLength = 10
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || event.HistoryStatus != 607 || event.StatusSource != "YaginxProxy" || event.ErrorCategory != "UpstreamRequestBodyFailure" {
		t.Fatalf("request body failure response=%d event=%#v", response.Code, event)
	}
}

func TestRuntimeClassifiesCanceledRequestContext(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer upstream.Close()
	var recordedEvent telemetry.Event
	historyCalls := 0
	runtime := New(&Snapshot{Sites: []Site{{Hosts: []string{"canceled.test"}, Destination: upstream.URL}}})
	runtime.HistoryCapture = &HistoryCapture{
		Decide: func(status int, statusSource, errorCategory string) event.HistoryDecision {
			return (telemetry.HistoryPolicy{Enabled: true}).Decide(telemetry.Event{
				Status: status, HistoryStatus: status, StatusSource: statusSource, ErrorCategory: errorCategory,
			})
		},
	}
	runtime.RecordHistory = func(event.Event) { historyCalls++ }
	runtime.Record = func(value telemetry.Event) { recordedEvent = value }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "http://canceled.test/", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || recordedEvent.HistoryStatus != 601 || recordedEvent.StatusSource != "ClientCanceled" || recordedEvent.ErrorCategory != "ClientCanceled" {
		t.Fatalf("canceled response=%d event=%#v", response.Code, recordedEvent)
	}
	if historyCalls != 0 {
		t.Fatalf("client cancellation entered history persistence: %d", historyCalls)
	}
}

func TestRuntimeClassifiesUpstreamResponseHeaderTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	var event telemetry.Event
	runtime := New(&Snapshot{Sites: []Site{{Hosts: []string{"timeout.test"}, Destination: upstream.URL}}})
	runtime.transport.ResponseHeaderTimeout = 5 * time.Millisecond
	runtime.Record = func(value telemetry.Event) { event = value }
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://timeout.test/", nil))
	if response.Code != http.StatusBadGateway || event.HistoryStatus != 602 || event.StatusSource != "YaginxProxy" || event.ErrorCategory != "UpstreamTimeout" {
		t.Fatalf("timeout response=%d event=%#v", response.Code, event)
	}
}

func TestSiteResponseHeaderTimeoutOverridesGlobalSetting(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	runtime := New(&Snapshot{Sites: []Site{
		{Hosts: []string{"slow.test"}, Destination: upstream.URL, ResponseHeaderTimeout: 2 * time.Second},
		{Hosts: []string{"fast.test"}, Destination: upstream.URL},
	}})
	runtime.transport.ResponseHeaderTimeout = 5 * time.Millisecond

	override := httptest.NewRecorder()
	runtime.ServeHTTP(override, httptest.NewRequest(http.MethodGet, "http://slow.test/", nil))
	if override.Code != http.StatusOK {
		t.Fatalf("site override should outlive global timeout, got %d", override.Code)
	}
	inherited := httptest.NewRecorder()
	runtime.ServeHTTP(inherited, httptest.NewRequest(http.MethodGet, "http://fast.test/", nil))
	if inherited.Code != http.StatusBadGateway {
		t.Fatalf("site without override should use global timeout, got %d", inherited.Code)
	}
}

func TestDerivedTransportIsCachedPerOverride(t *testing.T) {
	r := New(nil)
	first, err := r.transportFor("", false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := r.transportFor("", false, time.Minute)
	other, _ := r.transportFor("", false, 2*time.Minute)
	if first != second || first == other || first.(*http.Transport).ResponseHeaderTimeout != time.Minute {
		t.Fatal("derived transports must be cached by override and carry the timeout")
	}
	if shared, _ := r.transportFor("", false, r.transport.ResponseHeaderTimeout); shared != http.RoundTripper(r.transport) {
		t.Fatal("override equal to the global value should reuse the shared transport")
	}
}

func TestRuntimeClassifiesPipelineExceptionAndInvalidTransport(t *testing.T) {
	var panicEvent telemetry.Event
	runtime := New(&Snapshot{Sites: []Site{{Hosts: []string{"pipeline.test"}}}})
	runtime.ACMEChallenge = func(string) (string, error) { panic("test pipeline exception") }
	runtime.Record = func(value telemetry.Event) { panicEvent = value }
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://pipeline.test/.well-known/acme-challenge/panic", nil))
	if response.Code != 609 || panicEvent.HistoryStatus != 609 || panicEvent.StatusSource != "YaginxPipeline" || panicEvent.ErrorCategory != "ProxyPipelineException" {
		t.Fatalf("panic response=%d event=%#v", response.Code, panicEvent)
	}

	var transportEvent telemetry.Event
	runtime = New(&Snapshot{Sites: []Site{{Hosts: []string{"invalid-transport.test"}, Destination: "http://127.0.0.1:1", WebProxy: "://invalid"}}})
	runtime.Record = func(value telemetry.Event) { transportEvent = value }
	response = httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://invalid-transport.test/", nil))
	if response.Code != 609 || transportEvent.HistoryStatus != 609 || transportEvent.StatusSource != "YaginxPipeline" || transportEvent.ErrorCategory != "ProxyPipelineException" {
		t.Fatalf("transport response=%d event=%#v", response.Code, transportEvent)
	}
}

func TestRuntimeClassifiesUnknownRoundTripError(t *testing.T) {
	var event telemetry.Event
	runtime := New(&Snapshot{Sites: []Site{{Hosts: []string{"unknown.test"}, Destination: "http://unknown.test"}}})
	runtime.transportFactory = func(string, bool) (http.RoundTripper, error) {
		return roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("unclassified test transport error")
		}), nil
	}
	runtime.Record = func(value telemetry.Event) { event = value }
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://unknown.test/", nil))
	if response.Code != http.StatusBadGateway || event.HistoryStatus != 610 || event.StatusSource != "Unknown" || event.ErrorCategory != "UnknownProxyError" {
		t.Fatalf("unknown response=%d event=%#v", response.Code, event)
	}
}

func TestRuntimeRetainsSnapshotForInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	continueOld := make(chan struct{})
	oldUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-continueOld
		_, _ = io.WriteString(w, "old")
	}))
	defer oldUpstream.Close()
	newUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "new") }))
	defer newUpstream.Close()
	runtime := New(&Snapshot{Revision: 1, Sites: []Site{{Hosts: []string{"reload.test"}, Destination: oldUpstream.URL}}})
	first := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		runtime.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "http://reload.test/long", nil))
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("old upstream was not reached")
	}
	runtime.Replace(&Snapshot{Revision: 2, Sites: []Site{{Hosts: []string{"reload.test"}, Destination: newUpstream.URL}}})
	close(continueOld)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("in-flight request did not complete")
	}
	if first.Code != http.StatusOK || first.Body.String() != "old" {
		t.Fatalf("in-flight response=%d/%q", first.Code, first.Body.String())
	}
	second := httptest.NewRecorder()
	runtime.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "http://reload.test/new", nil))
	if second.Code != http.StatusOK || second.Body.String() != "new" || runtime.Snapshot().Revision != 2 {
		t.Fatalf("post-reload response=%d/%q revision=%d", second.Code, second.Body.String(), runtime.Snapshot().Revision)
	}
}

func rawUpstream(t *testing.T, respond func(net.Conn)) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(time.Second))
		respond(connection)
	}()
	return listener
}

type failingResponseWriter struct{ header http.Header }

func (w failingResponseWriter) Header() http.Header     { return w.header }
func (failingResponseWriter) WriteHeader(int)           {}
func (failingResponseWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type errorReadCloser struct{}

func (errorReadCloser) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (errorReadCloser) Close() error             { return nil }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestRuntimeRedirectsToPrimaryHostWithEmptyLegacy302(t *testing.T) {
	runtime := New(&Snapshot{Sites: []Site{{
		Hosts:         []string{"primary.example.invalid", "alias.example.invalid"},
		RedirectHost:  "primary.example.invalid",
		RedirectHTTPS: true,
	}}})
	var event telemetry.Event
	runtime.Record = func(value telemetry.Event) { event = value }
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://alias.example.invalid/encoded%2Fpath?x=1", nil))
	if response.Code != http.StatusFound || response.Header().Get("Location") != "https://primary.example.invalid/encoded%2Fpath?x=1" || response.Header().Get("Content-Length") != "0" || response.Body.Len() != 0 {
		t.Fatalf("redirect response=%d headers=%#v body=%q", response.Code, response.Header(), response.Body.String())
	}
	if event.HistoryStatus != http.StatusFound || event.StatusSource != "YaginxPipeline" {
		t.Fatalf("redirect event=%#v", event)
	}
}

func TestTelemetryCapturesBoundedErrorMetadataWithoutChangingStreams(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Body; got == nil {
			t.Fatal("upstream body is nil")
		}
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Upstream", "metadata")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"response":"is-long"}`))
	}))
	defer upstream.Close()
	var got telemetry.Event
	runtime := New(&Snapshot{Sites: []Site{{Hosts: []string{"metadata.test"}, Destination: upstream.URL}}})
	runtime.HistoryCapture = &HistoryCapture{BodyMaxBytes: 8, HeaderMaxBytes: 128, BodyContentTypes: []string{"application/json"}}
	runtime.Record = func(event telemetry.Event) { got = event }
	request := httptest.NewRequest(http.MethodPost, "http://metadata.test/path", strings.NewReader(`{"request":"is-long"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request", "metadata")
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || response.Body.String() != `{"response":"is-long"}` {
		t.Fatalf("response=%d/%q", response.Code, response.Body.String())
	}
	if got.HistoryStatus != http.StatusBadGateway || got.RequestHeaders["X-Request"][0] != "metadata" || got.ResponseHeaders["X-Upstream"][0] != "metadata" {
		t.Fatalf("history metadata=%#v", got)
	}
	if got.RequestBodyBase64 == "" || got.ResponseBodyBase64 == "" || !got.RequestBodyTruncated || got.ResponseBodyTruncated {
		t.Fatalf("history bodies=%#v", got)
	}
	if got.UpstreamStatus == nil || *got.UpstreamStatus != http.StatusBadGateway {
		t.Fatalf("upstream status=%v", got.UpstreamStatus)
	}
}

func TestResponseBodyCaptureCompressionPolicy(t *testing.T) {
	encode := func(t *testing.T, body []byte) []byte {
		t.Helper()
		var encoded bytes.Buffer
		writer := gzip.NewWriter(&encoded)
		if _, err := writer.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return encoded.Bytes()
	}
	run := func(t *testing.T, body []byte, encoding, contentType, disposition string) ([]byte, telemetry.Event) {
		t.Helper()
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentType)
			if encoding != "" {
				w.Header().Set("Content-Encoding", encoding)
			}
			if disposition != "" {
				w.Header().Set("Content-Disposition", disposition)
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(body)
		}))
		defer upstream.Close()
		runtime := New(&Snapshot{Sites: []Site{{Hosts: []string{"capture.test"}, Destination: upstream.URL}}})
		runtime.HistoryCapture = &HistoryCapture{HeaderMaxBytes: 128, BodyContentTypes: []string{"text/", "application/json", "application/xml", "+json", "+xml"}}
		var event telemetry.Event
		runtime.Record = func(value telemetry.Event) { event = value }
		response := httptest.NewRecorder()
		runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://capture.test/", nil))
		return response.Body.Bytes(), event
	}

	t.Run("gzip records decoded text", func(t *testing.T) {
		plain := []byte("small compressed response")
		encoded := encode(t, plain)
		forwarded, event := run(t, encoded, "gzip", "text/plain; charset=utf-8", "")
		if !bytes.Equal(forwarded, encoded) || event.ResponseBodyBase64 != base64.StdEncoding.EncodeToString(plain) || event.ResponseBodyTruncated {
			t.Fatalf("forwarded=%d/%d event=%#v", len(forwarded), len(encoded), event)
		}
	})

	t.Run("deflate records decoded text", func(t *testing.T) {
		plain := []byte("small deflate response")
		var encoded bytes.Buffer
		writer := zlib.NewWriter(&encoded)
		if _, err := writer.Write(plain); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		forwarded, event := run(t, encoded.Bytes(), "deflate", "text/plain", "")
		if !bytes.Equal(forwarded, encoded.Bytes()) || event.ResponseBodyBase64 != base64.StdEncoding.EncodeToString(plain) || event.ResponseBodyTruncated {
			t.Fatalf("forwarded=%d/%d event=%#v", len(forwarded), encoded.Len(), event)
		}
	})

	t.Run("compressed bytes over 512 are omitted", func(t *testing.T) {
		plain := make([]byte, 1024)
		for i := range plain {
			plain[i] = byte((i*73 + i/7) % 251)
		}
		encoded := encode(t, plain)
		if len(encoded) <= compressedResponseBodyMaxBytes {
			t.Fatalf("test gzip body=%d, want >%d", len(encoded), compressedResponseBodyMaxBytes)
		}
		forwarded, event := run(t, encoded, "gzip", "text/plain", "")
		if !bytes.Equal(forwarded, encoded) || event.ResponseBodyBase64 != "" || !event.ResponseBodyTruncated {
			t.Fatalf("forwarded=%d/%d event=%#v", len(forwarded), len(encoded), event)
		}
	})

	t.Run("decoded body at 4K is omitted", func(t *testing.T) {
		plain := bytes.Repeat([]byte{'x'}, responseBodyMaxBytes+1)
		encoded := encode(t, plain)
		forwarded, event := run(t, encoded, "gzip", "text/plain", "")
		if !bytes.Equal(forwarded, encoded) || event.ResponseBodyBase64 != "" || !event.ResponseBodyTruncated {
			t.Fatalf("forwarded=%d/%d event=%#v", len(forwarded), len(encoded), event)
		}
	})

	t.Run("unsupported encoding is omitted", func(t *testing.T) {
		body := []byte("encoded but not decoded")
		forwarded, event := run(t, body, "br", "text/plain", "")
		if !bytes.Equal(forwarded, body) || event.ResponseBodyBase64 != "" || event.ResponseBodyTruncated {
			t.Fatalf("forwarded=%d/%d event=%#v", len(forwarded), len(body), event)
		}
	})

	t.Run("binary and attachment responses are omitted", func(t *testing.T) {
		body := []byte("small body")
		if forwarded, event := run(t, body, "", "application/octet-stream", ""); !bytes.Equal(forwarded, body) || event.ResponseBodyBase64 != "" {
			t.Fatalf("binary forwarded=%q event=%#v", forwarded, event)
		}
		if forwarded, event := run(t, body, "", "text/plain", `attachment; filename="body.txt"`); !bytes.Equal(forwarded, body) || event.ResponseBodyBase64 != "" {
			t.Fatalf("attachment forwarded=%q event=%#v", forwarded, event)
		}
	})
}

func TestResponseBodyCaptureUncompressedBoundary(t *testing.T) {
	for _, test := range []struct {
		name      string
		length    int
		truncated bool
	}{
		{name: "below limit", length: responseBodyMaxBytes, truncated: false},
		{name: "at limit", length: responseBodyMaxBytes + 1, truncated: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			capture := newResponseBodyCapture("", int64(test.length))
			capture.Append(bytes.Repeat([]byte{'x'}, test.length))
			encoded, truncated := capture.Result(true)
			if truncated != test.truncated {
				t.Fatalf("truncated=%t want %t", truncated, test.truncated)
			}
			if test.truncated && encoded != "" {
				t.Fatalf("encoded oversized body=%q", encoded)
			}
			if !test.truncated && encoded == "" {
				t.Fatal("small body was not captured")
			}
		})
	}
}
func TestResponseCapturePreservesReaderFrom(t *testing.T) {
	w := httptest.NewRecorder()
	capture := &responseCapture{ResponseWriter: w}
	if _, err := capture.ReadFrom(strings.NewReader("stream")); err != nil {
		t.Fatal(err)
	}
	if capture.Status() != http.StatusOK || capture.Bytes() != 6 || w.Body.String() != "stream" {
		t.Fatalf("status=%d bytes=%d body=%q", capture.Status(), capture.Bytes(), w.Body.String())
	}
}

func TestResponseCaptureLazilyAllocatesBodyCapture(t *testing.T) {
	writer := httptest.NewRecorder()
	capture := &responseCapture{
		ResponseWriter: writer,
		captureLimit:   1 << 20,
		captureEnabled: func() bool { return false },
	}
	if _, err := capture.Write([]byte("success")); err != nil {
		t.Fatal(err)
	}
	if capture.capture != nil {
		t.Fatal("successful response allocated a body capture")
	}

	writer = httptest.NewRecorder()
	capture = &responseCapture{
		ResponseWriter: writer,
		captureLimit:   8,
		captureEnabled: func() bool { return true },
	}
	if _, err := capture.Write([]byte("error")); err != nil {
		t.Fatal(err)
	}
	if capture.capture == nil {
		t.Fatal("eligible response did not allocate a body capture")
	}
}

func TestNewLimitedBodyCaptureDoesNotPreallocate(t *testing.T) {
	capture := newLimitedBodyCapture(8192)
	if cap(capture.value) != 0 {
		t.Fatalf("body capture capacity=%d, want zero before first read", cap(capture.value))
	}
	capture.Append([]byte("body"))
	if string(capture.value) != "body" || cap(capture.value) == 0 {
		t.Fatalf("body capture after append=%q capacity=%d", capture.value, cap(capture.value))
	}
}

func TestHistoryMetadataIsLazyWhenPolicyDoesNotRecordSuccess(t *testing.T) {
	runtime := New(&Snapshot{Sites: []Site{{
		Hosts:  []string{"lazy-metadata.test"},
		Simple: map[string]Response{"/": {Status: http.StatusOK, Body: "ok"}},
	}}})
	runtime.HistoryCapture = &HistoryCapture{
		BodyMaxBytes:   8,
		HeaderMaxBytes: 128,
		ShouldCaptureMetadata: func(int, string, string) bool {
			return false
		},
	}
	var event telemetry.Event
	runtime.Record = func(value telemetry.Event) { event = value }
	request := httptest.NewRequest(http.MethodGet, "http://lazy-metadata.test/", nil)
	request.Header.Set("X-Large-Metadata", strings.Repeat("x", 64))
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "ok" {
		t.Fatalf("response=%d/%q", response.Code, response.Body.String())
	}
	if event.RequestHeaders != nil || event.ResponseHeaders != nil || event.RequestContentType != "" || event.ResponseContentType != "" {
		t.Fatalf("successful metadata was copied: %#v", event)
	}
}

func TestHistoryDecisionControlsMetadataAndPersistenceTogether(t *testing.T) {
	runtime := New(&Snapshot{Sites: []Site{{
		Hosts:  []string{"sampled-history.test"},
		Simple: map[string]Response{"/": {Status: http.StatusOK, Body: "ok"}},
	}}})
	runtime.HistoryCapture = &HistoryCapture{
		BodyMaxBytes:   8,
		HeaderMaxBytes: 128,
		Decide: func(int, string, string) event.HistoryDecision {
			return event.HistoryDecision{}
		},
	}
	historyCalls := 0
	runtime.RecordHistory = func(event.Event) { historyCalls++ }
	var observed telemetry.Event
	runtime.Record = func(value telemetry.Event) { observed = value }
	request := httptest.NewRequest(http.MethodGet, "http://sampled-history.test/", nil)
	request.Header.Set("X-Large-Metadata", strings.Repeat("x", 64))
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "ok" {
		t.Fatalf("response=%d/%q", response.Code, response.Body.String())
	}
	if historyCalls != 0 {
		t.Fatalf("history calls=%d, want zero", historyCalls)
	}
	if observed.RequestHeaders != nil || observed.ResponseHeaders != nil {
		t.Fatalf("metadata was copied for rejected decision: %#v", observed)
	}
}

func TestHistoryDecisionUsesMatchedSitePolicy(t *testing.T) {
	runtime := New(&Snapshot{Sites: []Site{
		{Hosts: []string{"all-history.test"}, RequestHistoryMode: "all", Simple: map[string]Response{"/": {Status: http.StatusOK, Body: "ok"}}},
		{Hosts: []string{"error-history.test"}, RequestHistoryMode: "errorsOnly", Simple: map[string]Response{"/": {Status: http.StatusOK, Body: "ok"}}},
	}})
	runtime.HistoryCapture = &HistoryCapture{DecideEvent: (telemetry.HistoryPolicy{}).Decide}
	var events []telemetry.Event
	runtime.RecordHistory = func(value telemetry.Event) { events = append(events, value) }
	for _, host := range []string{"all-history.test", "error-history.test"} {
		response := httptest.NewRecorder()
		runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://"+host+"/", nil))
	}
	if len(events) != 1 || events[0].HistoryMode != "all" {
		t.Fatalf("site history events=%#v", events)
	}
}

func TestRouteAndTransformApplyBeforeUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"path": r.URL.RequestURI(), "header": r.Header.Get("X-Test"), "host": r.Host})
		w.Header().Set("X-Upstream", "yes")
	}))
	defer upstream.Close()
	r := New(&Snapshot{Sites: []Site{{
		Hosts: []string{"example.test"}, Destination: upstream.URL,
		Transforms: Transform{PathPrefix: "/prefix", QueryValueParameter: "mode", Set: "set", RequestHeader: "X-Test"},
		Rules:      []Rule{{Enabled: true, PathPattern: "child", Destination: upstream.URL, RemovePrefix: "/child"}},
	}}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.test/source?old=one", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("site transform status=%d", w.Code)
	}
	var siteOut map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &siteOut); err != nil {
		t.Fatal(err)
	}
	if siteOut["path"] != "/prefix/source?old=one&mode=set" || siteOut["header"] != "set" {
		t.Fatalf("site transform: %#v", siteOut)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.test/child/source", nil))
	var childOut map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &childOut); err != nil {
		t.Fatal(err)
	}
	if childOut["path"] != "/source" || childOut["header"] != "" {
		t.Fatalf("child rule: %#v", childOut)
	}
}

func TestRouteTransportOptionsAreValidatedAndScoped(t *testing.T) {
	r := New(nil)
	if _, err := r.transportFor("://bad", false, 0); err == nil {
		t.Fatal("invalid forward proxy accepted")
	}
	transport, err := r.transportFor("http://127.0.0.1:8080", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	configured, ok := transport.(*http.Transport)
	if !ok || configured.Proxy == nil || configured.TLSClientConfig == nil || !configured.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("unexpected transport: %#v", transport)
	}
	if r.transport.TLSClientConfig != nil && r.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("unsafe TLS leaked into default transport")
	}
}

func TestRuntimeUsesPerRouteForwardProxyAndUnsafeTLS(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(w, "upstream:"+request.URL.RequestURI())
	}))
	defer upstream.Close()
	usedProxy := false
	forwardProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		usedProxy = true
		out := request.Clone(request.Context())
		out.RequestURI = ""
		response, err := http.DefaultTransport.RoundTrip(out)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		for name, values := range response.Header {
			w.Header()[name] = append([]string(nil), values...)
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	defer forwardProxy.Close()
	runtime := New(&Snapshot{Sites: []Site{{Hosts: []string{"proxy.test"}, Destination: upstream.URL, WebProxy: forwardProxy.URL}}})
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://proxy.test/forward", nil))
	if response.Code != http.StatusOK || response.Body.String() != "upstream:/forward" || !usedProxy {
		t.Fatalf("forward proxy response=%d/%q used=%t", response.Code, response.Body.String(), usedProxy)
	}

	unsafeUpstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(w, "unsafe:"+request.URL.Path)
	}))
	defer unsafeUpstream.Close()
	runtime = New(&Snapshot{Sites: []Site{{Hosts: []string{"unsafe.test"}, Destination: unsafeUpstream.URL, AllowUnsafeTLS: true}}})
	response = httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://unsafe.test/tls", nil))
	if response.Code != http.StatusOK || response.Body.String() != "unsafe:/tls" {
		t.Fatalf("unsafe TLS response=%d/%q", response.Code, response.Body.String())
	}
}

func TestCORSPreflightDoesNotCallUpstream(t *testing.T) {
	r := New(&Snapshot{Sites: []Site{{Hosts: []string{"example.test"}}}})
	request := httptest.NewRequest(http.MethodOptions, "http://example.test/cors", nil)
	request.Header.Set("Origin", "https://console.example.test")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Access-Control-Allow-Origin") != "https://console.example.test" || response.Header().Get("Access-Control-Allow-Methods") != http.MethodPost {
		t.Fatalf("status=%d headers=%v", response.Code, response.Header())
	}
}

func TestLegacyStringBooleanTransformsAndForwardedHeader(t *testing.T) {
	var transform Transform
	if err := json.Unmarshal([]byte(`{"RequestHeadersCopy":"false","ResponseHeadersCopy":"false","ResponseTrailersCopy":"false","Forwarded":"for,proto,host","Action":"Set"}`), &transform); err != nil {
		t.Fatal(err)
	}
	if !transform.RequestHeadersCopy.Set || transform.RequestHeadersCopy.Value || !transform.ResponseHeadersCopy.Set || transform.ResponseHeadersCopy.Value {
		t.Fatalf("transform=%#v", transform)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Header.Get("Forwarded") + "|" + r.Header.Get("X-Forwarded-For")))
	}))
	defer upstream.Close()
	r := New(&Snapshot{Sites: []Site{{Hosts: []string{"example.test"}, Destination: upstream.URL, Transforms: transform}}})
	request := httptest.NewRequest(http.MethodGet, "http://example.test/forwarded", nil)
	request.RemoteAddr = "192.0.2.8:1234"
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != `proto=http;host="example.test";for=192.0.2.8|` {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestOptionalBoolSnapshotRoundTripUsesScalarContract(t *testing.T) {
	snapshot := Snapshot{Sites: []Site{{Transforms: Transform{RequestHeadersCopy: OptionalBool{Set: true, Value: false}}}}}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `RequestHeadersCopy":{`) || !strings.Contains(string(raw), `RequestHeadersCopy":false`) {
		t.Fatalf("snapshot did not use scalar transform contract: %s", raw)
	}
	var restored Snapshot
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	value := restored.Sites[0].Transforms.RequestHeadersCopy
	if !value.Set || value.Value {
		t.Fatalf("round trip value=%#v", value)
	}
}

func TestResponseTrailerTransform(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Trailer", "X-Upstream-Trailer")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("body"))
		w.Header().Set("X-Upstream-Trailer", "upstream")
	}))
	defer upstream.Close()
	transform := Transform{ResponseTrailer: "X-Added-Trailer", Set: "added"}
	r := New(&Snapshot{Sites: []Site{{Hosts: []string{"example.test"}, Destination: upstream.URL, Transforms: transform}}})
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/trailers", nil))
	result := response.Result()
	defer result.Body.Close()
	if result.Trailer.Get("X-Upstream-Trailer") != "upstream" || result.Trailer.Get("X-Added-Trailer") != "added" {
		t.Fatalf("trailers=%v", result.Trailer)
	}
}

func TestResponseTrailerCopyCanBeDisabled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Trailer", "X-Upstream-Trailer")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("body"))
		w.Header().Set("X-Upstream-Trailer", "upstream")
	}))
	defer upstream.Close()
	r := New(&Snapshot{Sites: []Site{{Hosts: []string{"example.test"}, Destination: upstream.URL, Transforms: Transform{ResponseTrailersCopy: OptionalBool{Set: true, Value: false}}}}})
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/trailers", nil))
	if result := response.Result(); result.Trailer.Get("X-Upstream-Trailer") != "" {
		t.Fatalf("trailers=%v", result.Trailer)
	}
}

func TestResponseHeadersCanBeDisabled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Upstream", "value")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("body"))
	}))
	defer upstream.Close()
	r := New(&Snapshot{Sites: []Site{{Hosts: []string{"example.test"}, Destination: upstream.URL, Transforms: Transform{ResponseHeadersCopy: OptionalBool{Set: true, Value: false}}}}})
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.test/headers", nil))
	if response.Header().Get("X-Upstream") != "" || response.Header().Get("Content-Type") != "" {
		t.Fatalf("headers=%v", response.Header())
	}
}

// These tests use real TCP listeners rather than invoking ServeHTTP directly.
// They exercise the ReverseProxy paths that are easy to accidentally break by
// wrapping ResponseWriter: long request/response streams, 100-continue and
// websocket hijacking.
func TestRuntimeE2EStreamsLargeUploadSSEAndContinue(t *testing.T) {
	firstEvent := make(chan struct{})
	allowSecondEvent := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/upload":
			body, err := io.ReadAll(request.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("X-Received-Bytes", strconv.Itoa(len(body)))
			_, _ = w.Write([]byte("uploaded"))
		case "/events":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			_, _ = io.WriteString(w, "data: first\n\n")
			flusher.Flush()
			close(firstEvent)
			<-allowSecondEvent
			_, _ = io.WriteString(w, "data: second\n\n")
			flusher.Flush()
		default:
			http.NotFound(w, request)
		}
	}))
	defer upstream.Close()
	proxyServer := httptest.NewServer(New(&Snapshot{Sites: []Site{{Hosts: []string{"fixture.test"}, Destination: upstream.URL}}}))
	defer proxyServer.Close()

	large := bytes.Repeat([]byte("yaginx-stream-"), 256*1024) // >3 MiB
	upload, err := http.NewRequest(http.MethodPost, proxyServer.URL+"/upload", bytes.NewReader(large))
	if err != nil {
		t.Fatal(err)
	}
	upload.Host = "fixture.test"
	upload.Header.Set("Expect", "100-continue")
	result, err := (&http.Client{Timeout: 10 * time.Second}).Do(upload)
	if err != nil {
		t.Fatalf("large expect-continue upload: %v", err)
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK || result.Header.Get("X-Received-Bytes") != strconv.Itoa(len(large)) {
		t.Fatalf("upload status=%d bytes=%q", result.StatusCode, result.Header.Get("X-Received-Bytes"))
	}

	events, err := http.NewRequest(http.MethodGet, proxyServer.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	events.Host = "fixture.test"
	stream, err := (&http.Client{Timeout: 10 * time.Second}).Do(events)
	if err != nil {
		t.Fatalf("sse request: %v", err)
	}
	defer stream.Body.Close()
	select {
	case <-firstEvent:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not flush the first SSE event")
	}
	reader := bufio.NewReader(stream.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "data: first\n" {
		t.Fatalf("first SSE event line=%q err=%v", line, err)
	}
	close(allowSecondEvent)
	remaining, err := io.ReadAll(reader)
	if err != nil || !strings.Contains(string(remaining), "data: second\n\n") {
		t.Fatalf("second SSE event=%q err=%v", remaining, err)
	}
}

func TestRuntimeE2EWebSocketUpgradeAndFrame(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !strings.EqualFold(request.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "upgrade required", http.StatusBadRequest)
			return
		}
		connection, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		key := request.Header.Get("Sec-WebSocket-Key")
		digest := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		_, _ = io.WriteString(buffer, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: "+base64.StdEncoding.EncodeToString(digest[:])+"\r\nSec-WebSocket-Protocol: fixture-protocol\r\n\r\n")
		if err := buffer.Flush(); err != nil {
			return
		}
		// A masked client text frame with a short payload. Echo a server frame.
		header := make([]byte, 2)
		if _, err := io.ReadFull(buffer, header); err != nil || header[0] != 0x81 || header[1]&0x80 == 0 {
			return
		}
		payloadLen := int(header[1] & 0x7f)
		mask := make([]byte, 4)
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(buffer, mask); err != nil {
			return
		}
		if _, err := io.ReadFull(buffer, payload); err != nil {
			return
		}
		for i := range payload {
			payload[i] ^= mask[i%len(mask)]
		}
		_, _ = buffer.Write([]byte{0x81, byte(len(payload))})
		_, _ = buffer.Write(payload)
		_ = buffer.Flush()
	}))
	defer upstream.Close()
	proxyServer := httptest.NewServer(New(&Snapshot{Sites: []Site{{Hosts: []string{"fixture.test"}, Destination: upstream.URL}}}))
	defer proxyServer.Close()

	address := strings.TrimPrefix(proxyServer.URL, "http://")
	connection, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	key := base64.StdEncoding.EncodeToString([]byte("fixture-websocket"))
	_, _ = io.WriteString(connection, "GET /websocket HTTP/1.1\r\nHost: fixture.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: "+key+"\r\nSec-WebSocket-Protocol: fixture-protocol\r\n\r\n")
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("websocket handshake: %v", err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols || response.Header.Get("Sec-Websocket-Protocol") != "fixture-protocol" {
		t.Fatalf("websocket response=%s headers=%v", response.Status, response.Header)
	}
	payload := []byte("fixture-websocket")
	mask := []byte{1, 2, 3, 4}
	frame := append([]byte{0x81, 0x80 | byte(len(payload))}, mask...)
	for index, value := range payload {
		frame = append(frame, value^mask[index%len(mask)])
	}
	if _, err := connection.Write(frame); err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil || header[0] != 0x81 || int(header[1]) != len(payload) {
		t.Fatalf("websocket frame header=%v err=%v", header, err)
	}
	echo := make([]byte, len(payload))
	if _, err := io.ReadFull(reader, echo); err != nil || string(echo) != string(payload) {
		t.Fatalf("websocket echo=%q err=%v", echo, err)
	}
}

func TestPruneDerivedTransportsKeepsRecentlyUsed(t *testing.T) {
	r := New(nil)
	stale, err := r.transportFor("", false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fresh, _ := r.transportFor("", false, 2*time.Minute)
	r.derivedMu.Lock()
	r.derived[transportKey{responseHeaderTimeout: time.Minute}].lastUsed = time.Now().Add(-2 * derivedTransportIdleTTL)
	r.derivedMu.Unlock()

	r.Replace(&Snapshot{})

	if again, _ := r.transportFor("", false, 2*time.Minute); again != fresh {
		t.Fatal("recently used transport must survive a snapshot reload")
	}
	if rebuilt, _ := r.transportFor("", false, time.Minute); rebuilt == stale {
		t.Fatal("idle transport should have been pruned and rebuilt")
	}
}
