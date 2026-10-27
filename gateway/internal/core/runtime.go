// Package core provides the standalone, database-free rescue data plane.
package core

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"yaginx-gateway/internal/certificates"
	"yaginx-gateway/internal/listeners"
	"yaginx-gateway/internal/platform/atomicfile"
	"yaginx-gateway/internal/proxy"
)

const (
	FormatVersion       = 1
	DefaultSnapshotPath = "/var/lib/yaginx/core/runtime.json"
)

// Runtime is the entire local rescue contract. It intentionally contains no
// control-plane state: the core process can start with only this file.
type Runtime struct {
	Version      int            `json:"version"`
	Snapshot     proxy.Snapshot `json:"snapshot"`
	Certificates []Certificate  `json:"certificates"`
	Listeners    ListenerPlan   `json:"listeners"`
	Proxy        ProxyPlan      `json:"proxy"`
}

type Certificate struct {
	ChainPEM []byte `json:"chainPem"`
	KeyPEM   []byte `json:"keyPem"`
}

type ListenerPlan struct {
	HTTP, ProxyHTTP, HTTPS, ProxyHTTPS, HTTP3 string   `json:"-"`
	TrustedCIDRs                              []string `json:"trustedCidrs"`
	RequireProxyProtocol                      bool     `json:"requireProxyProtocol"`
	HeaderTimeout                             string   `json:"headerTimeout"`
	HTTP3MaxIdleTimeout                       string   `json:"http3MaxIdleTimeout"`
}

func (p ListenerPlan) MarshalJSON() ([]byte, error) {
	type wire struct {
		HTTP                 string   `json:"http"`
		ProxyHTTP            string   `json:"proxyHttp"`
		HTTPS                string   `json:"https"`
		ProxyHTTPS           string   `json:"proxyHttps"`
		HTTP3                string   `json:"http3"`
		TrustedCIDRs         []string `json:"trustedCidrs"`
		RequireProxyProtocol bool     `json:"requireProxyProtocol"`
		HeaderTimeout        string   `json:"headerTimeout"`
		HTTP3MaxIdleTimeout  string   `json:"http3MaxIdleTimeout"`
	}
	return json.Marshal(wire{
		HTTP: p.HTTP, ProxyHTTP: p.ProxyHTTP, HTTPS: p.HTTPS, ProxyHTTPS: p.ProxyHTTPS, HTTP3: p.HTTP3,
		TrustedCIDRs: append([]string(nil), p.TrustedCIDRs...), RequireProxyProtocol: p.RequireProxyProtocol,
		HeaderTimeout: p.HeaderTimeout, HTTP3MaxIdleTimeout: p.HTTP3MaxIdleTimeout,
	})
}

func (p *ListenerPlan) UnmarshalJSON(raw []byte) error {
	type wire struct {
		HTTP                 string   `json:"http"`
		ProxyHTTP            string   `json:"proxyHttp"`
		HTTPS                string   `json:"https"`
		ProxyHTTPS           string   `json:"proxyHttps"`
		HTTP3                string   `json:"http3"`
		TrustedCIDRs         []string `json:"trustedCidrs"`
		RequireProxyProtocol bool     `json:"requireProxyProtocol"`
		HeaderTimeout        string   `json:"headerTimeout"`
		HTTP3MaxIdleTimeout  string   `json:"http3MaxIdleTimeout"`
	}
	var value wire
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	p.HTTP, p.ProxyHTTP, p.HTTPS, p.ProxyHTTPS, p.HTTP3 = value.HTTP, value.ProxyHTTP, value.HTTPS, value.ProxyHTTPS, value.HTTP3
	p.TrustedCIDRs = append([]string(nil), value.TrustedCIDRs...)
	p.RequireProxyProtocol, p.HeaderTimeout, p.HTTP3MaxIdleTimeout = value.RequireProxyProtocol, value.HeaderTimeout, value.HTTP3MaxIdleTimeout
	return nil
}

type ProxyPlan struct {
	ActivityTimeout           string                     `json:"activityTimeout"`
	ResponseHeaderTimeout     string                     `json:"responseHeaderTimeout"`
	MaxConcurrentRequests     int                        `json:"maxConcurrentRequests"`
	MaxRequestHeaderBytes     int                        `json:"maxRequestHeaderBytes"`
	MaxRequestBodyBytes       int64                      `json:"maxRequestBodyBytes"`
	MaxIdleConnections        int                        `json:"maxIdleConnections"`
	MaxIdleConnectionsPerHost int                        `json:"maxIdleConnectionsPerHost"`
	IdleConnectionTimeout     string                     `json:"idleConnectionTimeout"`
	ExpectContinueTimeout     string                     `json:"expectContinueTimeout"`
	UpstreamExpectContinue    proxy.ExpectContinuePolicy `json:"upstreamExpectContinue"`
	IPBlacklistCIDRs          []string                   `json:"ipBlacklistCidrs"`
}

