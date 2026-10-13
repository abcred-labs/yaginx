// proxyfixturecapture records a W0 proxy exchange from the .NET golden host.
// It only targets an isolated test upstream whose JSON echo proves the
// forwarded method, Host, and path without retaining external traffic.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"yaginx-gateway/internal/contract"
)

type capturePlan struct {
	Cases []captureCase `json:"cases"`
}

type captureCase struct {
	BaseURL                  string                             `json:"baseUrl,omitempty"`
	MatrixLine               int                                `json:"matrixLine"`
	Name                     string                             `json:"name"`
	Source                   string                             `json:"source"`
	Listener                 contract.ProxyListener             `json:"listener"`
	Config                   json.RawMessage                    `json:"config"`
	Client                   captureClient                      `json:"client"`
	UpstreamAttempted        bool                               `json:"upstreamAttempted"`
	ExpectedUpstream         *contract.ProxyUpstreamObservation `json:"expectedUpstream,omitempty"`
	ResponseBodySummaryLimit int                                `json:"responseBodySummaryLimit,omitempty"`
	AllowBodyReadError       bool                               `json:"allowBodyReadError,omitempty"`
	History                  contract.ProxyHistoryObservation   `json:"history"`
}

type captureClient struct {
	Method  string            `json:"method"`
	Host    string            `json:"host"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	// ProxyProtocol selects a deliberately constrained wire preamble for a
	// listener fixture. Empty uses normal HTTP; v1 sends a synthetic RFC 7239
	// PROXY TCP4 header to the isolated golden listener.
	ProxyProtocol string `json:"proxyProtocol,omitempty"`
	// DisableRedirect records a redirect response rather than following it.
	DisableRedirect bool `json:"disableRedirect,omitempty"`
	// InsecureTLS and TLSServerName are restricted to disposable golden-host
	// capture. They allow a local self-signed certificate to be exercised with
	// its intended SNI name without trusting it system-wide.
	InsecureTLS   bool   `json:"insecureTls,omitempty"`
	TLSServerName string `json:"tlsServerName,omitempty"`
	BodyBytes     int    `json:"bodyBytes,omitempty"`
	Chunked       bool   `json:"chunked,omitempty"`
	WebSocket     bool   `json:"webSocket,omitempty"`
}

type upstreamEcho struct {
	Method     string            `json:"method"`
	Host       string            `json:"host"`
	Path       string            `json:"path"`
	Headers    map[string]string `json:"headers"`
	BodyBytes  int64             `json:"bodyBytes"`
	BodySHA256 string            `json:"bodySha256"`
}

func main() {
	baseURL := flag.String("base-url", "", "base URL of the golden proxy listener")
	planPath := flag.String("plan", "", "safe proxy capture-plan JSON")
	outDir := flag.String("out", "tests/contract/proxy", "proxy fixture output directory")
	only := flag.String("only", "", "comma-separated fixture names to capture (default all)")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout")
	flag.Parse()
	if *baseURL == "" || *planPath == "" {
		fatal("-base-url and -plan are required")
	}
	base, err := url.Parse(*baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		fatal("invalid -base-url %q", *baseURL)
	}
	contents, err := os.ReadFile(*planPath)
	if err != nil {
		fatal("read plan: %v", err)
	}
	var plan capturePlan
	if err := json.Unmarshal(contents, &plan); err != nil || len(plan.Cases) == 0 {
		fatal("parse plan: %v", err)
	}
	client := &http.Client{Timeout: *timeout}
	if err := os.MkdirAll(*outDir, 0o750); err != nil {
		fatal("create output directory: %v", err)
	}
	selected := make(map[string]struct{})
	for _, name := range strings.Split(*only, ",") {
		if name = strings.TrimSpace(name); name != "" {
			selected[name] = struct{}{}
		}
	}
	captured := 0
	for _, item := range plan.Cases {
		if len(selected) != 0 {
			if _, ok := selected[item.Name]; !ok {
				continue
			}
		}
		caseBase := base
		if item.BaseURL != "" {
			caseBase, err = parseLocalBaseURL(item.BaseURL)
			if err != nil {
				fatal("%s: %v", item.Name, err)
			}
		}
		fixture, err := capture(client, caseBase, item)
		if err != nil {
			fatal("%s: %v", item.Name, err)
		}
		if err := contract.RedactProxyFixture(&fixture); err != nil {
			fatal("%s: %v", item.Name, err)
		}
		if err := fixture.Validate(); err != nil {
			fatal("%s: unsafe fixture: %v", item.Name, err)
		}
		encoded, err := json.MarshalIndent(fixture, "", "  ")
		if err != nil {
			fatal("encode %s: %v", item.Name, err)
		}
		path := filepath.Join(*outDir, item.Name+".json")
		if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
			fatal("write %s: %v", path, err)
		}
		fmt.Printf("captured %s\n", path)
		captured++
	}
	if captured == 0 {
		fatal("-only did not match any capture-plan case")
	}
}

func parseLocalBaseURL(value string) (*url.URL, error) {
	base, err := url.Parse(value)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("invalid case baseUrl %q", value)
	}
	host := base.Hostname()
	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		return nil, fmt.Errorf("case baseUrl must target a loopback listener")
	}
	return base, nil
}

func capture(client *http.Client, base *url.URL, item captureCase) (contract.ProxyFixture, error) {
	relative, err := url.Parse(item.Client.Path)
	if err != nil || !strings.HasPrefix(item.Client.Path, "/") {
		return contract.ProxyFixture{}, fmt.Errorf("client path must be an absolute path")
	}
	requestBody, err := fixtureRequestBody(item.Client.BodyBytes)
	if err != nil {
		return contract.ProxyFixture{}, err
	}
	request, err := http.NewRequest(item.Client.Method, base.ResolveReference(relative).String(), requestBody)
	if err != nil {
		return contract.ProxyFixture{}, err
	}
	request.Host = item.Client.Host
	if item.Client.Chunked {
		if item.Client.BodyBytes == 0 {
			return contract.ProxyFixture{}, fmt.Errorf("chunked request requires bodyBytes")
		}
		request.ContentLength = -1
		request.TransferEncoding = []string{"chunked"}
	}
	for name, value := range item.Client.Headers {
		request.Header.Set(name, value)
	}
	requestClient := client
	if item.Client.DisableRedirect {
		clone := *client
		clone.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		requestClient = &clone
	}
	if item.Client.InsecureTLS || item.Client.TLSServerName != "" {
		clone := *requestClient
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: item.Client.InsecureTLS, // #nosec G402 -- disposable golden fixture only
			ServerName:         item.Client.TLSServerName,
		}
		clone.Transport = transport
		requestClient = &clone
	}
	var status int
	var headers http.Header
	var trailers http.Header
	var body []byte
	if item.Client.WebSocket {
		if item.Client.ProxyProtocol != "" || base.Scheme != "http" {
			return contract.ProxyFixture{}, fmt.Errorf("WebSocket fixture supports only a direct HTTP listener")
		}
		status, headers, body, err = captureWebSocket(base, request, client.Timeout)
	} else if item.Client.ProxyProtocol == "v1" || item.Client.ProxyProtocol == "v2" {
		status, headers, body, err = captureProxy(base, request, client.Timeout, item.Client.ProxyProtocol, item.Client.InsecureTLS, item.Client.TLSServerName)
	} else if item.Client.ProxyProtocol != "" {
		return contract.ProxyFixture{}, fmt.Errorf("unsupported proxy protocol %q", item.Client.ProxyProtocol)
	} else {
		response, requestErr := requestClient.Do(request)
		if requestErr != nil {
			return contract.ProxyFixture{}, requestErr
		}
		defer response.Body.Close()
		status = response.StatusCode
		headers = response.Header
		body, err = io.ReadAll(io.LimitReader(response.Body, 16<<20))
		if err != nil && item.AllowBodyReadError {
			err = nil
		}
		trailers = response.Trailer
	}
	if err != nil {
		return contract.ProxyFixture{}, err
	}
	var fixtureBody json.RawMessage
	var responseBodyBytes int64
	var responseBodySHA256 string
	if item.ResponseBodySummaryLimit > 0 && len(body) > item.ResponseBodySummaryLimit {
		responseBodyBytes = int64(len(body))
		digest := sha256.Sum256(body)
		responseBodySHA256 = fmt.Sprintf("%x", digest[:])
	} else {
		fixtureBody, err = encodeFixtureBody(body)
		if err != nil {
			return contract.ProxyFixture{}, err
		}
	}
	fixture := contract.ProxyFixture{
		Version: 1, MatrixLine: item.MatrixLine, Name: item.Name, Source: item.Source,
		Listener: item.Listener, Config: item.Config,
		Client:  contract.ProxyClientObservation{Method: request.Method, Host: request.Host, Path: item.Client.Path, Status: status, Headers: flattenHeaders(headers), Trailers: flattenHeaders(trailers), Body: fixtureBody, BodyBytes: responseBodyBytes, BodySHA256: responseBodySHA256},
		History: item.History,
	}
	if !item.UpstreamAttempted {
		return fixture, nil
	}
	if item.ExpectedUpstream != nil {
		if !item.ExpectedUpstream.Attempted {
			return contract.ProxyFixture{}, fmt.Errorf("expectedUpstream must set attempted=true")
		}
		fixture.Upstream = *item.ExpectedUpstream
		if fixture.Upstream.Status == 0 {
			fixture.Upstream.Status = status
		}
		return fixture, nil
	}
	var upstream upstreamEcho
	if err := json.Unmarshal(body, &upstream); err != nil {
		return contract.ProxyFixture{}, fmt.Errorf("parse isolated upstream echo: %w", err)
	}
	fixture.Upstream = contract.ProxyUpstreamObservation{Attempted: true, Method: upstream.Method, Host: upstream.Host, Path: upstream.Path, Status: status, Headers: upstream.Headers, BodyBytes: upstream.BodyBytes, BodySHA256: upstream.BodySHA256}
	return fixture, nil
}

func captureWebSocket(base *url.URL, request *http.Request, timeout time.Duration) (int, http.Header, []byte, error) {
	connection, err := net.DialTimeout("tcp", base.Host, timeout)
	if err != nil {
		return 0, nil, nil, err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(timeout)); err != nil {
		return 0, nil, nil, err
	}
	key := []byte("yaginx-w0-wskey!")
	if _, err := fmt.Fprintf(connection, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Protocol: fixture-protocol\r\n\r\n", request.URL.RequestURI(), request.Host, base64.StdEncoding.EncodeToString(key)); err != nil {
		return 0, nil, nil, err
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), request)
	if err != nil {
		return 0, nil, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusSwitchingProtocols {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return response.StatusCode, response.Header, body, readErr
	}
	const message = "fixture-websocket"
	if err := writeMaskedWebSocketText(connection, []byte(message)); err != nil {
		return 0, nil, nil, err
	}
	payload, err := readWebSocketText(bufio.NewReader(connection))
	if err != nil {
		return 0, nil, nil, err
	}
	body, err := json.Marshal(map[string]string{"sent": message, "received": string(payload), "subprotocol": response.Header.Get("Sec-WebSocket-Protocol")})
	if err != nil {
		return 0, nil, nil, err
	}
	return response.StatusCode, response.Header, body, nil
}

func writeMaskedWebSocketText(writer io.Writer, payload []byte) error {
	if len(payload) > 125 {
		return fmt.Errorf("fixture WebSocket payload is too large")
	}
	mask := [4]byte{0x10, 0x20, 0x30, 0x40}
	header := []byte{0x81, 0x80 | byte(len(payload)), mask[0], mask[1], mask[2], mask[3]}
	masked := make([]byte, len(payload))
	for index, value := range payload {
		masked[index] = value ^ mask[index%len(mask)]
	}
	if _, err := writer.Write(header); err != nil {
		return err
	}
	_, err := writer.Write(masked)
	return err
}

func readWebSocketText(reader io.Reader) ([]byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	if header[0] != 0x81 || header[1]&0x80 != 0 || header[1]&0x7f > 125 {
		return nil, fmt.Errorf("unsupported fixture WebSocket response frame")
	}
	payload := make([]byte, int(header[1]&0x7f))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func fixtureRequestBody(size int) (io.Reader, error) {
	if size < 0 || size > 8<<20 {
		return nil, fmt.Errorf("bodyBytes must be between 0 and %d", 8<<20)
	}
	if size == 0 {
		return nil, nil
	}
	pattern := []byte("yaginx-w0-stream-fixture-")
	body := make([]byte, size)
	for offset := 0; offset < len(body); {
		offset += copy(body[offset:], pattern)
	}
	return bytes.NewReader(body), nil
}

func captureProxy(base *url.URL, request *http.Request, timeout time.Duration, protocol string, insecureTLS bool, tlsServerName string) (int, http.Header, []byte, error) {
	if base.Scheme != "http" && base.Scheme != "https" {
		return 0, nil, nil, fmt.Errorf("PROXY fixture requires an HTTP or HTTPS listener")
	}
	address := base.Host
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(base.Hostname(), "80")
	}
	connection, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return 0, nil, nil, err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(timeout)); err != nil {
		return 0, nil, nil, err
	}
	port := "80"
	if _, parsedPort, err := net.SplitHostPort(address); err == nil {
		port = parsedPort
	}
	header, err := proxyHeader(protocol, port)
	if err != nil {
		return 0, nil, nil, err
	}
	if _, err := connection.Write(header); err != nil {
		return 0, nil, nil, err
	}
	var wire net.Conn = connection
	if base.Scheme == "https" {
		secureConnection := tls.Client(connection, &tls.Config{
			InsecureSkipVerify: insecureTLS, // #nosec G402 -- disposable golden fixture only
			ServerName:         tlsServerName,
		})
		if err := secureConnection.Handshake(); err != nil {
			return 0, nil, nil, err
		}
		wire = secureConnection
	}
	request.Close = true
	if err := request.Write(wire); err != nil {
		return 0, nil, nil, err
	}
	response, err := http.ReadResponse(bufio.NewReader(wire), request)
	if err != nil {
		return 0, nil, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return 0, nil, nil, err
	}
	return response.StatusCode, response.Header, body, nil
}

func proxyHeader(protocol, destinationPort string) ([]byte, error) {
	switch protocol {
	case "v1":
		return []byte(proxyV1Header(destinationPort)), nil
	case "v2":
		port, err := strconv.ParseUint(destinationPort, 10, 16)
		if err != nil || port == 0 {
			return nil, fmt.Errorf("invalid PROXY destination port %q", destinationPort)
		}
		// PROXY protocol v2: signature, version 2/PROXY command, TCPv4/STREAM,
		// and a 12-byte IPv4 address-and-port payload.
		return []byte{
			0x0d, 0x0a, 0x0d, 0x0a, 0x00, 0x0d, 0x0a, 0x51, 0x55, 0x49, 0x54, 0x0a,
			0x21, 0x11, 0x00, 0x0c,
			198, 51, 100, 11, 127, 0, 0, 1,
			0x10, 0x93, byte(port >> 8), byte(port),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported proxy protocol %q", protocol)
	}
}

func proxyV1Header(destinationPort string) string {
	return "PROXY TCP4 198.51.100.10 127.0.0.1 4242 " + destinationPort + "\r\n"
}

// encodeFixtureBody keeps JSON responses structurally comparable. Other
// UTF-8 proxy responses are represented as JSON strings so a fixture remains
// valid JSON while retaining the exact text. Binary response capture is not
// enabled by the safe W0 plan and must add an explicit base64 encoding before
// it can be captured rather than silently replacing bytes.
func encodeFixtureBody(body []byte) (json.RawMessage, error) {
	if json.Valid(body) {
		return json.RawMessage(body), nil
	}
	if !utf8.Valid(body) {
		return nil, fmt.Errorf("non-UTF-8 proxy response requires an explicit binary fixture encoding")
	}
	encoded, err := json.Marshal(string(body))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func flattenHeaders(headers http.Header) map[string]string {
	result := make(map[string]string, len(headers))
	for name, values := range headers {
		result[name] = strings.Join(values, ", ")
	}
	return result
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "proxyfixturecapture: "+format+"\n", args...)
	os.Exit(1)
}
