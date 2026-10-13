package app

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yaginx-gateway/internal/certificates"
	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/configsync"
	"yaginx-gateway/internal/core"
	"yaginx-gateway/internal/platform/identity"
	"yaginx-gateway/internal/proxy"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := Run(context.Background(), []string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run(version) returned %d: %s", code, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatal("version output is empty")
	}
}

func TestLegacyDatabaseCommandsAreNotExposed(t *testing.T) {
	for _, args := range [][]string{
		{"migrate", "up"},
		{"import-legacy"},
		{"legacy-preflight"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), args, &stdout, &stderr); code != 2 {
			t.Fatalf("Run(%v) returned %d, want unknown-command failure", args, code)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"database"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "database <init|migrate|status>") {
		t.Fatalf("database usage code=%d stderr=%q", code, stderr.String())
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"database":{"dsn":"postgres://127.0.0.1:1/yaginx?sslmode=disable"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"database", "status", "--config", configPath}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "database status:") {
		t.Fatalf("database action-first flags code=%d stderr=%q", code, stderr.String())
	}
}

func TestRuntimeProxySettingsMapsRuntimeContract(t *testing.T) {
	cfg := config.RuntimeDocument{Values: map[string]any{"proxy": map[string]any{
		"activityTimeout": "31s", "responseHeaderTimeout": "32s", "maxRequestHeaderBytes": float64(8192), "maxRequestBodyBytes": float64(64), "maxIdleConnections": float64(12), "maxIdleConnectionsPerHost": float64(4), "idleConnectionTimeout": "33s", "expectContinueTimeout": "34s", "ipBlacklistCIDRs": []any{"198.51.100.0/24"},
		"upstreamExpectContinue": map[string]any{"enabled": true, "minContentLengthBytes": float64(63), "includedHosts": []any{"https://ai.example.test"}, "includedPathPrefixes": []any{"/api/image-gen"}},
	}}}
	settings, err := runtimeProxySettings(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ActivityTimeout != 31*time.Second || settings.ResponseHeaderTimeout != 32*time.Second || settings.MaxRequestHeaderBytes != 8192 || settings.MaxRequestBodyBytes != 64 || settings.MaxIdleConnections != 12 || settings.MaxIdleConnectionsPerHost != 4 || settings.IdleConnectionTimeout != 33*time.Second || settings.ExpectContinueTimeout != 34*time.Second {
		t.Fatalf("settings=%#v", settings)
	}
	if !settings.UpstreamExpectContinue.Enabled || settings.UpstreamExpectContinue.MinContentLengthBytes != 63 || len(settings.UpstreamExpectContinue.IncludedHosts) != 1 || len(settings.IPBlacklistCIDRs) != 1 {
		t.Fatalf("policy=%#v", settings)
	}
}

func TestSPAStaticUsesContractCacheHeaders(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("index"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "assets", "main-abcdef.js"), []byte("asset"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := spaStaticWithCache(directory, "no-cache", "public, max-age=31536000, immutable")
	for _, item := range []struct{ path, cache, body string }{{"/", "no-cache", "index"}, {"/assets/main-abcdef.js", "public, max-age=31536000, immutable", "asset"}, {"/client/route", "no-cache", "index"}} {
		t.Run(item.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://admin.test"+item.path, nil))
			if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != item.cache || response.Body.String() != item.body {
				t.Fatalf("path=%s status=%d cache=%q body=%q", item.path, response.Code, response.Header().Get("Cache-Control"), response.Body.String())
			}
		})
	}
}

func TestOfflineRuntimeRestoresPlaintextCertificates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.bin")
	node := identity.Identity{NodeID: "0123456789abcdef0123456789abcdef"}
	chain, key := appSelfSignedPEM(t, "lkg.example.invalid")
	entry, err := certificates.ParsePEM(chain, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &proxy.Snapshot{Revision: 9, Sites: []proxy.Site{{Hosts: []string{"lkg.example.invalid"}, Destination: "http://example.invalid"}}}
	if err := persistRuntimeLKG(path, snapshot, []certificates.Entry{entry}); err != nil {
		t.Fatal(err)
	}
	cfg := config.RuntimeDocument{Values: map[string]any{"node": map[string]any{"snapshotFile": path}}}
	loaded, entries, err := loadOfflineRuntime(cfg, node)
	if err != nil || loaded.Revision != 9 || len(entries) != 1 {
		t.Fatalf("loaded=%#v certificates=%d err=%v", loaded, len(entries), err)
	}
	manager := certificates.NewManager()
	if err := manager.Replace(entries); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.GetCertificate(&tls.ClientHelloInfo{ServerName: "lkg.example.invalid"}); err != nil {
		t.Fatalf("offline SNI certificate: %v", err)
	}
}

func TestPersistCoreRuntimeWritesValidatedRescueSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core-runtime.json")
	chain, key := appSelfSignedPEM(t, "core.example.invalid")
	entry, err := certificates.ParsePEM(chain, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &proxy.Snapshot{Revision: 11, Sites: []proxy.Site{{Hosts: []string{"core.example.invalid"}, Destination: "http://127.0.0.1:1"}}}
	plan := core.ListenerPlan{
		HTTP: "127.0.0.1:18080", ProxyHTTP: "127.0.0.1:19080", HTTPS: "127.0.0.1:18443", ProxyHTTPS: "127.0.0.1:19443", HTTP3: "127.0.0.1:18443",
		TrustedCIDRs: []string{"127.0.0.0/8"}, RequireProxyProtocol: true, HeaderTimeout: "1s", HTTP3MaxIdleTimeout: "1s",
	}
	if err := persistCoreRuntimeAt(path, snapshot, []certificates.Entry{entry}, plan, proxy.DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	loaded, err := core.Load(path)
	if err != nil || loaded.Snapshot.Revision != 11 || len(loaded.Certificates) != 1 {
		t.Fatalf("core runtime=%#v err=%v", loaded, err)
	}
}

func appSelfSignedPEM(t *testing.T, name string) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment}
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

func TestSPAStaticFallsBackToIndexForClientRoute(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("<html>app</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "asset.js"), []byte("asset"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := spaStatic(directory)
	for _, path := range []string{"/websites/42", "/asset.js"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("path=%s status=%d", path, response.Code)
		}
	}
}

func TestLoadOfflineSnapshotRejectsEmptyAndAcceptsPlaintextLKG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.bin")
	node := identity.Identity{NodeID: "0123456789abcdef0123456789abcdef"}
	snapshot, _ := json.Marshal(&proxy.Snapshot{Revision: 7, Sites: []proxy.Site{{Hosts: []string{"example.test"}, Destination: "https://example.invalid"}}})
	if err := configsync.SaveLKG(path, snapshot); err != nil {
		t.Fatal(err)
	}
	cfg := config.RuntimeDocument{Values: map[string]any{"node": map[string]any{"snapshotFile": path}}}
	loaded, err := loadOfflineSnapshot(cfg, node)
	if err != nil || loaded.Revision != 7 || len(loaded.Sites) != 1 {
		t.Fatalf("snapshot=%#v err=%v", loaded, err)
	}
	empty, _ := json.Marshal(&proxy.Snapshot{})
	if err := configsync.SaveLKG(path, empty); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOfflineSnapshot(cfg, node); err == nil {
		t.Fatal("empty LKG accepted")
	}
}