// NewRuntime creates a serialized rescue configuration from a fully accepted
// production snapshot. It does not write anything until Save succeeds.
func NewRuntime(snapshot *proxy.Snapshot, entries []certificates.Entry, listeners ListenerPlan, settings proxy.Settings) (Runtime, error) {
	if snapshot == nil {
		return Runtime{}, fmt.Errorf("core proxy snapshot is nil")
	}
	certificateValues := make([]Certificate, 0, len(entries))
	for _, entry := range entries {
		chain, key, err := certificates.EncodePEM(entry)
		if err != nil {
			return Runtime{}, err
		}
		certificateValues = append(certificateValues, Certificate{ChainPEM: chain, KeyPEM: key})
	}
	value := Runtime{
		Version: FormatVersion, Snapshot: *snapshot, Certificates: certificateValues, Listeners: listeners,
		Proxy: ProxyPlan{
			ActivityTimeout: settings.ActivityTimeout.String(), ResponseHeaderTimeout: settings.ResponseHeaderTimeout.String(),
			MaxConcurrentRequests: settings.MaxConcurrentRequests,
			MaxRequestHeaderBytes: settings.MaxRequestHeaderBytes, MaxRequestBodyBytes: settings.MaxRequestBodyBytes,
			MaxIdleConnections: settings.MaxIdleConnections, MaxIdleConnectionsPerHost: settings.MaxIdleConnectionsPerHost,
			IdleConnectionTimeout: settings.IdleConnectionTimeout.String(), ExpectContinueTimeout: settings.ExpectContinueTimeout.String(),
			UpstreamExpectContinue: settings.UpstreamExpectContinue, IPBlacklistCIDRs: append([]string(nil), settings.IPBlacklistCIDRs...),
		},
	}
	if _, _, err := value.Build(); err != nil {
		return Runtime{}, err
	}
	return value, nil
}

func Save(path string, value Runtime) error {
	if _, _, err := value.Build(); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, raw, 0600)
}

func Load(path string) (Runtime, error) {
	raw, err := atomicfile.Read(path)
	if err != nil {
		return Runtime{}, err
	}
	var value Runtime
	if err := json.Unmarshal(raw, &value); err != nil {
		return Runtime{}, fmt.Errorf("decode core runtime: %w", err)
	}
	if _, _, err := value.Build(); err != nil {
		return Runtime{}, err
	}
	return value, nil
}

// Build validates every listener, certificate, and proxy setting before any
// socket is bound. Callers can then safely pass the result to listeners.Start.
func (r Runtime) Build() (*proxy.Runtime, *listeners.Config, error) {
	if r.Version != FormatVersion {
		return nil, nil, fmt.Errorf("unsupported core runtime version %d", r.Version)
	}
	if len(r.Snapshot.Sites) == 0 {
		return nil, nil, fmt.Errorf("core runtime has no sites")
	}
	if err := validateListenerPlan(r.Listeners); err != nil {
		return nil, nil, err
	}
	settings, err := r.Proxy.settings()
	if err != nil {
		return nil, nil, err
	}
	manager, err := loadCertificates(r.Certificates)
	if err != nil {
		return nil, nil, err
	}
	for _, site := range r.Snapshot.Sites {
		for _, host := range site.Hosts {
			if _, err := manager.GetCertificate(&tls.ClientHelloInfo{ServerName: serverName(host)}); err != nil {
				return nil, nil, fmt.Errorf("core TLS certificate for %q: %w", host, err)
			}
		}
	}
	business, err := proxy.NewConfigured(&r.Snapshot, settings)
	if err != nil {
		return nil, nil, fmt.Errorf("core proxy settings: %w", err)
	}
	manager.SetHostFilter(business.HasHost)
	headerTimeout, _ := time.ParseDuration(r.Listeners.HeaderTimeout)
	h3Idle, _ := time.ParseDuration(r.Listeners.HTTP3MaxIdleTimeout)
	config := &listeners.Config{
		HTTP: r.Listeners.HTTP, ProxyHTTP: r.Listeners.ProxyHTTP, HTTPS: r.Listeners.HTTPS, ProxyHTTPS: r.Listeners.ProxyHTTPS, HTTP3: r.Listeners.HTTP3,
		TrustedCIDRs: append([]string(nil), r.Listeners.TrustedCIDRs...), RequireProxyProtocol: r.Listeners.RequireProxyProtocol,
		HeaderTimeout: headerTimeout, ActivityTimeout: settings.ActivityTimeout, HTTP3MaxIdleTimeout: h3Idle,
		MaxConcurrentRequests: settings.MaxConcurrentRequests, MaxRequestHeaderBytes: settings.MaxRequestHeaderBytes, MaxRequestBodyBytes: settings.MaxRequestBodyBytes,
		TLS: manager.TLSConfig(), Handler: business,
	}
	return business, config, nil
}

