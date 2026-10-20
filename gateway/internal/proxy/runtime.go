// Package proxy provides the fail-closed, hot-swappable HTTP proxy runtime.
package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"yaginx-gateway/internal/platform/hostnorm"
	"yaginx-gateway/internal/proxy/event"
)

type Site struct {
	// ID is the stable website primary key used only by config-event snapshot
	// reconciliation. It is not part of request routing.
	ID           int64
	Hosts        []string
	HostBindings []HostBinding
	// RedirectHost is the normalized website defaultHost used for HTTP to
	// HTTPS redirects. It is deliberately distinct from Hosts: aliases route
	// to the site but must redirect to its primary hostname.
	RedirectHost                      string
	Destination                       string
	DestinationHost                   string
	RedirectHTTPS                     bool
	MaintenanceMode                   bool
	IPAccessMode                      string
	IPAccessCIDRs                     []string
	OriginalHost                      bool
	WebProxy                          string
	AllowUnsafeTLS                    bool
	Simple                            map[string]Response
	Transforms                        Transform
	Rules                             []Rule
	UpstreamPools                     map[string]UpstreamPool
	RequestHistoryMode                string
	RequestHistorySuccessSamplingRate *float64
	// ResponseHeaderTimeout overrides proxy.responseHeaderTimeout for this site.
	// Zero inherits the global runtime setting.
	ResponseHeaderTimeout time.Duration
}

type UpstreamPool struct {
	Strategy  string             `json:"strategy"`
	Endpoints []UpstreamEndpoint `json:"endpoints"`
}
type UpstreamEndpoint struct {
	Address   string `json:"address"`
	Host      string `json:"host"`
	Weight    int    `json:"weight"`
	Enabled   bool   `json:"enabled"`
	TLSVerify *bool  `json:"tlsVerify"`
	Proxy     string `json:"proxy"`
	Priority  int    `json:"priority"`
}

// HostBinding describes how a hostname enters a site. Hosts remains populated
// for legacy snapshots and callers; bindings take precedence when present.
type HostBinding struct {
	Hostname           string `json:"hostname"`
	Enabled            bool   `json:"enabled"`
	Role               string `json:"role"`
	AccessMode         string `json:"accessMode"`
	RedirectStatusCode int    `json:"redirectStatusCode"`
	RedirectTargetHost string `json:"redirectTargetHost"`
}

// Transform is the safe, currently supported subset of the legacy route
// transform contract. Child rules deliberately do not inherit site transforms.
type Transform struct {
	PathPrefix, PathSet, PathRemovePrefix, QueryValueParameter, Set string
	RequestHeader, ResponseHeader, ResponseTrailer                  string
	Forwarded, XForwarded, For, Host, Proto, Prefix, Action         string
	RequestHeadersCopy, ResponseHeadersCopy, ResponseTrailersCopy   OptionalBool
}

// OptionalBool accepts the legacy serialized form ("false") as well as a
// JSON boolean, while retaining whether a transform was explicitly present.
type OptionalBool struct {
	Set   bool
	Value bool
}