func TestServeRejectsColdStartWhenDatabaseIsUnavailable(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	dsn := "host=127.0.0.1 port=1 user=offline dbname=offline sslmode=disable"
	identityPath, snapshotPath := filepath.Join(directory, "identity.json"), filepath.Join(directory, "snapshot.bin")
	uiPath := filepath.Join(directory, "ui")
	if err := os.Mkdir(uiPath, 0o700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{identityPath: `{"nodeId":"0123456789abcdef0123456789abcdef","createdAt":"2026-01-01T00:00:00Z"}`} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, _ := json.Marshal(&proxy.Snapshot{Sites: []proxy.Site{{Hosts: []string{"offline.example.invalid"}, Simple: map[string]proxy.Response{"/health": {Body: "lkg"}}}}})
	if err := configsync.SaveLKG(snapshotPath, snapshot); err != nil {
		t.Fatal(err)
	}
	httpPort, proxyHTTPPort, httpsPort, proxyHTTPSPort, adminPort := freePort(t), freePort(t), freePort(t), freePort(t), freePort(t)
	document := map[string]any{"database": map[string]any{"dsn": dsn}, "node": map[string]any{"identityFile": identityPath, "snapshotFile": snapshotPath, "shutdownGracePeriod": "1s"}, "ports": map[string]any{"bindAddress": "127.0.0.1", "http": httpPort, "proxyHttp": proxyHTTPPort, "https": httpsPort, "proxyHttps": proxyHTTPSPort, "management": adminPort}, "listeners": map[string]any{"http3": map[string]any{"enabled": false}}, "admin": map[string]any{"uiDirectory": uiPath}, "acme": map[string]any{"enabled": false}}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "config.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	result := make(chan int, 1)
	go func() { result <- runServe(ctx, []string{"--config", configPath}, &stdout, &stderr) }()
	select {
	case code := <-result:
		if code != 0 {
			t.Fatalf("setup serve=%d stderr=%s", code, stderr.String())
		}
	case <-time.After(250 * time.Millisecond):
		cancel()
		select {
		case code := <-result:
			if code != 0 {
				t.Fatalf("setup serve=%d stderr=%s", code, stderr.String())
			}
		case <-time.After(5 * time.Second):
			t.Fatal("setup serve did not stop")
		}
	}
}

func TestRunReportBackfillRejectsInvalidArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"report", "backfill", "--from", "2026-08-04", "--to", "2026-08-03"}, &stdout, &stderr); code != 2 {
		t.Fatalf("Run(report backfill)=%d stderr=%s", code, stderr.String())
	}
	if code := Run(context.Background(), []string{"report", "backfill", "--from", "invalid", "--to", "2026-08-03"}, &stdout, &stderr); code != 2 {
		t.Fatalf("Run(report invalid date)=%d stderr=%s", code, stderr.String())
	}
}

