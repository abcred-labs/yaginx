package listeners

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
)

func TestStartLogsUntrustedProxySource(t *testing.T) {
	addresses := testAddresses(t)
	var logs lockedBuffer
	running, err := Start(Config{
		HTTP: addresses.http, ProxyHTTP: addresses.proxyHTTP, HTTPS: addresses.https, ProxyHTTPS: addresses.proxyHTTPS,
		TrustedCIDRs: []string{"192.0.2.0/24"}, RequireProxyProtocol: true, HeaderTimeout: time.Second, TLS: testTLSConfig(t),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}),
		Logger:  slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close(context.Background()) }()

	connection, err := net.DialTimeout("tcp", addresses.proxyHTTP, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.WriteString(connection, "PROXY TCP4 198.51.100.99 127.0.0.1 12345 80\r\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Read(make([]byte, 1)); err == nil {
		t.Fatal("untrusted PROXY connection unexpectedly remained open")
	}

	tlsConnection, err := net.DialTimeout("tcp", addresses.proxyHTTPS, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer tlsConnection.Close()
	_ = tlsConnection.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.WriteString(tlsConnection, "PROXY TCP4 198.51.100.99 127.0.0.1 12345 443\r\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := tlsConnection.Read(make([]byte, 1)); err == nil {
		t.Fatal("untrusted PROXY TLS connection unexpectedly remained open")
	}

	logOutput := logs.String()
	if !bytes.Contains([]byte(logOutput), []byte(`"msg":"reject PROXY connection from untrusted source"`)) {
		t.Fatalf("untrusted source rejection was not logged: %s", logOutput)
	}
	if !bytes.Contains([]byte(logOutput), []byte(`"listener":"proxy_http"`)) {
		t.Fatalf("listener was not logged: %s", logOutput)
	}
	if !bytes.Contains([]byte(logOutput), []byte(`"source_ip":"127.0.0.1"`)) {
		t.Fatalf("TCP source IP was not logged: %s", logOutput)
	}
	if !bytes.Contains([]byte(logOutput), []byte(`"listener":"proxy_https"`)) {
		t.Fatalf("TLS listener was not logged: %s", logOutput)
	}
	if bytes.Contains([]byte(logOutput), []byte("198.51.100.99")) {
		t.Fatalf("untrusted PROXY Header client IP was logged: %s", logOutput)
	}
}

func TestOptionalProxyListenerPreservesDirectHTTPAndLogsHeaderRejection(t *testing.T) {
	addresses := testAddresses(t)
	var logs lockedBuffer
	running, err := Start(Config{
		HTTP: addresses.http, ProxyHTTP: addresses.proxyHTTP, HTTPS: addresses.https, ProxyHTTPS: addresses.proxyHTTPS,
		TrustedCIDRs: []string{"192.0.2.0/24"}, HeaderTimeout: time.Second, TLS: testTLSConfig(t),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }),
		Logger:  slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close(context.Background()) }()

	response, err := http.Get("http://" + addresses.proxyHTTP + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("direct HTTP status=%d body=%q", response.StatusCode, body)
	}
	if logs.String() != "" {
		t.Fatalf("direct HTTP request was logged as a rejected PROXY connection: %s", logs.String())
	}

	connection, err := net.DialTimeout("tcp", addresses.proxyHTTP, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.WriteString(connection, "PROXY TCP4 198.51.100.99 127.0.0.1 12345 80\r\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !bytes.Contains([]byte(logs.String()), []byte(`"source_ip":"127.0.0.1"`)) {
		time.Sleep(time.Millisecond)
	}

	logOutput := logs.String()
	if !bytes.Contains([]byte(logOutput), []byte(`"listener":"proxy_http"`)) {
		t.Fatalf("listener was not logged: %s", logOutput)
	}
	if !bytes.Contains([]byte(logOutput), []byte(`"source_ip":"127.0.0.1"`)) {
		t.Fatalf("TCP source IP was not logged: %s", logOutput)
	}
	if bytes.Contains([]byte(logOutput), []byte("198.51.100.99")) {
		t.Fatalf("untrusted PROXY Header client IP was logged: %s", logOutput)
	}
}

func TestStartAcceptsProxyV1V2AndTLS(t *testing.T) {
	addresses := testAddresses(t)
	running, err := Start(Config{
		HTTP: addresses.http, ProxyHTTP: addresses.proxyHTTP, HTTPS: addresses.https, ProxyHTTPS: addresses.proxyHTTPS,
		TrustedCIDRs: []string{"127.0.0.0/8"}, RequireProxyProtocol: true, HeaderTimeout: time.Second, TLS: testTLSConfig(t),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) { _, _ = io.WriteString(w, request.RemoteAddr) }),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close(context.Background()) }()

	if got := proxyV1Request(t, addresses.proxyHTTP); got != "198.51.100.10:12345" {
		t.Fatalf("PROXY v1 remote=%q", got)
	}
	if got := proxyV2Request(t, addresses.proxyHTTP); got != "198.51.100.11:12346" {
		t.Fatalf("PROXY v2 remote=%q", got)
	}
	if got := proxyV1TLSRequest(t, addresses.proxyHTTPS); got != "198.51.100.12:12347" {
		t.Fatalf("TLS PROXY v1 remote=%q", got)
	}
	assertDirectConnectionRejected(t, addresses.proxyHTTP, false)
	assertDirectConnectionRejected(t, addresses.proxyHTTPS, true)
}