func (o *OptionalBool) UnmarshalJSON(raw []byte) error {
	if strings.TrimSpace(string(raw)) == "null" {
		o.Set, o.Value = false, false
		return nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err == nil {
		o.Set, o.Value = true, value
		return nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return err
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(text))
	if err != nil {
		return err
	}
	o.Set, o.Value = true, parsed
	return nil
}

// MarshalJSON keeps LKG snapshots compatible with the configured
// transform shape. Encoding the Go bookkeeping fields (Set/Value) made a
// snapshot impossible to reload because UnmarshalJSON correctly accepts only
// the legacy scalar bool/string forms.
func (o OptionalBool) MarshalJSON() ([]byte, error) {
	if !o.Set {
		return []byte("null"), nil
	}
	return json.Marshal(o.Value)
}

type Rule struct {
	Enabled                                                 bool
	PathPattern, Destination, DestinationHost, RemovePrefix string
	OriginalHost                                            *bool
	WebProxy                                                string
	AllowUnsafeTLS                                          bool
	UpstreamPoolID                                          string
	IsFallback                                              bool
	AllowUnsafeTLSSet                                       bool
	WebProxySet                                             bool
}
type Response struct {
	Status      int
	ContentType string
	Body        string
	Headers     map[string]string
}
type Snapshot struct {
	Revision  int64
	Sites     []Site
	Blacklist []BlacklistRule
}

// BlacklistRule is a database-managed CIDR rule. CIDR remains text here so
// last-known-good snapshots stay portable.
type BlacklistRule struct {
	ID   int64
	CIDR string
}

// Settings contains process-wide data-plane limits. Route-specific behavior
// remains part of Snapshot so a configuration reload never mutates a request
// already in flight.
type Settings struct {
	ActivityTimeout           time.Duration
	ResponseHeaderTimeout     time.Duration
	MaxConcurrentRequests     int
	MaxRequestHeaderBytes     int
	MaxRequestBodyBytes       int64
	MaxIdleConnections        int
	MaxIdleConnectionsPerHost int
	IdleConnectionTimeout     time.Duration
	ExpectContinueTimeout     time.Duration
	UpstreamExpectContinue    ExpectContinuePolicy
	IPBlacklistCIDRs          []string
	BlacklistCandidateWarning func(candidate, remote, host string)
}

// ExpectContinuePolicy controls the legacy selective upstream Expect header.
// It never removes a client-supplied Expect header.
type ExpectContinuePolicy struct {
	Enabled               bool
	MinContentLengthBytes int64
	IncludedHosts         []string
	IncludedPathPrefixes  []string
}

func DefaultSettings() Settings {
	return Settings{
		ActivityTimeout:           5 * time.Minute,
		ResponseHeaderTimeout:     30 * time.Second,
		MaxConcurrentRequests:     256,
		MaxRequestHeaderBytes:     65536,
		MaxRequestBodyBytes:       0,
		MaxIdleConnections:        128,
		MaxIdleConnectionsPerHost: 16,
		IdleConnectionTimeout:     90 * time.Second,
		ExpectContinueTimeout:     time.Second,
		UpstreamExpectContinue: ExpectContinuePolicy{
			Enabled:               true,
			MinContentLengthBytes: 1024 * 1024,
			IncludedHosts:         []string{"ai.abc.red"},
			IncludedPathPrefixes:  []string{"/v1/images/edits", "/api/image-gen", "/api/chat/images/edits"},
		},
	}
}

type Runtime struct {
	snapshot          atomic.Pointer[Snapshot]
	transport         *http.Transport
	transportFactory  func(proxyURL string, allowUnsafeTLS bool) (http.RoundTripper, error)
	derivedMu         sync.Mutex
	derived           map[transportKey]*derivedTransport
	ACMEChallenge     func(string) (string, error)
	Record            func(event.Event)
	RecordAggregation func(event.AggregationEvent)
	RecordHistory     func(event.Event)
	ShouldAggregate   func(event.Event) bool
	TraceID           func(context.Context) string
	InternalPanic     func(error)
	HistoryCapture    *HistoryCapture
	expectPolicy      ExpectContinuePolicy
	blacklist         []netip.Prefix
	blacklistWarning  func(candidate, remote, host string)
	upstreamMu        sync.Mutex
	upstreamCursor    map[string]int
	upstreamActive    map[string][]int
}

// HistoryCapture controls the bounded metadata copied into an asynchronous
// request-history event. It never changes the request or response stream;
// the capture wrappers only mirror bytes already read or written by the proxy.
type HistoryCapture struct {
	BodyMaxBytes, HeaderMaxBytes int
	BodyContentTypes             []string
	Decide                       func(status int, statusSource, errorCategory string) event.HistoryDecision
	DecideEvent                  func(event.Event) event.HistoryDecision
	ShouldCaptureMetadata        func(status int, statusSource, errorCategory string) bool
}

func New(initial *Snapshot) *Runtime {
	r, err := NewConfigured(initial, DefaultSettings())
	if err != nil {
		panic(err)
	}
	return r
}

// NewConfigured validates immutable process settings before accepting traffic.
// Runtime configuration has already been schema-validated, but this boundary
// also protects callers such as tests and future embedding applications.
func NewConfigured(initial *Snapshot, settings Settings) (*Runtime, error) {
	if settings.MaxConcurrentRequests < 1 {
		settings.MaxConcurrentRequests = 256
	}
	if settings.MaxRequestBodyBytes < 0 {
		return nil, fmt.Errorf("invalid proxy request body limit")
	}
	if settings.ActivityTimeout <= 0 || settings.ResponseHeaderTimeout <= 0 || settings.IdleConnectionTimeout <= 0 || settings.ExpectContinueTimeout <= 0 || settings.MaxRequestHeaderBytes < 8192 || settings.MaxIdleConnections < 1 || settings.MaxIdleConnectionsPerHost < 1 {
		return nil, fmt.Errorf("invalid proxy transport settings")
	}
	blacklist := make([]netip.Prefix, 0, len(settings.IPBlacklistCIDRs))
	for _, raw := range settings.IPBlacklistCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("invalid IP blacklist CIDR %q: %w", raw, err)
		}
		blacklist = append(blacklist, prefix.Masked())
	}
	r := &Runtime{
		transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DisableCompression:    true,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          settings.MaxIdleConnections,
			MaxIdleConnsPerHost:   settings.MaxIdleConnectionsPerHost,
			IdleConnTimeout:       settings.IdleConnectionTimeout,
			ExpectContinueTimeout: settings.ExpectContinueTimeout,
			ResponseHeaderTimeout: settings.ResponseHeaderTimeout,
		},
		expectPolicy:     normalizeExpectPolicy(settings.UpstreamExpectContinue),
		blacklist:        blacklist,
		blacklistWarning: settings.BlacklistCandidateWarning,
		upstreamCursor:   make(map[string]int),
		upstreamActive:   make(map[string][]int),
	}
	if initial == nil {
		initial = &Snapshot{}
	}
	r.snapshot.Store(initial)
	return r, nil
}
func (r *Runtime) Replace(next *Snapshot) {
	if next != nil {
		r.snapshot.Store(next)
		r.pruneDerivedTransports()
	}
}

// derivedTransportIdleTTL is how long an unused derived transport is kept.
const derivedTransportIdleTTL = 10 * time.Minute

type derivedTransport struct {
	transport *http.Transport
	lastUsed  time.Time
}

