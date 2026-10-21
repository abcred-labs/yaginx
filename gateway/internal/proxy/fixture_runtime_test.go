package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"yaginx-gateway/internal/telemetry"
	"yaginx-gateway/internal/testfixtures"
)

// TestRuntimeExecutesFixtures turns the proxy HTTP fixtures into executable
// data-plane checks. Transport-specific and deliberately approved Go-only
// differences stay in proxyFixtureSpecializations; each name names the test
// suite that owns it so fixture coverage cannot quietly become metadata-only.
func TestRuntimeExecutesFixtures(t *testing.T) {
	fixtures, err := testfixtures.Proxy()
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			if owner, specialized := proxyFixtureSpecializations[fixture.Name]; specialized {
				t.Skipf("covered by %s", owner)
			}
			runRuntimeFixture(t, fixture)
		})
	}
}

// These cases need a raw connection, TLS/QUIC listener, management router, or
// a fault injector that cannot be represented by a one-request httptest
// upstream. They are intentionally listed rather than silently omitted.
var proxyFixtureSpecializations = map[string]string{
	"acme-challenge-missing-falls-through":                "TestACMEChallengeFallthrough",
	"approved-9443-no-http3":                              "listeners.TestStartServesHTTP3OnlyOnConfiguredUDPAddress",
	"approved-client-cancel":                              "TestRuntimeClassifiesCanceledRequestContext",
	"approved-config-conflict":                            "repository snapshot validation",
	"approved-host-normalization":                         "repository snapshot validation",
	"approved-management-listener":                        "internal/app management listener integration",
	"approved-path-pattern-missing-route-value":           "repository.TestValidateSnapshotTransformsRejectsUnresolvedRouteValues",
	"approved-query-route-missing-route-value":            "repository.TestValidateSnapshotTransformsRejectsUnresolvedRouteValues",
	"approved-reload-lkg":                                 "internal/app offline LKG integration",
	"approved-status-601":                                 "TestRuntimeClassifiesCanceledRequestContext",
	"approved-status-602":                                 "TestRuntimeClassifiesUpstreamResponseHeaderTimeout",
	"approved-status-603":                                 "TestTelemetryClassifiesProxyHistoryWithoutChangingHTTPStatus",
	"approved-status-607":                                 "TestRuntimeClassifiesRequestBodyReadFailure",
	"approved-status-608":                                 "TestResponseCaptureClassifiesClientWriteFailure",
	"approved-status-609":                                 "TestRuntimeClassifiesPipelineExceptionAndInvalidTransport",
	"approved-status-610":                                 "TestRuntimeClassifiesUnknownRoundTripError",
	"authorization-policy-empty-is-anonymous":             "management authorization integration",
	"child-rule-explicit-forward-proxy":                   "TestRuntimeUsesPerRouteForwardProxyAndUnsafeTLS",
	"child-rule-unsafe-upstream-tls":                      "TestRuntimeUsesPerRouteForwardProxyAndUnsafeTLS",
	"default-route-explicit-forward-proxy":                "TestRuntimeUsesPerRouteForwardProxyAndUnsafeTLS",
	"default-route-unsafe-upstream-tls":                   "TestRuntimeUsesPerRouteForwardProxyAndUnsafeTLS",
	"listener-http-default-route":                         "listeners.Start HTTP socket integration",
	"listener-https-tcp-default-route":                    "listeners.TestStartAcceptsProxyV1V2AndTLS",
	"listener-proxy-http-v1-default-route":                "listeners.TestStartAcceptsProxyV1V2AndTLS",
	"listener-proxy-http-v2-default-route":                "listeners.TestStartAcceptsProxyV1V2AndTLS",
	"listener-proxy-https-v1-default-route":               "listeners.TestStartAcceptsProxyV1V2AndTLS",
	"management-api-before-spa-fallback":                  "internal/app management integration",
	"management-spa-root-index":                           "internal/app admin UI integration",
	"next-scoped-package-encoding":                        "Next scoped-asset integration",
	"streaming-chunked-request-body":                      "TestRuntimeE2EStreamsLargeUploadSSEAndContinue",
	"streaming-declared-trailer-passthrough":              "TestResponseTrailerTransform",
	"streaming-large-download":                            "TestRuntimeE2EStreamsLargeUploadSSEAndContinue",
	"streaming-large-upload":                              "TestRuntimeE2EStreamsLargeUploadSSEAndContinue",
	"streaming-sse-flushes-event":                         "TestRuntimeE2EStreamsLargeUploadSSEAndContinue",
	"streaming-websocket-upgrade-subprotocol-and-frame":   "TestRuntimeE2EWebSocketUpgradeAndFrame",
	"transform-path-pattern-missing-route-value":          "repository.TestValidateSnapshotTransformsRejectsUnresolvedRouteValues",
	"transform-query-route-parameter-missing-route-value": "repository.TestValidateSnapshotTransformsRejectsUnresolvedRouteValues",
	"upstream-request-refused-classification":             "TestTelemetryClassifiesProxyHistoryWithoutChangingHTTPStatus",
	"upstream-response-body-failure":                      "TestRuntimeClassifiesRawUpstreamHeaderAndBodyFailures",
	"upstream-response-header-failure":                    "TestRuntimeClassifiesRawUpstreamHeaderAndBodyFailures",
}

