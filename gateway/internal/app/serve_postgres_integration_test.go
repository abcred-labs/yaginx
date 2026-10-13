//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/platform/identity"
)

// TestServeHostsBuiltReactAndManagementAPI starts the production application
// topology against a freshly migrated schema. It deliberately fetches the
// built React entrypoint over the management listener before exercising login,
// rather than treating a Vite build or an httptest-only router as evidence that
// the deployed URL shape works.
func TestServeHostsBuiltReactAndManagementAPI(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	schema := serveUniqueSchema(t)
	t.Logf("isolated schema=%s", schema)
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolated, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolated, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolated)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, "INSERT INTO account(email,password,password_hash,password_salt) VALUES($1,$2,'','')", "serve-admin@example.invalid", "serve-password"); err != nil {
		t.Fatal(err)
	}
	dir := serveTempDir(t)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.json")
	identityPath, snapshotPath := filepath.Join(dir, "identity.json"), filepath.Join(dir, "snapshot.bin")
	for path, value := range map[string]string{
		identityPath: `{"nodeId":"0123456789abcdef0123456789abcdef","createdAt":"2026-01-01T00:00:00Z"}`,
	} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	adminPort, httpPort, proxyHTTPPort, httpsPort, proxyHTTPSPort := freePort(t), freePort(t), freePort(t), freePort(t), freePort(t)
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	uiPath := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", "adminui", "dist"))
	if _, err := os.Stat(filepath.Join(uiPath, "index.html")); err != nil {
		t.Fatalf("built admin UI is required at %s: %v", uiPath, err)
	}
	document := map[string]any{
		"database":  map[string]any{"dsn": isolated},
		"node":      map[string]any{"identityFile": identityPath, "snapshotFile": snapshotPath, "shutdownGracePeriod": "1s"},
		"ports":     map[string]any{"bindAddress": "127.0.0.1", "http": httpPort, "proxyHttp": proxyHTTPPort, "https": httpsPort, "proxyHttps": proxyHTTPSPort, "management": adminPort},
		"listeners": map[string]any{"http3": map[string]any{"enabled": false}},
		"admin":     map[string]any{"uiDirectory": uiPath},
		"acme":      map[string]any{"enabled": false},
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	serveCtx, stop := context.WithCancel(context.Background())
	defer stop()
	var stdout, stderr bytes.Buffer
	finished := make(chan int, 1)
	go func() { finished <- runServe(serveCtx, []string{"--config", configPath}, &stdout, &stderr) }()
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", adminPort)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	var index *http.Response
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		index, _ = client.Get(baseURL + "/")
		if index != nil {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if index == nil {
		t.Fatalf("management listener did not start: %s", stderr.String())
	}
	content := new(bytes.Buffer)
	_, _ = content.ReadFrom(index.Body)
	index.Body.Close()
	if index.StatusCode != http.StatusOK || !bytes.Contains(content.Bytes(), []byte("<div id=\"root\">")) {
		t.Fatalf("React entrypoint status=%d body=%q", index.StatusCode, content.Bytes())
	}
	legacy, err := client.Get(baseURL + "/yaginx_console/")
	if err != nil {
		t.Fatal(err)
	}
	legacy.Body.Close()
	if legacy.StatusCode != http.StatusNotFound {
		t.Fatalf("legacy management UI path status=%d, want %d", legacy.StatusCode, http.StatusNotFound)
	}
	live, err := client.Get(baseURL + "/health/live")
	if err != nil {
		t.Fatal(err)
	}
	defer live.Body.Close()
	var liveBody map[string]any
	if err := json.NewDecoder(live.Body).Decode(&liveBody); err != nil || live.StatusCode != http.StatusOK || liveBody["status"] != "live" || liveBody["nodeId"] == "" {
		t.Fatalf("live status=%d body=%#v err=%v", live.StatusCode, liveBody, err)
	}
	ready, err := client.Get(baseURL + "/health/ready")
	if err != nil {
		t.Fatal(err)
	}
	defer ready.Body.Close()
	var readyBody map[string]any
	if err := json.NewDecoder(ready.Body).Decode(&readyBody); err != nil || ready.StatusCode != http.StatusServiceUnavailable || readyBody["ready"] != false || readyBody["nodeStatus"] != "active" {
		t.Fatalf("ready status=%d body=%#v err=%v", ready.StatusCode, readyBody, err)
	}
	metrics, err := client.Get(baseURL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer metrics.Body.Close()
	metricsBody := new(bytes.Buffer)
	_, _ = metricsBody.ReadFrom(metrics.Body)
	if metrics.StatusCode != http.StatusOK || !bytes.Contains(metricsBody.Bytes(), []byte("yaginx_config_revision")) {
		t.Fatalf("metrics status=%d body=%q", metrics.StatusCode, metricsBody.Bytes())
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/account/login", bytes.NewBufferString(`{"email":"serve-admin@example.invalid","password":"serve-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || envelope.Code != http.StatusOK || envelope.Data["token"] == "" {
		t.Fatalf("login status=%d envelope=%#v", response.StatusCode, envelope)
	}
	stop()
	select {
	case code := <-finished:
		if code != 0 {
			t.Fatalf("serve=%d stderr=%s", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
}

// TestServeRetainsLKGAndRejectsManagementWritesDuringDatabaseOutage exercises
// an actual connection loss. The application reaches PostgreSQL through a
// test-owned TCP forwarder; closing it terminates its existing pool sockets
// and prevents reconnects without stopping the shared integration database.
func TestServeRetainsLKGAndRejectsManagementWritesDuringDatabaseOutage(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	schema := serveUniqueSchema(t)
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolateDSN, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolateDSN, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	directPool, err := pgxpool.New(ctx, isolateDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer directPool.Close()
	nodeID := "123e4567-e89b-12d3-a456-426614174000"
	fingerprint, err := identity.MachineFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := directPool.Exec(ctx, "INSERT INTO gateway_node(node_id,fingerprint,machine_fingerprint,status,node_number,last_heartbeat_at) VALUES($1::uuid,$2,$3,'active',1,clock_timestamp())", nodeID, fingerprint, fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err := directPool.Exec(ctx, "INSERT INTO website(name,specifications,hosts,proxy_rules,proxy_transforms,simple_responses,create_time,update_time) VALUES($1,$2::jsonb,$3::jsonb,'[]'::jsonb,'{}'::jsonb,$4::jsonb,clock_timestamp(),clock_timestamp())", "lkg-site", `{"name":"lkg-site","hosts":["lkg.example.invalid"]}`, `["lkg.example.invalid"]`, `[{"path":"/lkg","statusCode":209,"contentType":"text/plain","responseContent":"still-serving"}]`); err != nil {
		t.Fatal(err)
	}
	forwarder, proxiedDSN := newPostgresForwarder(t, isolateDSN)
	defer forwarder.Close()
	dir := serveTempDir(t)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.json")
	_ = filepath.Join(dir, "jwt")
	identityPath, snapshotPath := filepath.Join(dir, "identity.json"), filepath.Join(dir, "snapshot.bin")
	for path, value := range map[string]string{
		identityPath: `{"nodeId":"123e4567-e89b-12d3-a456-426614174000","createdAt":"2026-01-01T00:00:00Z"}`,
	} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	adminPort, httpPort, proxyHTTPPort, httpsPort, proxyHTTPSPort := freePort(t), freePort(t), freePort(t), freePort(t), freePort(t)
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	uiPath := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", "adminui", "dist"))
	document := map[string]any{
		"database":  map[string]any{"dsn": proxiedDSN, "healthQueryTimeout": "100ms", "connectTimeout": "100ms"},
		"node":      map[string]any{"identityFile": identityPath, "snapshotFile": snapshotPath, "shutdownGracePeriod": "1s", "heartbeatInterval": "50ms"},
		"ports":     map[string]any{"bindAddress": "127.0.0.1", "http": httpPort, "proxyHttp": proxyHTTPPort, "https": httpsPort, "proxyHttps": proxyHTTPSPort, "management": adminPort},
		"listeners": map[string]any{"http3": map[string]any{"enabled": false}},
		"admin":     map[string]any{"uiDirectory": uiPath},
		"acme":      map[string]any{"enabled": false},
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	serveCtx, stop := context.WithCancel(context.Background())
	defer stop()
	var stdout, stderr bytes.Buffer
	finished := make(chan int, 1)
	go func() { finished <- runServe(serveCtx, []string{"--config", configPath}, &stdout, &stderr) }()
	adminURL, businessURL := fmt.Sprintf("http://127.0.0.1:%d", adminPort), fmt.Sprintf("http://127.0.0.1:%d/lkg", httpPort)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	requestBusiness := func() (*http.Response, error) {
		request, err := http.NewRequest(http.MethodGet, businessURL, nil)
		if err != nil {
			return nil, err
		}
		request.Host = "lkg.example.invalid"
		return client.Do(request)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := requestBusiness()
		if err == nil {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode == 209 && string(body) == "still-serving" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("business LKG did not start: stderr=%s", stderr.String())
		}
		time.Sleep(30 * time.Millisecond)
	}
	// This is a network-level loss, not a mocked Store or a closed local pool.
	forwarder.Close()
	deadline = time.Now().Add(5 * time.Second)
	for {
		request, err := http.NewRequest(http.MethodPost, adminURL+"/api/account/register", bytes.NewBufferString(`{"email":"outage@example.invalid","password":"x"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err == nil {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode == http.StatusServiceUnavailable && bytes.Contains(body, []byte(`"code":503`)) && bytes.Contains(body, []byte(`"requestId":"`)) {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("management write did not fail closed after DB outage: stderr=%s", stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	response, err := requestBusiness()
	if err != nil {
		t.Fatalf("business request during DB outage: %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 209 || string(body) != "still-serving" {
		t.Fatalf("business LKG during DB outage status=%d body=%q", response.StatusCode, body)
	}
	stop()
	select {
	case code := <-finished:
		if code != 0 {
			t.Fatalf("serve=%d stderr=%s", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
}

// TestServePropagatesConfigToThreeApprovedNodes verifies the complete serve
// topology rather than only the LISTEN helper. Every node owns its listener,
// identity, LKG file and PostgreSQL pool; the shared database mutation must
// become visible through each node's public HTTP data-plane listener.
func TestServePropagatesConfigToThreeApprovedNodes(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	schema := serveUniqueSchema(t)
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	isolation, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolation, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolation)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	fingerprint, err := identity.MachineFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003"}
	for index, id := range ids {
		if _, err := pool.Exec(ctx, "INSERT INTO gateway_node(node_id,fingerprint,machine_fingerprint,status,node_number,last_heartbeat_at) VALUES($1::uuid,$2,$3,'active',$4,clock_timestamp())", id, fingerprint, fingerprint, index+1); err != nil {
			t.Fatal(err)
		}
	}
	const host = "propagation.example.invalid"
	initial := `[{"path":"/config","statusCode":200,"contentType":"text/plain","responseContent":"before"}]`
	if _, err := pool.Exec(ctx, "INSERT INTO website(name,specifications,hosts,proxy_rules,proxy_transforms,simple_responses,create_time,update_time) VALUES('propagation-site','{}',$1::jsonb,'[]'::jsonb,'{}'::jsonb,$2::jsonb,clock_timestamp(),clock_timestamp())", `["`+host+`"]`, initial); err != nil {
		t.Fatal(err)
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	uiPath := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", "adminui", "dist"))
	if _, err := os.Stat(filepath.Join(uiPath, "index.html")); err != nil {
		t.Fatalf("built admin UI is required at %s: %v", uiPath, err)
	}
	type node struct {
		httpPort int
		stop     context.CancelFunc
		finished <-chan int
		stderr   *bytes.Buffer
	}
	nodes := make([]node, 0, len(ids))
	defer func() {
		for _, item := range nodes {
			item.stop()
		}
		for _, item := range nodes {
			select {
			case code := <-item.finished:
				if code != 0 {
					t.Errorf("serve node stopped with code=%d stderr=%s", code, item.stderr.String())
				}
			case <-time.After(10 * time.Second):
				t.Errorf("serve node did not stop: %s", item.stderr.String())
			}
		}
	}()
	for _, id := range ids {
		dir := serveTempDir(t)
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(dir, "config.json")
		_ = filepath.Join(dir, "jwt")
		identityPath, snapshotPath := filepath.Join(dir, "identity.json"), filepath.Join(dir, "snapshot.bin")
		for path, value := range map[string]string{
			identityPath: `{"nodeId":"` + id + `","createdAt":"2026-01-01T00:00:00Z"}`,
		} {
			if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		adminPort, httpPort, proxyHTTPPort, httpsPort, proxyHTTPSPort := freePort(t), freePort(t), freePort(t), freePort(t), freePort(t)
		document := map[string]any{
			"database": map[string]any{"dsn": isolation},
			"node":     map[string]any{"identityFile": identityPath, "snapshotFile": snapshotPath, "shutdownGracePeriod": "1s", "heartbeatInterval": "50ms"},
			// The test asserts end-to-end convergence, while LISTEN registration
			// itself is covered by configsync integration tests. A short poll
			// interval makes the fallback deterministic if a node begins LISTENing
			// just after the initial snapshot has become reachable.
			"configSync": map[string]any{"revisionPollInterval": "50ms"},
			"ports":      map[string]any{"bindAddress": "127.0.0.1", "http": httpPort, "proxyHttp": proxyHTTPPort, "https": httpsPort, "proxyHttps": proxyHTTPSPort, "management": adminPort},
			"listeners":  map[string]any{"http3": map[string]any{"enabled": false}},
			"admin":      map[string]any{"uiDirectory": uiPath}, "acme": map[string]any{"enabled": false},
		}
		raw, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		serveCtx, stop := context.WithCancel(context.Background())
		stderr := new(bytes.Buffer)
		finished := make(chan int, 1)
		go func() { finished <- runServe(serveCtx, []string{"--config", configPath}, io.Discard, stderr) }()
		nodes = append(nodes, node{httpPort: httpPort, stop: stop, finished: finished, stderr: stderr})
		// Start each node only after the previous node has claimed its HTTP
		// port. freePort releases the probe socket before runServe binds it;
		// launching all nodes back-to-back can otherwise hand the same port to
		// two nodes under concurrent test activity.
		deadline := time.Now().Add(5 * time.Second)
		for {
			connection, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", httpPort), 100*time.Millisecond)
			if dialErr == nil {
				_ = connection.Close()
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("serve node did not claim HTTP port %d: %s", httpPort, stderr.String())
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	client := &http.Client{Timeout: 250 * time.Millisecond}
	request := func(port int) (int, string, error) {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/config", port), nil)
		if err != nil {
			return 0, "", err
		}
		req.Host = host
		response, err := client.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(body), nil
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		ready := 0
		for _, item := range nodes {
			if status, body, err := request(item.httpPort); err == nil && status == http.StatusOK && body == "before" {
				ready++
			}
		}
		if ready == len(nodes) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("three serve nodes did not expose initial snapshot")
		}
		time.Sleep(25 * time.Millisecond)
	}
	durations := make([]time.Duration, 0, 20)
	for iteration := 0; iteration < 20; iteration++ {
		body := fmt.Sprintf("after-%d", iteration)
		updated := fmt.Sprintf(`[{"path":"/config","statusCode":200,"contentType":"text/plain","responseContent":"%s"}]`, body)
		started := time.Now()
		if _, err := pool.Exec(ctx, "UPDATE website SET simple_responses=$1::jsonb,update_time=clock_timestamp() WHERE name='propagation-site'", updated); err != nil {
			t.Fatal(err)
		}
		deadline = time.Now().Add(2 * time.Second)
		for {
			updatedNodes := 0
			for _, item := range nodes {
				if status, responseBody, err := request(item.httpPort); err == nil && status == http.StatusOK && responseBody == body {
					updatedNodes++
				}
			}
			if updatedNodes == len(nodes) {
				durations = append(durations, time.Since(started))
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("sample %d reached %d/%d nodes", iteration, updatedNodes, len(nodes))
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[(95*len(durations)+99)/100-1]
	if p95 >= 2*time.Second {
		t.Fatalf("three-node serve propagation p95=%s, want <2s; samples=%v", p95, durations)
	}
}

type postgresForwarder struct {
	listener    net.Listener
	target      string
	mu          sync.Mutex
	connections map[net.Conn]struct{}
}

func newPostgresForwarder(t *testing.T, dsn string) (*postgresForwarder, string) {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Host == "" {
		t.Fatalf("parse PostgreSQL DSN: %q %v", dsn, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	forwarder := &postgresForwarder{listener: listener, target: parsed.Host, connections: make(map[net.Conn]struct{})}
	go forwarder.accept()
	parsed.Host = listener.Addr().String()
	return forwarder, parsed.String()
}

func (f *postgresForwarder) accept() {
	for {
		client, err := f.listener.Accept()
		if err != nil {
			return
		}
		upstream, err := net.Dial("tcp", f.target)
		if err != nil {
			_ = client.Close()
			continue
		}
		f.track(client)
		f.track(upstream)
		go func() {
			defer f.untrack(client)
			defer f.untrack(upstream)
			defer client.Close()
			defer upstream.Close()
			go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close() }()
			_, _ = io.Copy(client, upstream)
		}()
	}
}

func (f *postgresForwarder) track(connection net.Conn) {
	f.mu.Lock()
	f.connections[connection] = struct{}{}
	f.mu.Unlock()
}

func (f *postgresForwarder) untrack(connection net.Conn) {
	f.mu.Lock()
	delete(f.connections, connection)
	f.mu.Unlock()
}

func (f *postgresForwarder) Close() {
	if f.listener != nil {
		_ = f.listener.Close()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for connection := range f.connections {
		_ = connection.Close()
	}
	f.connections = make(map[net.Conn]struct{})
}

func serveUniqueSchema(t *testing.T) string {
	t.Helper()
	value := make([]byte, 5)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	return "yaginx_goal_ui" + hex.EncodeToString(value)
}

// serveTempDir waits out the final asynchronous snapshot write during Windows
// cleanup. t.TempDir removes immediately, while a successfully cancelled
// listener can still be returning from its last atomic rename.
func serveTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "yaginx-serve-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(2 * time.Second)
		for {
			err := os.RemoveAll(dir)
			if err == nil {
				if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("remove serve temporary directory %s: %v", dir, err)
			}
			time.Sleep(25 * time.Millisecond)
		}
	})
	return dir
}