// pruneDerivedTransports closes and drops derived transports not used recently,
// so edited or deleted sites do not keep idle upstream connections alive while
// active ones keep their pools across snapshot reloads. In-flight requests
// finish on the old transport.
func (r *Runtime) pruneDerivedTransports() {
	cutoff := time.Now().Add(-derivedTransportIdleTTL)
	r.derivedMu.Lock()
	defer r.derivedMu.Unlock()
	for key, entry := range r.derived {
		if entry.lastUsed.Before(cutoff) {
			entry.transport.CloseIdleConnections()
			delete(r.derived, key)
		}
	}
}
func (r *Runtime) Snapshot() *Snapshot { return r.snapshot.Load() }
func (r *Runtime) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	started := time.Now()
	domain := "__unmatched__"
	// Keep the entry site's policy with the request, including early exits and
	// requests which finish after a configuration reload.
	site, matched, binding := r.match(req.Host)
	captured := &responseCapture{ResponseWriter: w, clientIP: clientHost(req.RemoteAddr)}
	captured.started = started
	if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		captured.responseMode = "upgrade"
	}
	var requestCapture *limitedBodyCapture
	var capture *HistoryCapture
	if r.HistoryCapture != nil {
		copy := *r.HistoryCapture
		capture = &copy
		if shouldCaptureBody(req.Header.Get("Content-Type"), req, capture) && req.Body != nil {
			requestCapture = newLimitedBodyCapture(capture.BodyMaxBytes)
			original := req.Body
			req.Body = &capturingReadCloser{ReadCloser: original, capture: requestCapture}
			defer func() { req.Body = original }()
		}
		captured.captureEnabled = func() bool {
			if !shouldCaptureResponseBody(captured.bodyHeaders(), req, capture) || captured.chunked {
				return false
			}
			return captured.HistoryStatus() >= 400 || captured.ErrorCategory() != ""
		}
	}
	w = captured
	defer func() {
		if r.Record != nil || r.RecordHistory != nil || r.RecordAggregation != nil {
			inbound := req.ContentLength
			if inbound < 0 {
				inbound = 0
			}
			traceID := ""
			if r.TraceID != nil {
				traceID = r.TraceID(req.Context())
			}
			full := event.Event{
				Host: req.Host, Domain: domain, Method: req.Method, Path: req.URL.Path, Query: req.URL.RawQuery,
				ClientIP: captured.clientIP, Scheme: scheme(req), Protocol: req.Proto,
				Status: captured.Status(), HistoryStatus: captured.HistoryStatus(), StatusSource: captured.StatusSource(), ErrorCategory: captured.ErrorCategory(), Inbound: inbound, Outbound: captured.Bytes(),
				DurationMS: time.Since(started).Milliseconds(), At: started, UserAgent: req.Header.Get("User-Agent"), ProxyStage: "completed", UpstreamStatus: captured.upstreamStatus,
				ExceptionType: captured.exceptionType, ExceptionMessage: captured.exceptionMessage, TraceID: traceID, BlacklistRuleID: captured.blacklistRuleID,
			}
			full.ResponseMode, full.TTFBMS, full.TTFTMS = captured.ResponseMode(), captured.TTFBMS(), captured.TTFTMS()
			if matched {
				full.HistoryMode = site.RequestHistoryMode
				full.HistorySuccessSamplingRate = site.RequestHistorySuccessSamplingRate
			}
			aggregate := event.AggregationEvent{
				Host: full.Host, Domain: full.Domain, At: full.At,
				Status: full.HistoryStatus, StatusSource: full.StatusSource, ErrorCategory: full.ErrorCategory,
				DurationMS: full.DurationMS, Inbound: full.Inbound, Outbound: full.Outbound, UserAgent: full.UserAgent,
				ResponseMode: full.ResponseMode, TTFBMS: full.TTFBMS, TTFTMS: full.TTFTMS,
			}
			historyRecord := true
			if capture != nil {
				metadata := true
				if capture.DecideEvent != nil {
					decision := capture.DecideEvent(full)
					historyRecord = decision.Record
					metadata = decision.CaptureMetadata
				} else if capture.Decide != nil {
					decision := capture.Decide(full.HistoryStatus, full.StatusSource, full.ErrorCategory)
					historyRecord = decision.Record
					metadata = decision.CaptureMetadata
				} else if capture.ShouldCaptureMetadata != nil {
					metadata = capture.ShouldCaptureMetadata(full.HistoryStatus, full.StatusSource, full.ErrorCategory)
				}
				if metadata {
					full.RequestHeaders = captureHeaders(req.Header, capture.HeaderMaxBytes)
					full.ResponseHeaders = captureHeaders(captured.Header(), capture.HeaderMaxBytes)
					full.RequestContentType = req.Header.Get("Content-Type")
					full.ResponseContentType = captured.Header().Get("Content-Type")
					if requestCapture != nil {
						full.RequestBodyBase64, full.RequestBodyTruncated = requestCapture.Base64(), requestCapture.Truncated()
					}
					if captured.capture != nil && captured.captureEnabled() {
						full.ResponseBodyBase64, full.ResponseBodyTruncated = captured.capture.Result(!captured.incomplete)
					}
					if captured.upstreamHeaders != nil {
						full.ResponseContentType = captured.upstreamHeaders.Get("Content-Type")
					}
				}
			}
			if r.RecordAggregation != nil && (r.ShouldAggregate == nil || r.ShouldAggregate(full)) {
				r.RecordAggregation(aggregate)
			}
			if r.RecordHistory != nil && historyRecord {
				r.RecordHistory(full)
			}
			if r.Record != nil {
				r.Record(full)
			}
		}
	}()
	// A plugin/provider panic must not escape to net/http's process-wide
	// recovery path, where it would be absent from request history. This is a
	// pipeline exception rather than an upstream failure.
	defer func() {
		if recovered := recover(); recovered != nil {
			if r.InternalPanic != nil {
				r.InternalPanic(fmt.Errorf("proxy pipeline panic: %v", recovered))
			}
			captured.classify(609, "YaginxPipeline", "ProxyPipelineException")
			captured.exceptionType = fmt.Sprintf("%T", recovered)
			captured.exceptionMessage = fmt.Sprint(recovered)
			if captured.status == 0 {
				w.Header().Set("Content-Length", "0")
				w.WriteHeader(609)
			}
		}
	}()
	const prefix = "/.well-known/acme-challenge/"
	if r.ACMEChallenge != nil && strings.HasPrefix(req.URL.Path, prefix) {
		if value, err := r.ACMEChallenge(strings.TrimPrefix(req.URL.Path, prefix)); err == nil {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", strconv.Itoa(len(value)))
			_, _ = w.Write([]byte(value))
			return
		}
	}
	if ruleID, candidate, blocked := r.blacklistMatch(req); blocked {
		captured.blacklistRuleID = ruleID
		captured.clientIP = candidate
		captured.classify(http.StatusForbidden, "YaginxPipeline", "Blacklist")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "Forbidden")
		return
	}
	if !matched {
		captured.classify(http.StatusNotFound, "YaginxPipeline", "")
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if binding != nil && strings.EqualFold(binding.AccessMode, "redirect") {
		target := binding.RedirectTargetHost
		if target == "" {
			target = site.RedirectHost
		}
		if target == "" {
			target = normalizedHost(req.Host)
		}
		status := binding.RedirectStatusCode
		if status != http.StatusMovedPermanently && status != http.StatusPermanentRedirect {
			status = http.StatusPermanentRedirect
		}
		w.Header().Set("Location", "https://"+target+req.URL.RequestURI())
		w.Header().Set("Content-Length", "0")
		captured.classify(status, "YaginxPipeline", "HostRedirect")
		w.WriteHeader(status)
		return
	}
	domain = normalizedHost(req.Host)
	if blocked, candidate := site.ipAccessBlocked(req); blocked {
		captured.clientIP = candidate
		category := "SiteIPBlacklist"
		if strings.EqualFold(site.IPAccessMode, "whitelist") {
			category = "SiteIPWhitelist"
		}
		captured.classify(http.StatusForbidden, "YaginxPipeline", category)
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "Forbidden")
		return
	}
	if site.MaintenanceMode {
		const message = "当前站点正在维护，暂时停止访问。"
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		captured.classify(http.StatusServiceUnavailable, "YaginxPipeline", "SiteMaintenance")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, message)
		return
	}
	if applyCORS(w, req) {
		captured.classify(http.StatusOK, "CorsPreflight", "")
		return
	}
	if response, ok := site.simpleResponse(req.URL.Path); ok {
		for k, v := range response.Headers {
			w.Header().Set(k, v)
		}
		if response.ContentType != "" {
			w.Header().Set("Content-Type", response.ContentType)
		}
		if response.Status == 0 {
			response.Status = http.StatusOK
		}
		captured.classify(response.Status, "YaginxPipeline", "")
		w.WriteHeader(response.Status)
		_, _ = w.Write([]byte(response.Body))
		return
	}
	if site.RedirectHTTPS && req.TLS == nil {
		host := site.RedirectHost
		if host == "" {
			host = strings.Split(req.Host, ":")[0]
		}
		// The legacy middleware returns an empty 302 response, not net/http's
		// default 308 HTML redirect body. Keep the wire behavior exact.
		w.Header().Set("Location", "https://"+host+req.URL.RequestURI())
		w.Header().Set("Content-Length", "0")
		captured.classify(http.StatusFound, "YaginxPipeline", "")
		w.WriteHeader(http.StatusFound)
		return
	}
	destination, destinationHost, originalHost, path, query, transforms := site.Destination, site.DestinationHost, site.OriginalHost, req.URL.Path, req.URL.RawQuery, site.Transforms
	webProxy, allowUnsafeTLS := site.WebProxy, site.AllowUnsafeTLS
	rule, ok := site.route(req.URL.Path)
	if !ok {
		rule, ok = site.fallback()
	}
	// Keep embedded callers and legacy snapshots with a direct destination
	// compatible; persisted website configs are migrated to an explicit fallback
	// rule by the repository decoder.
	if !ok && site.Destination != "" {
		rule = Rule{Enabled: true, IsFallback: true, Destination: site.Destination, DestinationHost: site.DestinationHost, WebProxy: site.WebProxy, AllowUnsafeTLS: site.AllowUnsafeTLS}
		ok = true
	}
	if !ok {
		captured.classify(http.StatusNotFound, "YaginxPipeline", "NoRoute")
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	{
		endpointSelected := false
		if pool, ok := site.UpstreamPools[rule.UpstreamPoolID]; ok {
			if endpoint, release, available := r.pickEndpoint(rule.UpstreamPoolID, pool); available {
				destination, destinationHost, webProxy = endpoint.Address, endpoint.Host, endpoint.Proxy
				if endpoint.TLSVerify != nil {
					allowUnsafeTLS = !*endpoint.TLSVerify
				}
				endpointSelected = true
				defer release()
			} else {
				captured.classify(http.StatusBadGateway, "YaginxProxy", "NoAvailableUpstream")
				http.Error(w, "no healthy upstream", http.StatusBadGateway)
				return
			}
		}
		if !endpointSelected && rule.Destination != "" {
			destination = rule.Destination
		}
		if rule.DestinationHost != "" {
			destinationHost = rule.DestinationHost
		}
		if rule.OriginalHost != nil {
			originalHost = *rule.OriginalHost
		}
		if !endpointSelected || rule.WebProxySet || rule.WebProxy != "" {
			webProxy = rule.WebProxy
		}
		if rule.AllowUnsafeTLSSet || (!endpointSelected && rule.AllowUnsafeTLS) {
			allowUnsafeTLS = rule.AllowUnsafeTLS
		}
		path = removePrefix(path, rule.RemovePrefix)
		if !rule.IsFallback {
			transforms = Transform{}
		} // child rules never inherit site transforms; fallback uses site defaults.
	}
	path, query = applyRequestTransform(path, query, transforms)
	target, err := url.Parse(destination)
	if err != nil || target.Scheme == "" || target.Host == "" {
		captured.classify(603, "YaginxProxy", "NoAvailableDestination")
		http.Error(w, "no usable upstream", http.StatusBadGateway)
		return
	}
	director := func(out *http.Request) {
		out.URL.Scheme = target.Scheme
		out.URL.Host = target.Host
		out.URL.Path = joinPath(target.Path, path)
		out.URL.RawQuery = query
		if !originalHost {
			out.Host = target.Host
			if destinationHost != "" && !looksLikeURL(destinationHost) {
				out.Host = destinationHost
			}
		}
		if transforms.RequestHeadersCopy.Set && !transforms.RequestHeadersCopy.Value {
			out.Header = make(http.Header)
		}
		if transforms.RequestHeader != "" {
			out.Header.Set(transforms.RequestHeader, transforms.Set)
		}
		if r.shouldAddExpectContinue(req, out) {
			out.Header.Set("Expect", "100-continue")
		}
		applyForwardedHeaders(out, req, transforms)
	}
	transport, err := r.transportFor(webProxy, allowUnsafeTLS, site.ResponseHeaderTimeout)
	if err != nil {
		captured.classify(609, "YaginxPipeline", "ProxyPipelineException")
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(609)
		return
	}
	(&httputil.ReverseProxy{Director: director, Transport: transport, ModifyResponse: func(response *http.Response) error {
		// Retain the upstream body metadata before response-header transforms.
		// Those transforms must not turn encoded/file bytes into eligible text.
		captured.upstreamHeaders = response.Header.Clone()
		captured.bodyLength = response.ContentLength
		captured.incomplete = true
		for _, encoding := range response.TransferEncoding {
			if strings.EqualFold(encoding, "chunked") {
				captured.chunked = true
			}
		}
		captured.setUpstreamStatus(response.StatusCode)
		if response.StatusCode >= http.StatusBadRequest {
			captured.classify(response.StatusCode, "Upstream", "UpstreamReturnedError")
		} else {
			captured.classify(response.StatusCode, "Upstream", "")
		}
		if transforms.ResponseHeadersCopy.Set && !transforms.ResponseHeadersCopy.Value {
			response.Header = make(http.Header)
		}
		if transforms.ResponseHeader != "" {
			response.Header.Set(transforms.ResponseHeader, transforms.Set)
		}
		if transforms.ResponseTrailersCopy.Set && !transforms.ResponseTrailersCopy.Value {
			response.Trailer = nil
			response.Header.Del("Trailer")
			response.Body = &trailerSuppressingBody{ReadCloser: response.Body, response: response}
		}
		if transforms.ResponseTrailer != "" {
			if response.Trailer == nil {
				response.Trailer = make(http.Header)
			}
			response.Trailer.Set(transforms.ResponseTrailer, transforms.Set)
		}
		// ReverseProxy owns the upgraded connection after a 101 response; wrapping
		// that body breaks WebSocket hijacking. Ordinary HTTP response streams can
		// still classify read failures before the request event is recorded.
		if response.StatusCode != http.StatusSwitchingProtocols {
			response.Body = &errorClassifyingBody{ReadCloser: response.Body, complete: func() {
				captured.incomplete = false
			}, classify: func() {
				captured.classify(606, "YaginxProxy", "UpstreamResponseBodyFailure")
			}}
		}
		return nil
	}, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
		status, source, category := classifyProxyError(req, err)
		captured.classify(status, source, category)
		captured.exceptionType, captured.exceptionMessage = fmt.Sprintf("%T", err), err.Error()
		if status == http.StatusRequestEntityTooLarge {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}}).ServeHTTP(w, req)
}