type fixtureUpstreamObservation struct {
	method  string
	host    string
	path    string
	headers http.Header
}

func runRuntimeFixture(t *testing.T, fixture testfixtures.ProxyFixture) {
	t.Helper()
	var observed []fixtureUpstreamObservation
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		_ = body
		observed = append(observed, fixtureUpstreamObservation{method: request.Method, host: request.Host, path: request.URL.RequestURI(), headers: request.Header.Clone()})
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Only", "present")
		if strings.Contains(fixture.Name, "trailer") {
			w.Header().Add("Trailer", "X-Fixture-Trailer")
		}
		status := fixture.Upstream.Status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write(fixtureBody(fixture.Client.Body))
		if strings.Contains(fixture.Name, "trailer") {
			w.Header().Set("X-Fixture-Trailer", "upstream")
		}
	}))
	defer upstream.Close()

	site, matched := fixtureSite(t, fixture, upstream.URL)
	snapshot := &Snapshot{}
	if matched {
		snapshot.Sites = []Site{site}
	}
	runtime := New(snapshot)
	var event telemetry.Event
	runtime.Record = func(value telemetry.Event) { event = value }

	request := httptest.NewRequest(fixture.Client.Method, "http://"+fixture.Client.Host+fixture.Client.Path, nil)
	request.RemoteAddr = "127.0.0.1:12345"
	configureFixtureRequest(request, fixture)
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, request)

	if response.Code != fixture.Client.Status {
		t.Fatalf("client status=%d want=%d", response.Code, fixture.Client.Status)
	}
	assertFixtureHeaders(t, response.Header(), fixture.Client.Headers)
	assertFixtureBody(t, response.Body.Bytes(), fixture.Client.Body)
	if event.HistoryStatus != fixture.History.Status || event.StatusSource != fixture.History.StatusSource || event.ErrorCategory != fixture.History.ErrorCategory {
		t.Fatalf("history=%#v want status=%d source=%q category=%q", event, fixture.History.Status, fixture.History.StatusSource, fixture.History.ErrorCategory)
	}
	if fixture.Upstream.Attempted {
		if len(observed) != 1 {
			t.Fatalf("upstream calls=%d want=1", len(observed))
		}
		got := observed[0]
		if got.method != fixture.Upstream.Method || got.path != fixture.Upstream.Path {
			t.Fatalf("upstream method/path=%s %s want=%s %s", got.method, got.path, fixture.Upstream.Method, fixture.Upstream.Path)
		}
		wantHost := fixture.Upstream.Host
		if isFixtureLoopbackHost(wantHost) {
			wantHost = strings.TrimPrefix(upstream.URL, "http://")
		}
		if got.host != wantHost {
			t.Fatalf("upstream host=%q want=%q", got.host, wantHost)
		}
		assertFixtureHeaders(t, got.headers, fixture.Upstream.Headers)
	} else if len(observed) != 0 {
		t.Fatalf("unexpected upstream calls=%d", len(observed))
	}
}

