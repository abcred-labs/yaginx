// proxyfixtureupstream is an isolated synthetic upstream for proxy verification
// and pre-production load tests. It reflects the received request metadata as
// JSON so forwarding behavior can be checked without recording real traffic.
package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type observation struct {
	Method     string            `json:"method"`
	Host       string            `json:"host"`
	Path       string            `json:"path"`
	Headers    map[string]string `json:"headers"`
	BodyBytes  int64             `json:"bodyBytes"`
	BodySHA256 string            `json:"bodySha256"`
}

func main() {
	address := flag.String("listen", "127.0.0.1:20180", "isolated upstream listen address")
	mode := flag.String("mode", "echo", "echo, forward-proxy, tls, raw-close, or raw-short-body")
	flag.Parse()
	var handler http.Handler
	switch *mode {
	case "echo", "tls":
		handler = echoHandler()
	case "forward-proxy":
		handler = forwardProxyHandler()
	case "raw-close", "raw-short-body":
		log.Printf("proxy fixture %s upstream listening on %s", *mode, *address)
		log.Fatal(runRawUpstream(*address, *mode))
	default:
		log.Fatalf("unsupported mode %q", *mode)
	}
	server := &http.Server{Addr: *address, Handler: handler}
	if *mode == "tls" {
		certificate, err := selfSignedCertificate()
		if err != nil {
			log.Fatal(err)
		}
		listener, err := tls.Listen("tcp", *address, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("proxy fixture TLS upstream listening on %s", *address)
		log.Fatal(server.Serve(listener))
	}
	log.Printf("proxy fixture %s upstream listening on %s", *mode, *address)
	log.Fatal(server.ListenAndServe())
}

func runRawUpstream(address, mode string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	for {
		connection, err := listener.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer connection.Close()
			_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
			reader := bufio.NewReader(connection)
			for {
				line, err := reader.ReadString('\n')
				if err != nil || line == "\r\n" {
					break
				}
			}
			if mode == "raw-close" {
				return
			}
			_, _ = io.WriteString(connection, "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 64\r\nConnection: close\r\n\r\nshort-body")
		}()
	}
}

func echoHandler() http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/perf/") {
			handlePerformanceFixture(response, request)
			return
		}
		bodyBytes, bodySHA256, err := requestBodySummary(request.Body)
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		observed := observation{
			Method: request.Method,
			Host:   request.Host,
			Path:   request.URL.RequestURI(),
			Headers: map[string]string{
				"X-Forwarded-For":         request.Header.Get("X-Forwarded-For"),
				"X-Forwarded-Host":        request.Header.Get("X-Forwarded-Host"),
				"X-Forwarded-Proto":       request.Header.Get("X-Forwarded-Proto"),
				"X-Forwarded-Prefix":      request.Header.Get("X-Forwarded-Prefix"),
				"Forwarded":               request.Header.Get("Forwarded"),
				"X-Fixture":               request.Header.Get("X-Fixture"),
				"X-Fixture-Forward-Proxy": request.Header.Get("X-Fixture-Forward-Proxy"),
			},
			BodyBytes:  bodyBytes,
			BodySHA256: bodySHA256,
		}
		if request.URL.Path == "/websocket" {
			handleWebSocket(response, request, 1)
			return
		}
		if request.URL.Path == "/sse" {
			response.Header().Set("Content-Type", "text/event-stream")
			response.Header().Set("Cache-Control", "no-cache")
			encoded, _ := json.Marshal(observed)
			_, _ = fmt.Fprintf(response, "event: fixture\ndata: %s\n\n", encoded)
			if flusher, ok := response.(http.Flusher); ok {
				flusher.Flush()
			}
			return
		}
		if request.URL.Path == "/large-download" {
			response.Header().Set("Content-Type", "application/octet-stream")
			response.Header().Set("X-Fixture-Body-Bytes", "1048576")
			chunk := bytes.Repeat([]byte("yaginx-w0-download-fixture-"), 1024)
			for remaining := 1048576; remaining > 0; {
				write := chunk
				if len(write) > remaining {
					write = write[:remaining]
				}
				if _, err := response.Write(write); err != nil {
					return
				}
				remaining -= len(write)
			}
			return
		}
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/trailers" {
			response.Header().Set("Trailer", "X-Fixture-Trailer")
		}
		// Test-only status paths let the capture plan prove upstream 4xx/5xx
		// history classification without contacting a real service.
		if text, found := strings.CutPrefix(request.URL.Path, "/status/"); found {
			if status, err := strconv.Atoi(text); err == nil && status >= 100 && status <= 599 {
				response.WriteHeader(status)
			}
		}
		_ = json.NewEncoder(response).Encode(observed)
		if request.URL.Path == "/trailers" {
			response.Header().Set("X-Fixture-Trailer", "upstream")
		}
	})
}

