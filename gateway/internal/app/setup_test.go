package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"yaginx-gateway/internal/config"
)

func setupTestPorts(t *testing.T) (setupRequest, net.Listener) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	return setupRequest{BindAddress: "127.0.0.1", HTTPPort: port}, listener
}

func TestCheckSetupPortsRejectsOccupiedTCPPort(t *testing.T) {
	request, listener := setupTestPorts(t)
	defer listener.Close()
	if err := checkSetupPorts(request); err == nil || !strings.Contains(err.Error(), "http") || !strings.Contains(err.Error(), "tcp") {
		t.Fatalf("occupied TCP port was accepted: %v", err)
	}
}

func TestCheckSetupPortsErrorFieldsIdentifyListener(t *testing.T) {
	request, listener := setupTestPorts(t)
	defer listener.Close()
	err := checkSetupPorts(request)
	var portErr *setupPortError
	if !errors.As(err, &portErr) || !reflect.DeepEqual(portErr.fields, []string{"bindAddress", "httpPort"}) {
		t.Fatalf("port error fields=%#v err=%v", portErr, err)
	}
}

func TestCheckSetupPortsChecksHTTP3UDPPort(t *testing.T) {
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	// Only the UDP socket is real. Unrelated TCP ports must not prevent this
	// test from reaching the HTTP/3 check on a busy CI runner.
	request := setupRequest{BindAddress: "127.0.0.1", HTTPPort: 1, ProxyHTTPPort: 2, HTTPSPort: udp.LocalAddr().(*net.UDPAddr).Port, ProxyHTTPSPort: 3}
	enabled := true
	request.HTTP3Enabled = &enabled
	err = checkSetupPortsWithBind(request, func(network, address string) (io.Closer, error) {
		if network == "udp" {
			return bindSetupPort(network, address)
		}
		return &setupTestSocket{}, nil
	})
	var portErr *setupPortError
	if !errors.As(err, &portErr) || !strings.Contains(err.Error(), "http3") || !strings.Contains(err.Error(), "udp") || !reflect.DeepEqual(portErr.fields, []string{"bindAddress", "httpsPort"}) {
		t.Fatalf("occupied UDP port was accepted: %v", err)
	}
}

type setupTestSocket struct{ closes int }

func (s *setupTestSocket) Close() error {
	s.closes++
	return nil
}

func TestCheckSetupPortsBindingAndCleanup(t *testing.T) {
	enabled := true
	for _, tc := range []struct {
		name       string
		request    setupRequest
		failOn     string
		wantCalls  []string
		wantFields []string
		wantError  string
	}{
		{
			name:      "setup management socket is reused",
			request:   setupRequest{BindAddress: "127.0.0.1", ManagementPort: 9090},
			wantCalls: []string{"tcp 127.0.0.1:8080", "tcp 127.0.0.1:9080", "tcp 127.0.0.1:8443", "tcp 127.0.0.1:9443"},
		},
		{
			name:      "HTTP3 shares HTTPS port using UDP",
			request:   setupRequest{BindAddress: "127.0.0.1", ManagementPort: 19090, HTTP3Enabled: &enabled},
			wantCalls: []string{"tcp 127.0.0.1:19090", "tcp 127.0.0.1:8080", "tcp 127.0.0.1:9080", "tcp 127.0.0.1:8443", "tcp 127.0.0.1:9443", "udp 127.0.0.1:8443"},
		},
		{
			name:    "TCP conflict",
			request: setupRequest{BindAddress: "127.0.0.1"}, failOn: "tcp 127.0.0.1:9080",
			wantCalls:  []string{"tcp 127.0.0.1:8080", "tcp 127.0.0.1:9080"},
			wantFields: []string{"bindAddress", "proxyHttpPort"}, wantError: "proxy_http cannot use 127.0.0.1:9080/tcp",
		},
		{
			name:    "UDP conflict",
			request: setupRequest{BindAddress: "127.0.0.1", HTTP3Enabled: &enabled}, failOn: "udp 127.0.0.1:8443",
			wantCalls:  []string{"tcp 127.0.0.1:8080", "tcp 127.0.0.1:9080", "tcp 127.0.0.1:8443", "tcp 127.0.0.1:9443", "udp 127.0.0.1:8443"},
			wantFields: []string{"bindAddress", "httpsPort"}, wantError: "http3 cannot use 127.0.0.1:8443/udp",
		},
		{
			name:       "duplicate TCP ports",
			request:    setupRequest{BindAddress: "127.0.0.1", HTTPPort: 18080, ProxyHTTPPort: 18080},
			wantCalls:  []string{"tcp 127.0.0.1:18080"},
			wantFields: []string{"httpPort", "proxyHttpPort"}, wantError: "used by both http and proxy_http",
		},
		{
			name:       "invalid port",
			request:    setupRequest{BindAddress: "127.0.0.1", HTTPPort: 65536},
			wantFields: []string{"httpPort"}, wantError: "http port 65536 is invalid",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			var sockets []*setupTestSocket
			bindErr := errors.New("address already in use")
			err := checkSetupPortsWithBind(tc.request, func(network, address string) (io.Closer, error) {
				call := network + " " + address
				calls = append(calls, call)
				if call == tc.failOn {
					return nil, bindErr
				}
				socket := &setupTestSocket{}
				sockets = append(sockets, socket)
				return socket, nil
			})
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var portErr *setupPortError
				if !errors.As(err, &portErr) || !strings.Contains(err.Error(), tc.wantError) || !reflect.DeepEqual(portErr.fields, tc.wantFields) {
					t.Fatalf("error=%v, want %q with fields %v", err, tc.wantError, tc.wantFields)
				}
				if tc.failOn != "" && !errors.Is(err, bindErr) {
					t.Fatalf("bind error was not preserved: %v", err)
				}
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("bind calls=%v, want %v", calls, tc.wantCalls)
			}
			for i, socket := range sockets {
				if socket.closes != 1 {
					t.Errorf("socket %d closed %d times, want once", i, socket.closes)
				}
			}
		})
	}
}