func (r *Runtime) pickEndpoint(id string, pool UpstreamPool) (UpstreamEndpoint, func(), bool) {
	eligible := make([]struct {
		endpoint UpstreamEndpoint
		index    int
	}, 0, len(pool.Endpoints))
	minPriority := 0
	strategy := pool.Strategy
	if strategy == "" {
		strategy = "weightedRoundRobin"
	}
	if strategy != "weightedRoundRobin" && strategy != "random" && strategy != "leastConnections" && strategy != "failover" {
		return UpstreamEndpoint{}, func() {}, false
	}
	total := 0
	for index, endpoint := range pool.Endpoints {
		if !endpoint.Enabled || strings.TrimSpace(endpoint.Address) == "" {
			continue
		}
		if endpoint.Weight < 1 {
			endpoint.Weight = 1
		}
		if strategy == "failover" && (len(eligible) == 0 || endpoint.Priority < minPriority) {
			minPriority = endpoint.Priority
			eligible = eligible[:0]
			total = 0
		}
		if strategy == "failover" && endpoint.Priority != minPriority {
			continue
		}
		eligible = append(eligible, struct {
			endpoint UpstreamEndpoint
			index    int
		}{endpoint, index})
		total += endpoint.Weight
	}
	if total == 0 {
		return UpstreamEndpoint{}, func() {}, false
	}
	selected := 0
	r.upstreamMu.Lock()
	if strategy == "random" {
		selected = rand.Intn(total)
	} else if strategy == "leastConnections" {
		active := r.upstreamActive[id]
		best := -1
		for i, candidate := range eligible {
			value := 0
			if candidate.index < len(active) {
				value = active[candidate.index]
			}
			if best == -1 || value < best {
				best, selected = value, i
			}
		}
	} else {
		selected = r.upstreamCursor[id] % total
		r.upstreamCursor[id] = (selected + 1) % total
	}
	if strategy == "random" || strategy == "weightedRoundRobin" || strategy == "failover" {
		cursor := selected
		for i, candidate := range eligible {
			if cursor < candidate.endpoint.Weight {
				selected = i
				break
			}
			cursor -= candidate.endpoint.Weight
		}
	}
	if strategy == "leastConnections" || strategy == "failover" {
		active := r.upstreamActive[id]
		if len(active) < len(pool.Endpoints) {
			expanded := make([]int, len(pool.Endpoints))
			copy(expanded, active)
			active = expanded
		}
		active[eligible[selected].index]++
		r.upstreamActive[id] = active
	}
	r.upstreamMu.Unlock()
	release := func() {}
	if strategy == "leastConnections" || strategy == "failover" {
		release = func() {
			r.upstreamMu.Lock()
			active := r.upstreamActive[id]
			if eligible[selected].index < len(active) && active[eligible[selected].index] > 0 {
				active[eligible[selected].index]--
			}
			r.upstreamMu.Unlock()
		}
	}
	return eligible[selected].endpoint, release, true
}

