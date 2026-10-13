// Package hostnorm centralizes normalization of domain names and HTTP
// authorities so routing, certificates, ACME and analytics use the same keys.
package hostnorm

import (
	"net"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

// Domain returns a canonical DNS name. Wildcards are rejected because this
// function is used for concrete ACME lock keys.
func Domain(value string) (string, bool) {
	value = strings.TrimSuffix(strings.TrimSpace(value), ".")
	if value == "" || strings.ContainsAny(value, "/:@*[]") {
		return "", false
	}
	value, err := idna.Lookup.ToASCII(value)
	if err != nil {
		return "", false
	}
	value = strings.ToLower(value)
	if len(value) > 253 {
		return "", false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 {
			return "", false
		}
	}
	return value, true
}

// HostHeader canonicalizes a Host header, stripping a port and IPv6 brackets.
func HostHeader(value string) (string, bool) {
	value = strings.TrimSpace(strings.ToLower(strings.TrimSuffix(value, ".")))
	if value == "" {
		return "", false
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	} else if strings.Count(value, ":") == 1 {
		value = strings.SplitN(value, ":", 2)[0]
	} else {
		value = strings.Trim(value, "[]")
	}
	return strings.TrimSuffix(value, "."), value != ""
}

// Authority canonicalizes a URL authority or host:port value for equality.
func Authority(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() {
		value = parsed.Host
	}
	value = strings.TrimSuffix(strings.ToLower(value), ".")
	if host, port, err := net.SplitHostPort(value); err == nil {
		return net.JoinHostPort(strings.TrimSuffix(host, "."), port), true
	}
	return value, value != ""
}

// IPOrHost is the resource-view form, treating the unmatched sentinel as
// absent while preserving valid IPv4/IPv6 and DNS host values.
func IPOrHost(value string) (string, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "__unmatched__" {
		return "", false
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	} else if strings.Count(value, ":") == 1 {
		value = strings.SplitN(value, ":", 2)[0]
	}
	value = strings.TrimSuffix(strings.Trim(value, "[]"), ".")
	return value, value != ""
}