func TestSetupApplyRejectsOccupiedPortBeforeApply(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	auth, err := newSetupAuth(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	called := false
	handler := setupHandlerWithApply(t.TempDir()+"/config.json", "", auth,
		func(context.Context, string, setupRequest) error { called = true; return nil }, func() {})
	body := fmt.Sprintf(`{"host":"127.0.0.1","database":"db","username":"user","password":"secret","bindAddress":"127.0.0.1","httpPort":%d,"confirm":true}`, port)
	request := httptest.NewRequest(http.MethodPost, "/api/setup/apply", strings.NewReader(body))
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Yaginx-Setup-Token", auth.token)
	record := httptest.NewRecorder()
	handler.ServeHTTP(record, request)
	if record.Code != http.StatusConflict || !strings.Contains(record.Body.String(), "http") {
		t.Fatalf("occupied port response status=%d body=%q", record.Code, record.Body.String())
	}
	if called {
		t.Fatal("apply callback ran despite occupied listener port")
	}
}

func TestSetupDSNUsesStructuredFieldsAndEscapesCredentials(t *testing.T) {
	dsn, err := setupDSN(setupRequest{Host: "2001:db8::1", Port: 5432, Database: "gateway", Username: "user@example", Password: "p@ss:/%", SSLMode: "require"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "[2001:db8::1]:5432" || u.Path != "/gateway" || u.Query().Get("sslmode") != "require" {
		t.Fatalf("dsn=%s", dsn)
	}
	password, _ := u.User.Password()
	if password != "p@ss:/%" || u.User.Username() != "user@example" {
		t.Fatalf("credentials not decoded: %s", u.Redacted())
	}
}

func TestSetupRejectsRawDSNAndWeakAdministratorPassword(t *testing.T) {
	for _, body := range []string{`{"runtimeDsn":"postgres://user:secret@host/db"}`, `{"host":"127.0.0.1","database":"db","username":"u","password":"p","adminEmail":"admin@example.com","adminPassword":"1234","adminPasswordConfirm":"1234"}`, `{"host":"127.0.0.1","database":"db","username":"u","password":"p","adminEmail":"admin@example.com","adminPassword":"12345","adminPasswordConfirm":"1234x"}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/setup/check", strings.NewReader(body))
		if _, err := decodeSetupRequest(req); err == nil {
			t.Fatalf("case accepted: %s", body)
		}
	}
	accepted := httptest.NewRequest(http.MethodPost, "/api/setup/check", strings.NewReader(`{"host":"127.0.0.1","database":"db","username":"u","password":"p","adminEmail":"admin@example.com","adminPassword":"12345","adminPasswordConfirm":"12345"}`))
	if _, err := decodeSetupRequest(accepted); err != nil {
		t.Fatalf("five-character administrator password rejected: %v", err)
	}
}

func TestSetupRejectsCreateDatabaseAction(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/setup/apply", strings.NewReader(`{"host":"127.0.0.1","database":"db","username":"app-user","password":"secret","action":"create-database"}`))
	if _, err := decodeSetupRequest(request); err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("create-database action accepted: %v", err)
	}
}

func TestSetupAllowsFiveCharacterAdministratorPassword(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/setup/check", strings.NewReader(`{"host":"127.0.0.1","database":"db","username":"u","password":"p","adminEmail":"admin@example.com","adminPassword":"12345","adminPasswordConfirm":"12345"}`))
	if _, err := decodeSetupRequest(request); err != nil {
		t.Fatalf("five-character password rejected: %v", err)
	}
}

func TestApplySetupOptionsUsesAdministratorEmailForACME(t *testing.T) {
	values, err := config.NewRuntimeValues("postgres://test@127.0.0.1/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	request := setupRequest{AdminEmail: "admin@example.com"}
	if err := applySetupOptions(values, request); err != nil {
		t.Fatal(err)
	}
	if got := values["acme"].(map[string]any)["email"]; got != "admin@example.com" {
		t.Fatalf("acme email=%v", got)
	}
	values, err = config.NewRuntimeValues("postgres://test@127.0.0.1/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	request.ACMEEmail = "cert@example.com"
	if err := applySetupOptions(values, request); err != nil {
		t.Fatal(err)
	}
	if got := values["acme"].(map[string]any)["email"]; got != "cert@example.com" {
		t.Fatalf("manual acme email=%v", got)
	}
}

func TestSafeSetupErrorRedactsDatabaseCredentials(t *testing.T) {
	request := setupRequest{Host: "127.0.0.1", Port: 5432, Database: "db", Username: "app", Password: "db-secret", SSLMode: "disable"}
	err := safeSetupError(errors.New("postgres://app:db-secret@127.0.0.1:5432/db"), request)
	if strings.Contains(err.Error(), "db-secret") {
		t.Fatalf("credential leaked: %s", err)
	}
}

func TestSetupPageIsPublicButAPIsRequireToken(t *testing.T) {
	auth, err := newSetupAuth(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	handler := setupHandler(t.TempDir()+"/config.json", "database setup or upgrade is required", auth)
	record := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	handler.ServeHTTP(record, request)
	if record.Code != http.StatusOK || !strings.Contains(record.Body.String(), "YAGINX / FIRST SETUP") {
		t.Fatalf("setup page status=%d body=%q", record.Code, record.Body.String())
	}
	if strings.Contains(record.Body.String(), "database setup or upgrade is required") {
		t.Fatal("setup reason leaked into the setup page")
	}

	blocked := httptest.NewRecorder()
	foreign := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/setup/check", strings.NewReader(`{"host":"127.0.0.1","database":"db","username":"user","password":"secret"}`))
	foreign.RemoteAddr = "192.0.2.1:1234"
	handler.ServeHTTP(blocked, foreign)
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized setup request status=%d", blocked.Code)
	}
}

func TestSetupApplyRequiresConfirmationBeforeDatabaseAccess(t *testing.T) {
	auth, err := newSetupAuth(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	handler := setupHandler(t.TempDir()+"/config.json", "", auth)
	record := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/setup/apply", strings.NewReader(`{"host":"127.0.0.1","database":"db","username":"user","password":"secret","action":"initialize"}`))
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Yaginx-Setup-Token", auth.token)
	handler.ServeHTTP(record, request)
	if record.Code != http.StatusBadRequest || strings.Contains(record.Body.String(), "secret") {
		t.Fatalf("setup confirmation response status=%d body=%q", record.Code, record.Body.String())
	}
}

func TestSetupTokenExpiresAndCannotBeReused(t *testing.T) {
	now := time.Now()
	auth, err := newSetupAuth(now)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/setup/check", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Yaginx-Setup-Token", auth.token)
	if !auth.authorize(request, now.Add(time.Minute)) {
		t.Fatal("valid setup token was rejected")
	}
	auth.consume()
	if auth.authorize(request, now.Add(2*time.Minute)) {
		t.Fatal("consumed setup token was accepted")
	}

	auth, err = newSetupAuth(now)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Yaginx-Setup-Token", auth.token)
	if auth.authorize(request, now.Add(setupTokenTTL+time.Second)) {
		t.Fatal("expired setup token was accepted")
	}
}

func TestSetupTokenRateLimitAndProcessRotation(t *testing.T) {
	now := time.Now()
	auth, err := newSetupAuth(now)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/setup/info", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	for i := 0; i < 5; i++ {
		if auth.authorize(request, now) {
			t.Fatal("missing token accepted")
		}
	}
	request.Header.Set("X-Yaginx-Setup-Token", auth.token)
	if auth.authorize(request, now) {
		t.Fatal("rate limit bypassed")
	}
	if !auth.authorize(request, now.Add(time.Minute)) {
		t.Fatal("rate limit did not reset")
	}
	rotated, err := newSetupAuth(now)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.authorize(request, now) {
		t.Fatal("prior process token accepted")
	}
}

func TestSetupApplyConsumesTokenAfterSuccess(t *testing.T) {
	auth, err := newSetupAuth(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	restarted := make(chan struct{}, 1)
	handler := setupHandlerWithApply(t.TempDir()+"/config.json", "", auth,
		func(context.Context, string, setupRequest) error { called++; return nil },
		func() { restarted <- struct{}{} })
	for i, want := range []int{http.StatusOK, http.StatusUnauthorized} {
		request := httptest.NewRequest(http.MethodPost, "/api/setup/apply", strings.NewReader(`{"host":"127.0.0.1","database":"db","username":"user","password":"secret","confirm":true,"adminEmail":"admin@example.com","adminPassword":"long-enough-password","adminPasswordConfirm":"long-enough-password"}`))
		request.RemoteAddr = "192.0.2.1:1234"
		request.Header.Set("X-Yaginx-Setup-Token", auth.token)
		record := httptest.NewRecorder()
		handler.ServeHTTP(record, request)
		if record.Code != want {
			t.Fatalf("request %d: status=%d", i, record.Code)
		}
		if strings.Contains(record.Body.String(), auth.token) || strings.Contains(record.Body.String(), "secret") {
			t.Fatal("secret leaked")
		}
	}
	if called != 1 {
		t.Fatalf("apply called %d times", called)
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("restart not requested")
	}
}