func (p ProxyPlan) settings() (proxy.Settings, error) {
	activity, err := requiredDuration("proxy.activityTimeout", p.ActivityTimeout)
	if err != nil {
		return proxy.Settings{}, err
	}
	response, err := requiredDuration("proxy.responseHeaderTimeout", p.ResponseHeaderTimeout)
	if err != nil {
		return proxy.Settings{}, err
	}
	idle, err := requiredDuration("proxy.idleConnectionTimeout", p.IdleConnectionTimeout)
	if err != nil {
		return proxy.Settings{}, err
	}
	expect, err := requiredDuration("proxy.expectContinueTimeout", p.ExpectContinueTimeout)
	if err != nil {
		return proxy.Settings{}, err
	}
	return proxy.Settings{
		ActivityTimeout: activity, ResponseHeaderTimeout: response, MaxConcurrentRequests: p.MaxConcurrentRequests, MaxRequestHeaderBytes: p.MaxRequestHeaderBytes,
		MaxRequestBodyBytes: p.MaxRequestBodyBytes, MaxIdleConnections: p.MaxIdleConnections,
		MaxIdleConnectionsPerHost: p.MaxIdleConnectionsPerHost, IdleConnectionTimeout: idle, ExpectContinueTimeout: expect,
		UpstreamExpectContinue: p.UpstreamExpectContinue, IPBlacklistCIDRs: append([]string(nil), p.IPBlacklistCIDRs...),
	}, nil
}

func validateListenerPlan(plan ListenerPlan) error {
	tcpAddresses := []struct {
		name, address string
	}{{"http", plan.HTTP}, {"proxy HTTP", plan.ProxyHTTP}, {"https", plan.HTTPS}, {"proxy HTTPS", plan.ProxyHTTPS}}
	seen := map[string]bool{}
	for _, item := range tcpAddresses {
		if err := validateAddress(item.name, item.address); err != nil {
			return err
		}
		if seen[item.address] {
			return fmt.Errorf("core listener address %q is duplicated", item.address)
		}
		seen[item.address] = true
	}
	// HTTP/3 is optional. When disabled, the serialized address is empty and
	// must not be validated as if a UDP listener were configured.
	if plan.HTTP3 != "" {
		if err := validateAddress("http3", plan.HTTP3); err != nil {
			return err
		}
	}
	if _, err := requiredDuration("listeners.headerTimeout", plan.HeaderTimeout); err != nil {
		return err
	}
	if _, err := requiredDuration("listeners.http3MaxIdleTimeout", plan.HTTP3MaxIdleTimeout); err != nil {
		return err
	}
	if len(plan.TrustedCIDRs) == 0 {
		return fmt.Errorf("core trusted PROXY CIDRs are empty")
	}
	for _, raw := range plan.TrustedCIDRs {
		if _, _, err := net.ParseCIDR(strings.TrimSpace(raw)); err != nil {
			return fmt.Errorf("core trusted PROXY CIDR %q: %w", raw, err)
		}
	}
	return nil
}

func validateAddress(name, address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(host) == "" {
		return fmt.Errorf("core %s listener address %q is invalid", name, address)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("core %s listener port %q is invalid", name, port)
	}
	return nil
}

func loadCertificates(values []Certificate) (*certificates.Manager, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("core runtime has no certificates")
	}
	entries := make([]certificates.Entry, 0, len(values))
	for _, value := range values {
		entry, err := certificates.ParsePEM(value.ChainPEM, value.KeyPEM, nil)
		if err != nil {
			return nil, fmt.Errorf("decode core certificate: %w", err)
		}
		if !entry.NotAfter.After(time.Now()) {
			return nil, fmt.Errorf("core certificate is expired")
		}
		entries = append(entries, entry)
	}
	manager := certificates.NewManager()
	if err := manager.Replace(entries); err != nil {
		return nil, err
	}
	return manager, nil
}

func requiredDuration(name, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		if err == nil {
			err = fmt.Errorf("must be positive")
		}
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return duration, nil
}

func serverName(host string) string {
	if value, _, err := net.SplitHostPort(host); err == nil {
		return strings.Trim(value, "[]")
	}
	return strings.Trim(strings.TrimSpace(host), "[]")
}