func TestRunReportResetRequiresExplicitGoOnlyConfirmation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"report", "reset"}, &stdout, &stderr); code != 2 {
		t.Fatalf("Run(report reset)=%d stderr=%s", code, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("confirm-go-contributions")) {
		t.Fatalf("reset refusal did not explain confirmation: %s", stderr.String())
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := Run(context.Background(), []string{"unknown"}, &stdout, &stderr); code != 2 {
		t.Fatalf("Run(unknown) returned %d, want 2", code)
	}
}

func TestLocalReadyURLUsesLoopbackForWildcardBinding(t *testing.T) {
	for _, test := range []struct{ address, want string }{
		{"0.0.0.0:9090", "http://127.0.0.1:9090/health/ready"},
		{"127.0.0.2:19090", "http://127.0.0.2:19090/health/ready"},
	} {
		got, err := localReadyURL(test.address, "/health/ready")
		if err != nil || got != test.want {
			t.Fatalf("localReadyURL(%q) = %q, %v; want %q", test.address, got, err, test.want)
		}
	}
}

func TestReadyHandlerIncludesUpgradeIdentity(t *testing.T) {
	health := newHealthState("0.1.400", "commit-1", "node-1", "active")
	health.setSnapshot(12, time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC))
	health.setReady([]string{"127.0.0.1:443"})

	response := httptest.NewRecorder()
	health.readyHandler(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	var body struct {
		Ready   bool     `json:"ready"`
		Version string   `json:"version"`
		NodeID  string   `json:"nodeId"`
		Reasons []string `json:"reasons"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !body.Ready || body.Version != "0.1.400" || body.NodeID != "node-1" || len(body.Reasons) != 0 {
		t.Fatalf("status=%d body=%#v", response.Code, body)
	}
}

func TestReadyHandlerPreservesNotReadyDiagnostics(t *testing.T) {
	health := newHealthState("0.1.400", "commit-1", "node-1", "disabled")

	response := httptest.NewRecorder()
	health.readyHandler(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	var body struct {
		Ready      bool     `json:"ready"`
		Version    string   `json:"version"`
		NodeID     string   `json:"nodeId"`
		NodeStatus string   `json:"nodeStatus"`
		Reasons    []string `json:"reasons"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusServiceUnavailable || body.Ready || body.Version != "0.1.400" || body.NodeID != "node-1" || body.NodeStatus != "disabled" {
		t.Fatalf("status=%d body=%#v", response.Code, body)
	}
	if len(body.Reasons) != 2 || body.Reasons[0] != "snapshot_unavailable" || body.Reasons[1] != "listeners_unavailable" {
		t.Fatalf("reasons=%#v", body.Reasons)
	}
}