func (site Site) ipAccessBlocked(request *http.Request) (bool, string) {
	mode := strings.ToLower(strings.TrimSpace(site.IPAccessMode))
	if mode == "" || mode == "disabled" {
		return false, clientHost(request.RemoteAddr)
	}
	candidate, _ := blacklistCandidate(request)
	client := clientHost(request.RemoteAddr)
	if !candidate.IsValid() {
		return mode == "whitelist", client
	}
	matched := false
	for _, raw := range site.IPAccessCIDRs {
		value := strings.TrimSpace(raw)
		prefix, err := netip.ParsePrefix(value)
		if err == nil && prefix.Masked().Contains(candidate) {
			matched = true
			break
		}
		if address, err := netip.ParseAddr(value); err == nil && address.Unmap() == candidate {
			matched = true
			break
		}
	}
	if mode == "whitelist" {
		return !matched, candidate.String()
	}
	return mode == "blacklist" && matched, candidate.String()
}

func normalizeExpectPolicy(policy ExpectContinuePolicy) ExpectContinuePolicy {
	policy.IncludedHosts = append([]string(nil), policy.IncludedHosts...)
	policy.IncludedPathPrefixes = append([]string(nil), policy.IncludedPathPrefixes...)
	return policy
}

func (r *Runtime) shouldAddExpectContinue(in, out *http.Request) bool {
	policy := r.expectPolicy
	if !policy.Enabled || out.Header.Get("Expect") != "" || in.ContentLength <= policy.MinContentLengthBytes || !isExpectMethod(in.Method) || isConnectionUpgrade(in) || !matchesExpectHost(in.Host, policy.IncludedHosts) {
		return false
	}
	for _, prefix := range policy.IncludedPathPrefixes {
		if pathPrefixMatches(in.URL.Path, prefix) {
			return true
		}
	}
	return false
}

func isExpectMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

func isConnectionUpgrade(request *http.Request) bool {
	if request.Header.Get("Upgrade") == "" {
		return false
	}
	for _, value := range request.Header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				return true
			}
		}
	}
	return false
}

func matchesExpectHost(requestHost string, configured []string) bool {
	requestHost = normalizeAuthority(requestHost)
	for _, raw := range configured {
		candidate := strings.TrimSpace(raw)
		if parsed, err := url.Parse(candidate); err == nil && parsed.IsAbs() {
			candidate = parsed.Host
		}
		if requestHost != "" && requestHost == normalizeAuthority(candidate) {
			return true
		}
	}
	return false
}

func normalizeAuthority(value string) string {
	value, _ = hostnorm.Authority(value)
	return value
}

func pathPrefixMatches(requestPath, configuredPrefix string) bool {
	prefix := path.Clean("/" + strings.TrimSpace(configuredPrefix))
	if prefix == "/" {
		return strings.HasPrefix(requestPath, "/")
	}
	return requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/")
}

