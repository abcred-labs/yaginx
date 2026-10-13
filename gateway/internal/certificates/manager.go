// Package certificates owns in-memory SNI selection. Private keys never leave
// the immutable snapshot handed to crypto/tls or HTTP/3.
package certificates

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"software.sslmate.com/src/go-pkcs12"
	"yaginx-gateway/internal/platform/hostnorm"
)

type Entry struct {
	Names       []string
	Certificate tls.Certificate
	NotBefore   time.Time
	NotAfter    time.Time
}
type snapshot struct {
	exact    map[string]Entry
	wildcard map[string]Entry
}
type Manager struct{ current atomic.Pointer[snapshot] }

func NewManager() *Manager {
	m := &Manager{}
	m.current.Store(&snapshot{exact: map[string]Entry{}, wildcard: map[string]Entry{}})
	return m
}
func (m *Manager) Replace(entries []Entry) error {
	next := &snapshot{exact: map[string]Entry{}, wildcard: map[string]Entry{}}
	for _, entry := range entries {
		if len(entry.Certificate.Certificate) == 0 {
			return fmt.Errorf("certificate chain is empty")
		}
		if entry.NotAfter.IsZero() {
			leaf, err := x509.ParseCertificate(entry.Certificate.Certificate[0])
			if err != nil {
				return fmt.Errorf("parse leaf: %w", err)
			}
			entry.NotAfter = leaf.NotAfter
		}
		for _, name := range entry.Names {
			name = normalize(name)
			if name == "" {
				return fmt.Errorf("certificate name is empty")
			}
			if strings.HasPrefix(name, "*.") {
				if _, exists := next.wildcard[name]; exists {
					return fmt.Errorf("duplicate wildcard certificate %q", name)
				}
				next.wildcard[name] = entry
			} else {
				if _, exists := next.exact[name]; exists {
					return fmt.Errorf("duplicate certificate %q", name)
				}
				next.exact[name] = entry
			}
		}
	}
	m.current.Store(next)
	return nil
}

// Entries returns the active immutable certificate set for LKG
// persistence after a proxy-only incremental configuration update.
func (m *Manager) Entries() []Entry {
	current := m.current.Load()
	seen := map[[32]byte]bool{}
	entries := make([]Entry, 0, len(current.exact)+len(current.wildcard))
	add := func(entry Entry) {
		if len(entry.Certificate.Certificate) == 0 {
			return
		}
		fingerprint := sha256.Sum256(entry.Certificate.Certificate[0])
		if seen[fingerprint] {
			return
		}
		seen[fingerprint] = true
		entry.Names = append([]string(nil), entry.Names...)
		entries = append(entries, entry)
	}
	for _, entry := range current.exact {
		add(entry)
	}
	for _, entry := range current.wildcard {
		add(entry)
	}
	return entries
}
func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if hello == nil {
		return nil, fmt.Errorf("SNI is required")
	}
	name := normalize(hello.ServerName)
	if name == "" {
		return nil, fmt.Errorf("SNI is required")
	}
	current := m.current.Load()
	if entry, ok := current.exact[name]; ok && entry.NotAfter.After(time.Now()) {
		return &entry.Certificate, nil
	}
	labels := strings.Split(name, ".")
	if len(labels) > 2 {
		if entry, ok := current.wildcard["*."+strings.Join(labels[1:], ".")]; ok && entry.NotAfter.After(time.Now()) {
			return &entry.Certificate, nil
		}
	}
	return nil, fmt.Errorf("no active certificate for %q", name)
}
func (m *Manager) TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: m.GetCertificate, NextProtos: []string{"h2", "http/1.1"}}
}
func ParsePEM(chainPEM, keyPEM []byte, names []string) (Entry, error) {
	certificate, err := tls.X509KeyPair(chainPEM, keyPEM)
	if err != nil {
		return Entry{}, err
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return Entry{}, err
	}
	if len(names) == 0 {
		names = leaf.DNSNames
	}
	return Entry{Names: names, Certificate: certificate, NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter}, nil
}

// EncodePEM returns a self-contained chain/key representation suitable for an
// LKG snapshot. It is never written as a standalone file.
func EncodePEM(entry Entry) ([]byte, []byte, error) {
	if len(entry.Certificate.Certificate) == 0 || entry.Certificate.PrivateKey == nil {
		return nil, nil, fmt.Errorf("certificate chain or private key is empty")
	}
	var chain []byte
	for _, der := range entry.Certificate.Certificate {
		chain = append(chain, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(entry.Certificate.PrivateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("encode certificate private key: %w", err)
	}
	return chain, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), nil
}
func ParsePFX(data, password []byte) (Entry, error) {
	blocks, err := pkcs12.ToPEM(data, string(password))
	if err != nil {
		return Entry{}, fmt.Errorf("decode PFX: %w", err)
	}
	var chain, key []byte
	for _, block := range blocks {
		if block.Type == "CERTIFICATE" {
			chain = append(chain, pem.EncodeToMemory(block)...)
		} else if strings.Contains(block.Type, "PRIVATE KEY") {
			key = append(key, pem.EncodeToMemory(block)...)
		}
	}
	if len(chain) == 0 || len(key) == 0 {
		return Entry{}, fmt.Errorf("PFX lacks certificate chain or private key")
	}
	return ParsePEM(chain, key, nil)
}
func normalize(value string) string {
	value, _ = hostnorm.HostHeader(value)
	return value
}
