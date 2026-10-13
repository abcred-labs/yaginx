package core

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yaginx-gateway/internal/certificates"
	"yaginx-gateway/internal/listeners"
	"yaginx-gateway/internal/proxy"
)

func TestRuntimeRoundTripStartsStaticProxy(t *testing.T) {
	ports := testPorts(t)
	chain, key := selfSignedPEM(t, "core.example.invalid")
	entry, err := certificates.ParsePEM(chain, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := NewRuntime(
		&proxy.Snapshot{Revision: 12, Sites: []proxy.Site{{Hosts: []string{"core.example.invalid"}, Simple: map[string]proxy.Response{"/probe": {Status: http.StatusNoContent}}}}},
		[]certificates.Entry{entry},
		ListenerPlan{HTTP: ports[0], ProxyHTTP: ports[1], HTTPS: ports[2], ProxyHTTPS: ports[3], HTTP3: ports[4], TrustedCIDRs: []string{"127.0.0.0/8"}, RequireProxyProtocol: true, HeaderTimeout: "1s", HTTP3MaxIdleTimeout: "1s"},
		proxy.DefaultSettings(),
	)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "runtime.json")
	if err := Save(path, value); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.Snapshot.Revision != 12 {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	_, config, err := loaded.Build()
	if err != nil {
		t.Fatal(err)
	}
	running, err := listeners.Start(*config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close(context.Background()) }()

	request, err := http.NewRequest(http.MethodGet, "http://"+ports[0]+"/probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "core.example.invalid"
	response, err := (&http.Client{Timeout: time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestSaveRetainsPriorRuntimeWhenCandidateIsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.json")
	value := validRuntime(t)
	if err := Save(path, value); err != nil {
		t.Fatal(err)
	}
	value.Snapshot.Sites = nil
	if err := Save(path, value); err == nil {
		t.Fatal("invalid core runtime was saved")
	}
	loaded, err := Load(path)
	if err != nil || len(loaded.Snapshot.Sites) != 1 {
		t.Fatalf("prior core runtime was not retained: %#v err=%v", loaded, err)
	}
}

func TestLoadRejectsMalformedOrIncompleteRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"snapshot":{"sites":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("incomplete core runtime was accepted")
	}
	if err := os.WriteFile(path, []byte(`not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("malformed core runtime was accepted")
	}
}

func TestValidateListenerPlanAllowsDisabledHTTP3(t *testing.T) {
	ports := testPorts(t)
	plan := ListenerPlan{HTTP: ports[0], ProxyHTTP: ports[1], HTTPS: ports[2], ProxyHTTPS: ports[3], TrustedCIDRs: []string{"127.0.0.1/32"}, HeaderTimeout: "1s", HTTP3MaxIdleTimeout: "30s"}
	if err := validateListenerPlan(plan); err != nil {
		t.Fatalf("disabled HTTP3 plan rejected: %v", err)
	}
}

func TestRunValidateNeedsOnlySnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.json")
	if err := Save(path, validRuntime(t)); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr limitedBuffer
	if code := Run(context.Background(), []string{"--validate", "--snapshot", path}, &stdout, &stderr); code != 0 {
		t.Fatalf("core validate=%d stderr=%s", code, stderr.String())
	}
	if stdout.String() != "core runtime snapshot: ok\n" {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func validRuntime(t *testing.T) Runtime {
	t.Helper()
	ports := testPorts(t)
	chain, key := selfSignedPEM(t, "core.example.invalid")
	entry, err := certificates.ParsePEM(chain, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := NewRuntime(
		&proxy.Snapshot{Sites: []proxy.Site{{Hosts: []string{"core.example.invalid"}, Destination: "http://127.0.0.1:1"}}},
		[]certificates.Entry{entry},
		ListenerPlan{HTTP: ports[0], ProxyHTTP: ports[1], HTTPS: ports[2], ProxyHTTPS: ports[3], HTTP3: ports[4], TrustedCIDRs: []string{"127.0.0.0/8"}, RequireProxyProtocol: true, HeaderTimeout: "1s", HTTP3MaxIdleTimeout: "1s"},
		proxy.DefaultSettings(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func testPorts(t *testing.T) []string {
	t.Helper()
	addresses := make([]string, 0, 5)
	for range 5 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addresses = append(addresses, listener.Addr().String())
		_ = listener.Close()
	}
	return addresses
}

func selfSignedPEM(t *testing.T, name string) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
}

type limitedBuffer struct{ values []byte }

func (b *limitedBuffer) Write(value []byte) (int, error) {
	b.values = append(b.values, value...)
	return len(value), nil
}
func (b *limitedBuffer) String() string { return string(b.values) }

var _ io.Writer = (*limitedBuffer)(nil)