func TestStartServesHTTP3OnlyOnConfiguredUDPAddress(t *testing.T) {
	var addresses listenerAddresses
	var running *Running
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		addresses = testAddresses(t)
		running, err = Start(Config{
			HTTP: addresses.http, ProxyHTTP: addresses.proxyHTTP, HTTPS: addresses.https, ProxyHTTPS: addresses.proxyHTTPS, HTTP3: addresses.http3,
			TrustedCIDRs: []string{"127.0.0.0/8"}, RequireProxyProtocol: true, HeaderTimeout: time.Second, TLS: testTLSConfig(t),
			Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "h3") }),
		})
		if err == nil || !strings.Contains(err.Error(), "address already in use") {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close(context.Background()) }()

	transport := &http3.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h3"}}} // #nosec G402 -- generated test certificate
	defer transport.Close()
	client := &http.Client{Transport: transport}
	var response *http.Response
	var responseCancel context.CancelFunc
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		requestContext, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		request, requestErr := http.NewRequestWithContext(requestContext, http.MethodGet, "https://"+addresses.http3+"/", nil)
		if requestErr != nil {
			cancel()
			t.Fatal(requestErr)
		}
		response, err = client.Do(request)
		if err == nil {
			responseCancel = cancel
			break
		}
		cancel()
		transport.CloseIdleConnections()
	}
	if err != nil {
		t.Fatalf("HTTP/3 request: %v", err)
	}
	defer responseCancel()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.ProtoMajor != 3 || string(body) != "h3" {
		t.Fatalf("HTTP/3 response proto=%s body=%q", response.Proto, body)
	}
	// 9443-equivalent is TCP + PROXY only. It must not claim a UDP socket.
	packet, err := net.ListenPacket("udp", addresses.proxyHTTPS)
	if err != nil {
		t.Fatalf("proxy HTTPS unexpectedly owns UDP: %v", err)
	}
	_ = packet.Close()
}

func TestStartRejectsOccupiedHTTP3UDPAddress(t *testing.T) {
	addresses := testAddresses(t)
	packet, err := net.ListenPacket("udp", addresses.http3)
	if err != nil {
		t.Fatal(err)
	}
	defer packet.Close()

	_, err = Start(Config{
		HTTP: addresses.http, ProxyHTTP: addresses.proxyHTTP, HTTPS: addresses.https, ProxyHTTPS: addresses.proxyHTTPS, HTTP3: addresses.http3,
		TrustedCIDRs: []string{"127.0.0.0/8"}, RequireProxyProtocol: true, HeaderTimeout: time.Second, TLS: testTLSConfig(t),
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	if err == nil {
		t.Fatal("Start accepted an occupied HTTP/3 UDP address")
	}
}

func TestStartAppliesConfiguredLimits(t *testing.T) {
	addresses := testAddresses(t)
	running, err := Start(Config{
		HTTP: addresses.http, ProxyHTTP: addresses.proxyHTTP, HTTPS: addresses.https, ProxyHTTPS: addresses.proxyHTTPS,
		TrustedCIDRs: []string{"127.0.0.0/8"}, RequireProxyProtocol: true, HeaderTimeout: time.Second, ActivityTimeout: 3 * time.Second, MaxRequestHeaderBytes: 8192, MaxRequestBodyBytes: 3, TLS: testTLSConfig(t),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) { _, _ = io.Copy(w, request.Body) }),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close(context.Background()) }()
	if len(running.servers) != 4 || running.servers[0].IdleTimeout != 3*time.Second || running.servers[0].MaxHeaderBytes != 8192 {
		t.Fatalf("server limits=%#v", running.servers)
	}
	if running.servers[2].TLSConfig == nil || !containsString(running.servers[2].TLSConfig.NextProtos, "h2") {
		t.Fatalf("TLS listener did not advertise HTTP/2: %#v", running.servers[2].TLSConfig)
	}
	response, err := http.Post("http://"+addresses.http+"/", "text/plain", bytes.NewReader([]byte("1234")))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("body limit status=%d", response.StatusCode)
	}
}