func (r *Runtime) blocked(request *http.Request) bool {
	_, _, blocked := r.blacklistMatch(request)
	return blocked
}

// blacklistMatch returns the database rule responsible for the block. A
// specific CIDR wins so its counter remains useful when rules overlap.
func (r *Runtime) blacklistMatch(request *http.Request) (int64, string, bool) {
	if len(r.blacklist) == 0 && len(r.snapshot.Load().Blacklist) == 0 {
		return 0, clientHost(request.RemoteAddr), false
	}
	candidate, fromForwarded := blacklistCandidate(request)
	if !candidate.IsValid() {
		return 0, clientHost(request.RemoteAddr), false
	}
	if fromForwarded && r.blacklistWarning != nil {
		r.blacklistWarning(candidate.String(), clientHost(request.RemoteAddr), request.Host)
	}
	var matchedID int64
	matchedBits := -1
	for _, rule := range r.snapshot.Load().Blacklist {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(rule.CIDR))
		if err == nil && prefix.Masked().Contains(candidate) && prefix.Bits() > matchedBits {
			matchedID, matchedBits = rule.ID, prefix.Bits()
		}
	}
	if matchedID != 0 {
		return matchedID, candidate.String(), true
	}
	for _, prefix := range r.blacklist {
		if prefix.Contains(candidate) {
			return 0, candidate.String(), true
		}
	}
	return 0, candidate.String(), false
}

func blacklistCandidate(request *http.Request) (netip.Addr, bool) {
	if forwarded := request.Header.Get("X-Forwarded-For"); forwarded != "" {
		first := strings.TrimSpace(strings.SplitN(forwarded, ",", 2)[0])
		if parsed, err := netip.ParseAddr(first); err == nil {
			return parsed.Unmap(), true
		}
	}
	if parsed, err := netip.ParseAddr(clientHost(request.RemoteAddr)); err == nil {
		return parsed.Unmap(), false
	}
	return netip.Addr{}, false
}

type trailerSuppressingBody struct {
	io.ReadCloser
	response *http.Response
}

type errorClassifyingBody struct {
	io.ReadCloser
	classify func()
	complete func()
}

func (b *errorClassifyingBody) Read(value []byte) (int, error) {
	n, err := b.ReadCloser.Read(value)
	if err == io.EOF && b.complete != nil {
		b.complete()
	}
	if err != nil && err != io.EOF && b.classify != nil {
		b.classify()
	}
	return n, err
}

func (b *trailerSuppressingBody) Close() error {
	err := b.ReadCloser.Close()
	b.response.Trailer = nil
	return err
}

func applyForwardedHeaders(out *http.Request, req *http.Request, transform Transform) {
	if transform.Forwarded != "" && strings.EqualFold(transform.Action, "set") {
		out.Header.Del("X-Forwarded-For")
		out.Header.Del("X-Forwarded-Host")
		out.Header.Del("X-Forwarded-Proto")
		out.Header.Del("X-Forwarded-Prefix")
		// ReverseProxy appends X-Forwarded-For when absent. A nil value is its
		// documented opt-out and preserves the legacy RFC7239-only transform.
		out.Header["X-Forwarded-For"] = nil
		values := strings.Split(strings.ToLower(transform.Forwarded), ",")
		wanted := map[string]bool{}
		for _, value := range values {
			wanted[strings.TrimSpace(value)] = true
		}
		parts := []string{}
		if wanted["proto"] {
			parts = append(parts, "proto="+scheme(req))
		}
		if wanted["host"] {
			parts = append(parts, `host="`+req.Host+`"`)
		}
		if wanted["for"] {
			parts = append(parts, "for="+clientHost(req.RemoteAddr))
		}
		out.Header.Set("Forwarded", strings.Join(parts, ";"))
		return
	}
	out.Header.Set("X-Forwarded-Proto", scheme(req))
	out.Header.Set("X-Forwarded-Host", req.Host)
	// Remove an inbound value before ReverseProxy performs its documented final
	// append, producing precisely one effective client IP rather than retaining
	// a client-supplied chain or appending it twice.
	out.Header.Del("X-Forwarded-For")
	if transform.Prefix != "" && strings.EqualFold(transform.XForwarded, "set") {
		out.Header.Set("X-Forwarded-Prefix", req.URL.Path)
	}
}

func (s Site) route(path string) (Rule, bool) {
	for _, rule := range s.Rules {
		pattern := "/" + strings.Trim(rule.PathPattern, "/")
		if !rule.Enabled || rule.IsFallback || rule.PathPattern == "" || (path != pattern && !strings.HasPrefix(path, pattern+"/")) {
			continue
		}
		return rule, true
	}
	return Rule{}, false
}

func (s Site) fallback() (Rule, bool) {
	for _, rule := range s.Rules {
		if rule.Enabled && rule.IsFallback {
			return rule, true
		}
	}
	return Rule{}, false
}

func (s Site) simpleResponse(path string) (Response, bool) {
	for candidate, response := range s.Simple {
		if strings.EqualFold(candidate, path) {
			return response, true
		}
	}
	return Response{}, false
}
func removePrefix(path, prefix string) string {
	if prefix == "" {
		return path
	}
	if strings.HasPrefix(path, prefix) {
		return "/" + strings.TrimLeft(strings.TrimPrefix(path, prefix), "/")
	}
	return path
}
func applyRequestTransform(path, rawQuery string, transform Transform) (string, string) {
	if transform.PathSet != "" {
		path = transform.PathSet
	}
	if transform.PathRemovePrefix != "" {
		path = removePrefix(path, transform.PathRemovePrefix)
	}
	if transform.PathPrefix != "" {
		path = joinPath(transform.PathPrefix, path)
	}
	if transform.QueryValueParameter != "" {
		rawQuery = setQueryValuePreservingOrder(rawQuery, transform.QueryValueParameter, transform.Set)
	}
	return path, rawQuery
}

// setQueryValuePreservingOrder retains unrelated raw query components exactly
// as received. url.Values.Encode sorts keys and would turn a legacy
// `old=one&fixture=value` request into a different wire URL.
func setQueryValuePreservingOrder(rawQuery, name, value string) string {
	parts := strings.Split(rawQuery, "&")
	kept := make([]string, 0, len(parts)+1)
	for _, part := range parts {
		key := part
		if index := strings.IndexByte(part, '='); index >= 0 {
			key = part[:index]
		}
		decoded, err := url.QueryUnescape(key)
		if err != nil || decoded != name {
			kept = append(kept, part)
		}
	}
	kept = append(kept, url.QueryEscape(name)+"="+url.QueryEscape(value))
	return strings.Join(kept, "&")
}
func looksLikeURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}

