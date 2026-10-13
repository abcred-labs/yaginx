package certificates

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	issuerCertificateTimeout = 3 * time.Second
	maxIssuerCertificateSize = 1 << 20
	maxIssuerChainDepth      = 3
)

var issuerHTTPClient = &http.Client{
	Timeout: issuerCertificateTimeout,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type issuerCertificateFetcher func(context.Context, string) ([]byte, error)

type issuerCacheEntry struct {
	body []byte
	err  error
}

var issuerCache sync.Map

// CompleteChain supplements a legacy leaf-only certificate with the issuer
// named by its AIA extension. The database material is never changed.
func CompleteChain(ctx context.Context, entry Entry) (Entry, error) {
	return completeChain(ctx, entry, fetchIssuerCertificate)
}

func completeChain(ctx context.Context, entry Entry, fetch issuerCertificateFetcher) (Entry, error) {
	if len(entry.Certificate.Certificate) != 1 {
		return entry, nil
	}
	leaf, err := x509.ParseCertificate(entry.Certificate.Certificate[0])
	if err != nil {
		return entry, fmt.Errorf("parse leaf certificate: %w", err)
	}
	entry.Certificate.Certificate = append([][]byte(nil), entry.Certificate.Certificate...)
	entry.Certificate.Leaf = leaf
	current := leaf
	added := false
	for range maxIssuerChainDepth {
		issuer, err := fetchVerifiedIssuer(ctx, current, fetch)
		if err != nil {
			if added {
				return entry, nil
			}
			return entry, err
		}
		if containsCertificate(entry.Certificate.Certificate, issuer.Raw) {
			return entry, nil
		}
		entry.Certificate.Certificate = append(entry.Certificate.Certificate, issuer.Raw)
		current = issuer
		added = true
	}
	return entry, nil
}

func fetchVerifiedIssuer(ctx context.Context, certificate *x509.Certificate, fetch issuerCertificateFetcher) (*x509.Certificate, error) {
	if len(certificate.IssuingCertificateURL) == 0 {
		return nil, fmt.Errorf("certificate has no AIA issuer URL")
	}
	for _, rawURL := range certificate.IssuingCertificateURL {
		if err := validateIssuerURL(rawURL); err != nil {
			continue
		}
		body, err := fetch(ctx, rawURL)
		if err != nil {
			continue
		}
		certificates, err := parseIssuerCertificates(body)
		if err != nil {
			continue
		}
		for _, issuer := range certificates {
			if err := validateIssuer(certificate, issuer); err == nil {
				return issuer, nil
			}
		}
	}
	return nil, fmt.Errorf("no valid issuer certificate available from AIA")
}

func containsCertificate(chain [][]byte, candidate []byte) bool {
	for _, certificate := range chain {
		if bytes.Equal(certificate, candidate) {
			return true
		}
	}
	return false
}

func validateIssuerURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse AIA URL: %w", err)
	}
	if parsed.User != nil || parsed.Hostname() == "" {
		return fmt.Errorf("AIA URL must have a host and no user info")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported AIA URL scheme %q", parsed.Scheme)
	}
	port := parsed.Port()
	if port != "" && port != "80" && port != "443" {
		return fmt.Errorf("unsupported AIA URL port %q", port)
	}
	return nil
}

func fetchIssuerCertificate(ctx context.Context, rawURL string) ([]byte, error) {
	if value, ok := issuerCache.Load(rawURL); ok {
		entry := value.(issuerCacheEntry)
		return append([]byte(nil), entry.body...), entry.err
	}
	body, err := fetchIssuerCertificateUncached(ctx, rawURL)
	if ctx.Err() == nil {
		issuerCache.Store(rawURL, issuerCacheEntry{body: append([]byte(nil), body...), err: err})
	}
	return body, err
}

func fetchIssuerCertificateUncached(ctx context.Context, rawURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/pkix-cert, application/x-x509-ca-cert, application/pem-certificate-chain")
	response, err := issuerHTTPClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("issuer response status %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxIssuerCertificateSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxIssuerCertificateSize {
		return nil, fmt.Errorf("issuer response exceeds %d bytes", maxIssuerCertificateSize)
	}
	return body, nil
}

func parseIssuerCertificates(data []byte) ([]*x509.Certificate, error) {
	if certificate, err := x509.ParseCertificate(data); err == nil {
		return []*x509.Certificate{certificate}, nil
	}
	var certificates []*x509.Certificate
	for len(data) > 0 {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		data = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certificates = append(certificates, certificate)
	}
	if len(certificates) == 0 {
		return nil, fmt.Errorf("issuer response contains no certificate")
	}
	return certificates, nil
}

func validateIssuer(leaf, issuer *x509.Certificate) error {
	if !bytes.Equal(leaf.RawIssuer, issuer.RawSubject) {
		return fmt.Errorf("issuer subject does not match leaf issuer")
	}
	if !issuer.IsCA || !issuer.BasicConstraintsValid {
		return fmt.Errorf("issuer is not a certificate authority")
	}
	if issuer.KeyUsage != 0 && issuer.KeyUsage&x509.KeyUsageCertSign == 0 {
		return fmt.Errorf("issuer cannot sign certificates")
	}
	now := time.Now()
	if now.Before(issuer.NotBefore) || now.After(issuer.NotAfter) {
		return fmt.Errorf("issuer is outside its validity period")
	}
	if err := leaf.CheckSignatureFrom(issuer); err != nil {
		return fmt.Errorf("verify leaf signature: %w", err)
	}
	return nil
}