func fixtureSite(t *testing.T, fixture testfixtures.ProxyFixture, upstreamURL string) (Site, bool) {
	t.Helper()
	var config struct {
		DefaultHost            string            `json:"defaultHost"`
		DefaultDestinationHost string            `json:"defaultDestinationHost"`
		Hosts                  []string          `json:"hosts"`
		RedirectHTTPS          bool              `json:"redirectHttpToHttps"`
		MaintenanceMode        bool              `json:"isMaintenanceMode"`
		OriginalHost           bool              `json:"preserveOriginalHost"`
		Transforms             json.RawMessage   `json:"proxyTransforms"`
		Rules                  []json.RawMessage `json:"proxyRules"`
		Simple                 []json.RawMessage `json:"simpleResponses"`
	}
	if err := json.Unmarshal(fixture.Config, &config); err != nil {
		t.Fatal(err)
	}
	if config.DefaultHost == "" {
		return Site{}, false
	}
	hosts := append([]string{config.DefaultHost}, config.Hosts...)
	site := Site{Hosts: hosts, RedirectHost: config.DefaultHost, Destination: upstreamURL, DestinationHost: config.DefaultDestinationHost, RedirectHTTPS: config.RedirectHTTPS, MaintenanceMode: config.MaintenanceMode, OriginalHost: config.OriginalHost, Simple: map[string]Response{}}
	if len(config.Transforms) != 0 && string(config.Transforms) != "null" {
		if err := json.Unmarshal(config.Transforms, &site.Transforms); err != nil {
			t.Fatalf("transforms: %v", err)
		}
	}
	for _, raw := range config.Rules {
		var value struct {
			Enabled         bool   `json:"isEnabled"`
			PathPattern     string `json:"pathPattern"`
			DestinationHost string `json:"destinationHost"`
			RemovePrefix    string `json:"removePrefix"`
			OriginalHost    *bool  `json:"preserveOriginalHost"`
		}
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatalf("rule: %v", err)
		}
		site.Rules = append(site.Rules, Rule{Enabled: value.Enabled, PathPattern: value.PathPattern, Destination: upstreamURL, DestinationHost: value.DestinationHost, RemovePrefix: value.RemovePrefix, OriginalHost: value.OriginalHost})
	}
	for _, raw := range config.Simple {
		var value struct {
			URL             string `json:"path"`
			StatusCode      int    `json:"statusCode"`
			ContentType     string `json:"contentType"`
			ResponseContent string `json:"responseContent"`
			ExtraHeaders    []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"extraHeaders"`
		}
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatalf("simple response: %v", err)
		}
		if value.StatusCode == 0 {
			value.StatusCode = fixture.Client.Status
		}
		if value.ContentType == "" {
			value.ContentType = fixture.Client.Headers["Content-Type"]
		}
		if value.ResponseContent == "" {
			value.ResponseContent = string(fixtureBody(fixture.Client.Body))
		}
		headers := make(map[string]string, len(value.ExtraHeaders))
		for _, header := range value.ExtraHeaders {
			headers[header.Key] = header.Value
		}
		for name, value := range fixture.Client.Headers {
			if name != "Content-Type" && name != "Date" && name != "Vary" && !strings.HasPrefix(value, "${") {
				if _, exists := headers[name]; !exists {
					headers[name] = value
				}
			}
		}
		site.Simple[value.URL] = Response{Status: value.StatusCode, ContentType: value.ContentType, Body: value.ResponseContent, Headers: headers}
	}
	return site, true
}

func configureFixtureRequest(request *http.Request, fixture testfixtures.ProxyFixture) {
	if origin := fixture.Client.Headers["Access-Control-Allow-Origin"]; origin != "" {
		request.Header.Set("Origin", origin)
	}
	if fixture.Client.Method == http.MethodOptions {
		request.Header.Set("Access-Control-Request-Method", fixture.Client.Headers["Access-Control-Allow-Methods"])
		request.Header.Set("Access-Control-Request-Headers", fixture.Client.Headers["Access-Control-Allow-Headers"])
		return
	}
	if methods := fixture.Client.Headers["Access-Control-Allow-Methods"]; methods != "" {
		request.Header.Set("Access-Control-Request-Method", methods)
	}
	if headers := fixture.Client.Headers["Access-Control-Allow-Headers"]; headers != "" {
		request.Header.Set("Access-Control-Request-Headers", headers)
	}
}

func fixtureBody(raw json.RawMessage) []byte {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []byte(text)
	}
	var compact bytes.Buffer
	if json.Compact(&compact, raw) == nil {
		return compact.Bytes()
	}
	return raw
}

func assertFixtureBody(t *testing.T, got []byte, raw json.RawMessage) {
	t.Helper()
	want := fixtureBody(raw)
	if json.Valid(want) && json.Valid(got) {
		var expected, actual any
		_ = json.Unmarshal(want, &expected)
		_ = json.Unmarshal(got, &actual)
		if fmt.Sprintf("%#v", actual) == fmt.Sprintf("%#v", expected) {
			return
		}
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("client body=%q want=%q", got, want)
	}
}

func assertFixtureHeaders(t *testing.T, got http.Header, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if name == "Date" || name == "Vary" || strings.HasPrefix(value, "${") {
			continue
		}
		if actual := got.Get(name); actual != value {
			t.Fatalf("header %s=%q want=%q all=%v", name, actual, value, got)
		}
	}
}

func isFixtureLoopbackHost(value string) bool {
	parsed, err := url.Parse("http://" + value)
	return err == nil && parsed.Hostname() == "127.0.0.1"
}