func handleWebSocket(response http.ResponseWriter, request *http.Request, echoes int) {
	if !strings.EqualFold(request.Header.Get("Upgrade"), "websocket") || !strings.Contains(strings.ToLower(request.Header.Get("Connection")), "upgrade") {
		http.Error(response, "fixture WebSocket upgrade required", http.StatusBadRequest)
		return
	}
	key := request.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(response, "fixture WebSocket key required", http.StatusBadRequest)
		return
	}
	hijacker, ok := response.(http.Hijacker)
	if !ok {
		http.Error(response, "fixture server cannot hijack", http.StatusInternalServerError)
		return
	}
	connection, buffer, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer connection.Close()
	digest := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if _, err := fmt.Fprintf(buffer, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: %s\r\nSec-WebSocket-Protocol: fixture-protocol\r\n\r\n", base64.StdEncoding.EncodeToString(digest[:])); err != nil {
		return
	}
	if err := buffer.Flush(); err != nil {
		return
	}
	for range echoes {
		payload, err := readMaskedWebSocketText(buffer)
		if err != nil {
			return
		}
		if err := writeWebSocketText(connection, payload); err != nil {
			return
		}
	}
}

const (
	maxPerformanceBytes  int64 = 1 << 30
	maxPerformanceEvents       = 10_000
)

