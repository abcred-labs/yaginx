package acme

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// PreflightError is returned before an ACME order is created.  Keeping the
// class stable lets callers distinguish a bad DNS/challenge setup from a CA
// failure without exposing challenge material.
type PreflightError struct {
	Class string
	Err   error
}

var errChallengeMismatch = errors.New("HTTP-01 response does not match key authorization")

func (e *PreflightError) Error() string { return fmt.Sprintf("ACME preflight %s: %v", e.Class, e.Err) }
func (e *PreflightError) Unwrap() error { return e.Err }

// IPResolver abstracts DNS lookups so deployments and tests can provide a
// resolver appropriate to their network.
type IPResolver interface {
	LookupIP(context.Context, string) ([]net.IP, error)
}

type systemIPResolver struct{}

func (systemIPResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

type publicIPResolver struct{ server string }

func (r publicIPResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "udp", net.JoinHostPort(r.server, "53"))
	}}
	return resolver.LookupIP(ctx, "ip", host)
}

var defaultPublicResolvers = []IPResolver{publicIPResolver{server: "1.1.1.1"}, publicIPResolver{server: "8.8.8.8"}}

// PreflightDomain verifies that the configured (possibly remote) key/value
// provider is usable and that HTTP-01 is reachable at every address returned
// by the local and public resolvers. It deliberately runs before StartACMEOrder.
func PreflightDomain(ctx context.Context, domain string, provider KeyValueProvider, resolvers []IPResolver, client *http.Client) error {
	if strings.TrimSpace(domain) == "" || strings.Contains(domain, "*") || net.ParseIP(domain) != nil {
		return &PreflightError{Class: "invalid_domain", Err: errors.New("domain is empty or wildcard")}
	}
	if strings.ContainsAny(domain, "/?#\t\r\n ") {
		return &PreflightError{Class: "invalid_domain", Err: errors.New("invalid domain")}
	}
	token, err := randomChallengeToken()
	if err != nil {
		return &PreflightError{Class: "key_value_write_failed", Err: err}
	}
	keyAuthorization := token + ".preflight"
	if err := provider.Present(domain, token, keyAuthorization); err != nil {
		return &PreflightError{Class: "key_value_write_failed", Err: err}
	}
	got, err := provider.Resolve(token)
	if err != nil {
		return &PreflightError{Class: "key_value_read_failed", Err: err}
	}
	if got != keyAuthorization {
		return &PreflightError{Class: "challenge_mismatch", Err: errors.New("key/value value mismatch")}
	}

	if len(resolvers) == 0 {
		resolvers = append([]IPResolver{systemIPResolver{}}, defaultPublicResolvers...)
	}
	addresses := make(map[string]net.IP)
	var lookupErrors []error
	var successfulLookups int
	for _, resolver := range resolvers {
		ips, lookupErr := resolver.LookupIP(ctx, domain)
		if lookupErr != nil {
			lookupErrors = append(lookupErrors, lookupErr)
			continue
		}
		successfulLookups++
		for _, ip := range ips {
			if ip != nil {
				addresses[ip.String()] = ip
			}
		}
	}
	if len(addresses) == 0 {
		if successfulLookups == 0 && len(lookupErrors) > 0 {
			return &PreflightError{Class: "dns_lookup_failed", Err: lookupErrors[0]}
		}
		return &PreflightError{Class: "dns_missing", Err: errors.New("no A or AAAA records")}
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	for _, ip := range addresses {
		if err := probeHTTP01(ctx, client, ip, domain, token, keyAuthorization); err != nil {
			class := "challenge_unreachable"
			if errors.Is(err, errChallengeMismatch) {
				class = "challenge_mismatch"
			}
			return &PreflightError{Class: class, Err: err}
		}
	}
	return nil
}

func probeHTTP01(ctx context.Context, client *http.Client, ip net.IP, domain, token, expected string) error {
	host := net.JoinHostPort(ip.String(), "80")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+host+"/.well-known/acme-challenge/"+token, nil)
	if err != nil {
		return err
	}
	request.Host = domain
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP-01 returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return err
	}
	if string(body) != expected {
		return errChallengeMismatch
	}
	return nil
}

func randomChallengeToken() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