// transportKey identifies a derived transport. Derived transports are cached so
// per-site overrides keep upstream connection reuse instead of cloning (and
// discarding) a connection pool on every request.
type transportKey struct {
	proxyURL              string
	allowUnsafeTLS        bool
	responseHeaderTimeout time.Duration
}

// transportFor returns the shared transport, or a cached clone when the request
// needs a forward proxy, relaxed TLS verification, or a site-specific response
// header timeout. A zero responseHeaderTimeout inherits the global setting.
func (r *Runtime) transportFor(proxyURL string, allowUnsafeTLS bool, responseHeaderTimeout time.Duration) (http.RoundTripper, error) {
	if r.transportFactory != nil {
		// Test seam: injected round trippers own their timeouts, so the per-site
		// override is intentionally not applied here.
		return r.transportFactory(proxyURL, allowUnsafeTLS)
	}
	if r.transport == nil {
		return nil, fmt.Errorf("proxy transport is not initialised")
	}
	if responseHeaderTimeout == r.transport.ResponseHeaderTimeout {
		responseHeaderTimeout = 0
	}
	if proxyURL == "" && !allowUnsafeTLS && responseHeaderTimeout == 0 {
		return r.transport, nil
	}
	key := transportKey{proxyURL, allowUnsafeTLS, responseHeaderTimeout}
	r.derivedMu.Lock()
	defer r.derivedMu.Unlock()
	if cached, ok := r.derived[key]; ok {
		cached.lastUsed = time.Now()
		return cached.transport, nil
	}
	transport, err := r.deriveTransport(proxyURL, allowUnsafeTLS, responseHeaderTimeout)
	if err != nil {
		return nil, err
	}
	if r.derived == nil {
		r.derived = make(map[transportKey]*derivedTransport)
	}
	r.derived[key] = &derivedTransport{transport: transport, lastUsed: time.Now()}
	return transport, nil
}

func (r *Runtime) deriveTransport(proxyURL string, allowUnsafeTLS bool, responseHeaderTimeout time.Duration) (*http.Transport, error) {
	transport := r.transport.Clone()
	if responseHeaderTimeout > 0 {
		transport.ResponseHeaderTimeout = responseHeaderTimeout
	}
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("invalid forward proxy")
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	if allowUnsafeTLS {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		}
		// This is an explicit per-route legacy option, never a global default.
		transport.TLSClientConfig.InsecureSkipVerify = true
	}
	return transport, nil
}

// applyCORS preserves the legacy echo-origin behavior for configured routes.
// It handles preflight locally so it cannot consume an upstream request.
func applyCORS(w http.ResponseWriter, req *http.Request) bool {
	origin := strings.TrimSpace(req.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Add("Vary", "Origin")
	if headers := req.Header.Get("Access-Control-Request-Headers"); headers != "" {
		w.Header().Set("Access-Control-Allow-Headers", headers)
	}
	if methods := req.Header.Get("Access-Control-Request-Method"); methods != "" {
		w.Header().Set("Access-Control-Allow-Methods", methods)
	}
	if req.Method != http.MethodOptions {
		return false
	}
	methods := req.Header.Get("Access-Control-Request-Method")
	if methods == "" {
		methods = http.MethodGet
	}
	w.Header().Set("Access-Control-Allow-Methods", methods)
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
	return true
}

// responseCapture records the observable response facts while preserving the
// optional interfaces ReverseProxy needs for streaming and websocket traffic.
// It deliberately keeps measurement local; persistence is handled by the
// asynchronous telemetry queue.
type responseCapture struct {
	http.ResponseWriter
	status, historyStatus           int
	statusSource, errorCategory     string
	bytes                           int64
	upstreamStatus                  *int
	exceptionType, exceptionMessage string
	clientIP                        string
	blacklistRuleID                 int64
	capture                         *responseBodyCapture
	captureLimit                    int
	captureEnabled                  func() bool
	upstreamHeaders                 http.Header
	bodyLength                      int64
	chunked, incomplete             bool
	started                         time.Time
	firstByteAt, firstTokenAt       time.Time
	responseMode                    string
}

func (w *responseCapture) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseCapture) Write(value []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	w.ensureBodyCapture()
	n, err := w.ResponseWriter.Write(value)
	w.markFirstByte(int64(n))
	if err != nil {
		w.incomplete = true
		w.classify(608, "YaginxProxy", "ClientWriteFailure")
	}
	w.bytes += int64(n)
	if w.capture != nil && w.captureEnabled != nil && w.captureEnabled() {
		w.capture.Append(value[:n])
	}
	return n, err
}

func classifyProxyError(request *http.Request, err error) (int, string, string) {
	if request != nil && errors.Is(request.Context().Err(), context.Canceled) {
		return 601, "ClientCanceled", "ClientCanceled"
	}
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return http.StatusRequestEntityTooLarge, "YaginxProxy", "RequestBodyTooLarge"
	}
	if errors.Is(err, io.EOF) {
		return 605, "YaginxProxy", "UpstreamResponseHeaderFailure"
	}
	if request != nil && request.Body != nil && errors.Is(err, io.ErrUnexpectedEOF) {
		return 607, "YaginxProxy", "UpstreamRequestBodyFailure"
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		if networkError.Timeout() {
			return 602, "YaginxProxy", "UpstreamTimeout"
		}
		return 604, "YaginxProxy", "UpstreamRequestFailure"
	}
	return 610, "Unknown", "UnknownProxyError"
}