func TestSlowTLSHandshakeDoesNotBlockAcceptLoop(t *testing.T) {
	addresses := testAddresses(t)
	running, err := Start(Config{
		HTTP: addresses.http, ProxyHTTP: addresses.proxyHTTP, HTTPS: addresses.https, ProxyHTTPS: addresses.proxyHTTPS,
		TrustedCIDRs: []string{"127.0.0.0/8"}, HeaderTimeout: time.Second, TLSHandshakeTimeout: 2 * time.Second,
		TLS: testTLSConfig(t), Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close(context.Background()) }()

	stalled, err := net.DialTimeout("tcp", addresses.https, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer stalled.Close()

	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // #nosec G402 -- generated test certificate
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	done := make(chan error, 1)
	go func() {
		response, requestErr := client.Get("https://" + addresses.https + "/")
		if requestErr != nil {
			done <- requestErr
			return
		}
		defer response.Body.Close()
		_, _ = io.ReadAll(response.Body)
		if response.StatusCode != http.StatusOK {
			done <- fmt.Errorf("status=%d", response.StatusCode)
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("a stalled TLS handshake blocked the accept loop")
	}
}

func TestTLSHandshakeBudgetRejectsExcessConnections(t *testing.T) {
	addresses := testAddresses(t)
	rejected := make(chan string, 1)
	running, err := Start(Config{
		HTTP: addresses.http, ProxyHTTP: addresses.proxyHTTP, HTTPS: addresses.https, ProxyHTTPS: addresses.proxyHTTPS,
		TrustedCIDRs: []string{"127.0.0.0/8"}, HeaderTimeout: time.Second, MaxTLSHandshakes: 1,
		TLS: testTLSConfig(t), Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		ObserveTLSHandshakeRejection: func(listener, reason string) {
			select {
			case rejected <- listener + ":" + reason:
			default:
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close(context.Background()) }()

	stalled, err := net.DialTimeout("tcp", addresses.https, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer stalled.Close()
	excess, err := net.DialTimeout("tcp", addresses.https, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer excess.Close()

	select {
	case got := <-rejected:
		if got != "https:limit" {
			t.Fatalf("rejection=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("excess TLS connection was not rejected")
	}
}

func TestStartAppliesHTTP3StreamLimits(t *testing.T) {
	addresses := testAddresses(t)
	running, err := Start(Config{
		HTTP: addresses.http, ProxyHTTP: addresses.proxyHTTP, HTTPS: addresses.https, ProxyHTTPS: addresses.proxyHTTPS, HTTP3: addresses.http3,
		TrustedCIDRs: []string{"127.0.0.0/8"}, HeaderTimeout: time.Second, MaxConcurrentRequests: 32, TLS: testTLSConfig(t),
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close(context.Background()) }()
	if running.h3 == nil || running.h3.QUICConfig == nil {
		t.Fatal("HTTP/3 server was not configured")
	}
	if running.h3.QUICConfig.MaxIncomingStreams != 32 || running.h3.QUICConfig.MaxIncomingUniStreams != 16 {
		t.Fatalf("QUIC stream limits=%#v", running.h3.QUICConfig)
	}
	if running.h3.QUICConfig.MaxStreamReceiveWindow != 4<<20 || running.h3.QUICConfig.MaxConnectionReceiveWindow != 16<<20 {
		t.Fatalf("QUIC flow-control limits=%#v", running.h3.QUICConfig)
	}
}

func TestStartSharesConcurrentRequestLimitAcrossListeners(t *testing.T) {
	addresses := testAddresses(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	running, err := Start(Config{
		HTTP: addresses.http, ProxyHTTP: addresses.proxyHTTP, HTTPS: addresses.https, ProxyHTTPS: addresses.proxyHTTPS,
		TrustedCIDRs: []string{"127.0.0.0/8"}, HeaderTimeout: time.Second,
		MaxConcurrentRequests: 1, TLS: testTLSConfig(t),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(entered)
			<-release
			_, _ = io.WriteString(w, "ok")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close(context.Background()) }()

	firstDone := make(chan *http.Response, 1)
	go func() {
		response, requestErr := http.Get("http://" + addresses.http + "/")
		if requestErr != nil {
			firstDone <- nil
			return
		}
		firstDone <- response
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first request did not enter handler")
	}

	second, err := http.Get("http://" + addresses.proxyHTTP + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Body.Close()
	if second.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("second status=%d, want 503", second.StatusCode)
	}
	close(release)
	first := <-firstDone
	if first == nil {
		t.Fatal("first request failed")
	}
	defer first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first status=%d", first.StatusCode)
	}
}

func TestStartPreservesBodyLimitWithRequestTracking(t *testing.T) {
	addresses := testAddresses(t)
	running, err := Start(Config{
		HTTP: addresses.http, ProxyHTTP: addresses.proxyHTTP, HTTPS: addresses.https, ProxyHTTPS: addresses.proxyHTTPS,
		TrustedCIDRs: []string{"127.0.0.0/8"}, RequireProxyProtocol: true, HeaderTimeout: time.Second, MaxRequestBodyBytes: 3, TLS: testTLSConfig(t),
		Handler:      http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) { _, _ = io.Copy(w, request.Body) }),
		TrackRequest: func(_, _ string) func() { return func() {} },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close(context.Background()) }()
	response, err := http.Post("http://"+addresses.http+"/", "text/plain", bytes.NewReader([]byte("1234")))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("body limit with tracking status=%d", response.StatusCode)
	}
}

type listenerAddresses struct{ http, proxyHTTP, https, proxyHTTPS, http3 string }

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func testAddresses(t *testing.T) listenerAddresses {
	t.Helper()
	address := func() string {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		address := listener.Addr().String()
		// Some assertions intentionally bind UDP on a TCP listener's port.
		// Verify that the randomly selected TCP port is also currently available
		// for UDP so concurrent package tests cannot make this test flaky.
		packet, err := net.ListenPacket("udp", address)
		if err != nil {
			t.Fatal(err)
		}
		_ = packet.Close()
		return address
	}
	return listenerAddresses{http: address(), proxyHTTP: address(), https: address(), proxyHTTPS: address(), http3: address()}
}

func proxyV1Request(t *testing.T, address string) string {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, _ = io.WriteString(connection, "PROXY TCP4 198.51.100.10 127.0.0.1 12345 80\r\nGET / HTTP/1.1\r\nHost: listener.test\r\nConnection: close\r\n\r\n")
	return readSocketResponse(t, connection)
}

func proxyV2Request(t *testing.T, address string) string {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	header := []byte("\r\n\r\n\x00\r\nQUIT\n")
	header = append(header, 0x21, 0x11, 0x00, 0x0c, 198, 51, 100, 11, 127, 0, 0, 1)
	ports := make([]byte, 4)
	binary.BigEndian.PutUint16(ports[:2], 12346)
	binary.BigEndian.PutUint16(ports[2:], 80)
	header = append(header, ports...)
	header = append(header, []byte("GET / HTTP/1.1\r\nHost: listener.test\r\nConnection: close\r\n\r\n")...)
	if _, err := connection.Write(header); err != nil {
		t.Fatal(err)
	}
	return readSocketResponse(t, connection)
}

func proxyV1TLSRequest(t *testing.T, address string) string {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := io.WriteString(connection, "PROXY TCP4 198.51.100.12 127.0.0.1 12347 443\r\n"); err != nil {
		t.Fatal(err)
	}
	secure := tls.Client(connection, &tls.Config{InsecureSkipVerify: true, ServerName: "listener.test"}) // #nosec G402 -- generated test certificate
	if err := secure.Handshake(); err != nil {
		t.Fatal(err)
	}
	defer secure.Close()
	_, _ = io.WriteString(secure, "GET / HTTP/1.1\r\nHost: listener.test\r\nConnection: close\r\n\r\n")
	return readSocketResponse(t, secure)
}

func readSocketResponse(t *testing.T, reader io.Reader) string {
	t.Helper()
	response, err := http.ReadResponse(bufio.NewReader(reader), httptest.NewRequest(http.MethodGet, "http://listener.test/", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func assertDirectConnectionRejected(t *testing.T, address string, useTLS bool) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if useTLS {
		secure := tls.Client(connection, &tls.Config{InsecureSkipVerify: true, ServerName: "listener.test"}) // #nosec G402 -- generated test certificate
		if err := secure.Handshake(); err == nil {
			t.Fatal("direct TLS connection unexpectedly completed")
		}
		return
	}
	if _, err := io.WriteString(connection, "GET / HTTP/1.1\r\nHost: listener.test\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1)
	if _, err := connection.Read(buffer); err == nil {
		t.Fatalf("direct HTTP connection unexpectedly received %q", buffer)
	}
}

func testTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "listener.test"}, DNSNames: []string{"listener.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate := new(bytes.Buffer)
	_ = pem.Encode(certificate, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	encodedKey := new(bytes.Buffer)
	_ = pem.Encode(encodedKey, &pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})
	pair, err := tls.X509KeyPair(certificate.Bytes(), encodedKey.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(value)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