// handlePerformanceFixture exposes deterministic, opt-in load-test endpoints.
// They are deliberately isolated below /perf/ so the W0 capture behavior of
// the ordinary echo endpoints remains unchanged.
func handlePerformanceFixture(response http.ResponseWriter, request *http.Request) {
	path := request.URL.Path
	switch {
	case strings.HasPrefix(path, "/perf/fixed/"):
		if request.Method != http.MethodGet {
			http.Error(response, "GET required", http.StatusMethodNotAllowed)
			return
		}
		bytes, ok := performanceBytes(response, strings.TrimPrefix(path, "/perf/fixed/"))
		if !ok {
			return
		}
		response.Header().Set("Content-Type", "application/octet-stream")
		response.Header().Set("Content-Length", strconv.FormatInt(bytes, 10))
		_ = writeRepeated(response, bytes, 'x')
	case strings.HasPrefix(path, "/perf/echo/"):
		if request.Method != http.MethodPost {
			http.Error(response, "POST required", http.StatusMethodNotAllowed)
			return
		}
		bytes, ok := performanceBytes(response, strings.TrimPrefix(path, "/perf/echo/"))
		if !ok {
			return
		}
		if _, err := io.Copy(io.Discard, request.Body); err != nil {
			http.Error(response, "read request body", http.StatusBadRequest)
			return
		}
		if bytes < int64(len(`{"payload":""}`)) {
			http.Error(response, "JSON response must be at least 14 bytes", http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Content-Length", strconv.FormatInt(bytes, 10))
		_, _ = io.WriteString(response, `{"payload":"`)
		_ = writeRepeated(response, bytes-int64(len(`{"payload":""}`)), 'x')
		_, _ = io.WriteString(response, `"}`)
	case strings.HasPrefix(path, "/perf/download/"):
		if request.Method != http.MethodGet {
			http.Error(response, "GET required", http.StatusMethodNotAllowed)
			return
		}
		bytes, ok := performanceBytes(response, strings.TrimPrefix(path, "/perf/download/"))
		if !ok {
			return
		}
		response.Header().Set("Content-Type", "application/octet-stream")
		response.Header().Set("Content-Length", strconv.FormatInt(bytes, 10))
		_ = writeRepeated(response, bytes, 'd')
	case path == "/perf/sse":
		handlePerformanceSSE(response, request)
	case path == "/perf/websocket":
		echoes, ok := performancePositiveQuery(response, request, "echoes", 1, maxPerformanceEvents)
		if !ok {
			return
		}
		handleWebSocket(response, request, echoes)
	case path == "/perf/delay":
		handlePerformanceDelay(response, request)
	default:
		http.NotFound(response, request)
	}
}

func performanceBytes(response http.ResponseWriter, value string) (int64, bool) {
	bytes, err := strconv.ParseInt(value, 10, 64)
	if err != nil || bytes < 0 || bytes > maxPerformanceBytes {
		http.Error(response, "bytes must be between 0 and 1073741824", http.StatusBadRequest)
		return 0, false
	}
	return bytes, true
}

func performancePositiveQuery(response http.ResponseWriter, request *http.Request, key string, fallback, maximum int) (int, bool) {
	value := request.URL.Query().Get(key)
	if value == "" {
		return fallback, true
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > maximum {
		http.Error(response, fmt.Sprintf("%s must be between 1 and %d", key, maximum), http.StatusBadRequest)
		return 0, false
	}
	return parsed, true
}

func handlePerformanceSSE(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(response, "GET required", http.StatusMethodNotAllowed)
		return
	}
	events, ok := performancePositiveQuery(response, request, "events", 1, maxPerformanceEvents)
	if !ok {
		return
	}
	payloadBytes, ok := performancePositiveQuery(response, request, "payloadBytes", 1024, 64*1024)
	if !ok {
		return
	}
	interval := time.Second
	if value := request.URL.Query().Get("interval"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed < time.Millisecond || parsed > time.Minute {
			http.Error(response, "interval must be between 1ms and 1m", http.StatusBadRequest)
			return
		}
		interval = parsed
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		http.Error(response, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	payload := strings.Repeat("s", payloadBytes)
	for event := 0; event < events; event++ {
		if _, err := fmt.Fprintf(response, "event: perf\\ndata: %s\\n\\n", payload); err != nil {
			return
		}
		flusher.Flush()
		if event+1 < events {
			select {
			case <-request.Context().Done():
				return
			case <-time.After(interval):
			}
		}
	}
}

func handlePerformanceDelay(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(response, "GET required", http.StatusMethodNotAllowed)
		return
	}
	delay := time.Second
	if value := request.URL.Query().Get("duration"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed < time.Millisecond || parsed > 5*time.Minute {
			http.Error(response, "duration must be between 1ms and 5m", http.StatusBadRequest)
			return
		}
		delay = parsed
	}
	select {
	case <-request.Context().Done():
		return
	case <-time.After(delay):
	}
	response.Header().Set("Content-Type", "application/octet-stream")
	response.Header().Set("Content-Length", "1024")
	_ = writeRepeated(response, 1024, 't')
}

func writeRepeated(writer io.Writer, count int64, value byte) error {
	chunk := bytes.Repeat([]byte{value}, 32*1024)
	for count > 0 {
		write := int64(len(chunk))
		if write > count {
			write = count
		}
		if _, err := writer.Write(chunk[:int(write)]); err != nil {
			return err
		}
		count -= write
	}
	return nil
}

func readMaskedWebSocketText(reader *bufio.ReadWriter) ([]byte, error) {
	header, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	lengthByte, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if header != 0x81 || lengthByte&0x80 == 0 || lengthByte&0x7f > 125 {
		return nil, fmt.Errorf("unsupported fixture WebSocket request frame")
	}
	var mask [4]byte
	if _, err := io.ReadFull(reader, mask[:]); err != nil {
		return nil, err
	}
	payload := make([]byte, int(lengthByte&0x7f))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	for index := range payload {
		payload[index] ^= mask[index%len(mask)]
	}
	return payload, nil
}

func writeWebSocketText(writer io.Writer, payload []byte) error {
	if len(payload) > 125 {
		return fmt.Errorf("fixture WebSocket payload is too large")
	}
	if _, err := writer.Write([]byte{0x81, byte(len(payload))}); err != nil {
		return err
	}
	_, err := writer.Write(payload)
	return err
}

func requestBodySummary(body io.Reader) (int64, string, error) {
	digest := sha256.New()
	bytes, err := io.Copy(digest, body)
	if err != nil {
		return 0, "", err
	}
	if bytes == 0 {
		return 0, "", nil
	}
	return bytes, fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func forwardProxyHandler() http.Handler {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodConnect {
			http.Error(response, "CONNECT is outside this HTTP-only fixture", http.StatusNotImplemented)
			return
		}
		outbound := request.Clone(request.Context())
		outbound.RequestURI = ""
		outbound.Host = ""
		outbound.Header = request.Header.Clone()
		outbound.Header.Set("X-Fixture-Forward-Proxy", "used")
		upstream, err := transport.RoundTrip(outbound)
		if err != nil {
			http.Error(response, fmt.Sprintf("fixture forward proxy: %v", err), http.StatusBadGateway)
			return
		}
		defer upstream.Body.Close()
		for name, values := range upstream.Header {
			response.Header()[name] = append([]string(nil), values...)
		}
		response.WriteHeader(upstream.StatusCode)
		_, _ = io.Copy(response, upstream.Body)
	})
}

func selfSignedCertificate() (tls.Certificate, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "proxy-fixture.invalid"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{certificateDER}, PrivateKey: privateKey}, nil
}