func (w *responseCapture) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *responseCapture) Bytes() int64 { return w.bytes }
func (w *responseCapture) HistoryStatus() int {
	if w.historyStatus != 0 {
		return w.historyStatus
	}
	return w.Status()
}
func (w *responseCapture) StatusSource() string {
	if w.statusSource != "" {
		return w.statusSource
	}
	return "YaginxPipeline"
}
func (w *responseCapture) ErrorCategory() string { return w.errorCategory }
func (w *responseCapture) classify(status int, source, category string) {
	w.historyStatus, w.statusSource, w.errorCategory = status, source, category
}
func (w *responseCapture) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseCapture) Flush() {
	if w.responseMode == "" {
		w.responseMode = "stream"
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *responseCapture) ResponseMode() string {
	if w.responseMode == "" {
		return "non_stream"
	}
	return w.responseMode
}
func (w *responseCapture) TTFBMS() int64 {
	if w.firstByteAt.IsZero() || w.started.IsZero() {
		return 0
	}
	return w.firstByteAt.Sub(w.started).Milliseconds()
}
func (w *responseCapture) TTFTMS() int64 {
	if w.firstTokenAt.IsZero() || w.started.IsZero() {
		return 0
	}
	return w.firstTokenAt.Sub(w.started).Milliseconds()
}
func (w *responseCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return h.Hijack()
}
func (w *responseCapture) Push(target string, options *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, options)
	}
	return http.ErrNotSupported
}
func (w *responseCapture) ReadFrom(source io.Reader) (int64, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if reader, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		w.ensureBodyCapture()
		if w.capture != nil && w.captureEnabled != nil && w.captureEnabled() {
			source = io.TeeReader(source, responseBodyWriter{capture: w.capture})
		}
		n, err := reader.ReadFrom(source)
		if err != nil {
			w.incomplete = true
		}
		w.bytes += n
		w.markFirstByte(n)
		return n, err
	}
	w.ensureBodyCapture()
	if w.capture != nil && w.captureEnabled != nil && w.captureEnabled() {
		source = io.TeeReader(source, responseBodyWriter{capture: w.capture})
	}
	n, err := io.Copy(w.ResponseWriter, source)
	if err != nil {
		w.incomplete = true
	}
	w.bytes += n
	w.markFirstByte(n)
	return n, err
}

func (w *responseCapture) markFirstByte(n int64) {
	if n <= 0 || !w.firstByteAt.IsZero() {
		return
	}
	w.firstByteAt = time.Now()
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(w.Header().Get("Content-Type"), ";")[0]))
	if contentType == "text/event-stream" {
		w.responseMode = "stream"
		w.firstTokenAt = w.firstByteAt
	}
}

func (w *responseCapture) bodyHeaders() http.Header {
	if w.upstreamHeaders != nil {
		return w.upstreamHeaders
	}
	return w.Header()
}

func (w *responseCapture) ensureBodyCapture() {
	if w.capture != nil || w.captureEnabled == nil || !w.captureEnabled() {
		return
	}
	length := w.bodyLength
	if w.upstreamHeaders == nil {
		length, _ = strconv.ParseInt(w.Header().Get("Content-Length"), 10, 64)
	}
	w.capture = newResponseBodyCapture(responseBodyEncoding(w.bodyHeaders()), length)
}

func (w *responseCapture) setUpstreamStatus(status int) {
	w.upstreamStatus = &status
}

type limitedBodyCapture struct {
	limit     int
	value     []byte
	truncated bool
}

func newLimitedBodyCapture(limit int) *limitedBodyCapture {
	if limit < 0 {
		limit = 0
	}
	return &limitedBodyCapture{limit: limit}
}

func (c *limitedBodyCapture) Append(value []byte) {
	if len(value) == 0 {
		return
	}
	remaining := c.limit - len(c.value)
	if remaining <= 0 {
		c.truncated = true
		return
	}
	if remaining < len(value) {
		c.value = append(c.value, value[:remaining]...)
		c.truncated = true
		return
	}
	c.value = append(c.value, value...)
}
func (c *limitedBodyCapture) Base64() string  { return base64.StdEncoding.EncodeToString(c.value) }
func (c *limitedBodyCapture) Truncated() bool { return c.truncated }

type limitedBodyWriter struct{ capture *limitedBodyCapture }

func (w limitedBodyWriter) Write(value []byte) (int, error) {
	w.capture.Append(value)
	return len(value), nil
}

type capturingReadCloser struct {
	io.ReadCloser
	capture *limitedBodyCapture
}

func (r *capturingReadCloser) Read(value []byte) (int, error) {
	n, err := r.ReadCloser.Read(value)
	r.capture.Append(value[:n])
	return n, err
}

func captureHeaders(headers http.Header, maxBytes int) map[string][]string {
	result := make(map[string][]string)
	used := 0
	for key, values := range headers {
		estimated := len(key)
		for _, value := range values {
			estimated += len(value)
		}
		if maxBytes > 0 && used+estimated > maxBytes {
			result["__truncated"] = []string{"true"}
			break
		}
		result[key] = append([]string(nil), values...)
		used += estimated
	}
	return result
}

func shouldCaptureBody(contentType string, req *http.Request, capture *HistoryCapture) bool {
	if capture == nil || isStreamingOrUpgrade(req) || strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "text/event-stream") {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" {
		return false
	}
	for _, value := range capture.BodyContentTypes {
		value = strings.ToLower(strings.TrimSpace(value))
		switch {
		case strings.HasSuffix(value, "/") && strings.HasPrefix(mediaType, value):
			return true
		case strings.HasPrefix(value, "+") && strings.HasSuffix(mediaType, value):
			return true
		case mediaType == value:
			return true
		}
	}
	return false
}

func isStreamingOrUpgrade(req *http.Request) bool {
	return strings.EqualFold(req.Header.Get("Upgrade"), "websocket")
}

func clientHost(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err == nil {
		return host
	}
	return remote
}
func (r *Runtime) match(host string) (Site, bool, *HostBinding) {
	host = normalizedHost(host)
	for _, site := range r.snapshot.Load().Sites {
		for i := range site.HostBindings {
			binding := &site.HostBindings[i]
			if binding.Enabled && normalizedHost(binding.Hostname) == host {
				return site, true, binding
			}
		}
		for _, candidate := range site.Hosts {
			if host == strings.ToLower(strings.TrimSuffix(candidate, ".")) {
				return site, true, nil
			}
		}
	}
	return Site{}, false, nil
}

func normalizedHost(host string) string {
	host, _ = hostnorm.HostHeader(host)
	return host
}
func joinPath(a, b string) string { return strings.TrimRight(a, "/") + "/" + strings.TrimLeft(b, "/") }
func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// WaitForRevision is used by config synchronization after a LISTEN wake-up;
// it keeps the last-known-good snapshot live when a reload fails.
func (r *Runtime) WaitForRevision(ctx context.Context, want int64) bool {
	for {
		if r.Snapshot().Revision >= want {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(20 * time.Millisecond):
		}
	}
}
