package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPerformanceFixtureFixedAndEchoBodiesAreExact(t *testing.T) {
	handler := echoHandler()

	fixed := httptest.NewRecorder()
	handler.ServeHTTP(fixed, httptest.NewRequest(http.MethodGet, "/perf/fixed/1024", nil))
	if fixed.Code != http.StatusOK || fixed.Header().Get("Content-Length") != "1024" || fixed.Body.Len() != 1024 {
		t.Fatalf("fixed status=%d length=%q body=%d", fixed.Code, fixed.Header().Get("Content-Length"), fixed.Body.Len())
	}

	echo := httptest.NewRecorder()
	handler.ServeHTTP(echo, httptest.NewRequest(http.MethodPost, "/perf/echo/32768", strings.NewReader(`{"request":"fixture"}`)))
	if echo.Code != http.StatusOK || echo.Header().Get("Content-Type") != "application/json" || echo.Body.Len() != 32768 {
		t.Fatalf("echo status=%d contentType=%q body=%d", echo.Code, echo.Header().Get("Content-Type"), echo.Body.Len())
	}
	if !bytes.HasPrefix(echo.Body.Bytes(), []byte(`{"payload":"`)) || !bytes.HasSuffix(echo.Body.Bytes(), []byte(`"}`)) {
		t.Fatalf("echo response is not JSON payload")
	}
}

func TestPerformanceFixtureStreamsConfiguredSSEEvents(t *testing.T) {
	recorder := httptest.NewRecorder()
	echoHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/perf/sse?events=2&payloadBytes=8&interval=1ms", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if got := strings.Count(recorder.Body.String(), "event: perf"); got != 2 {
		t.Fatalf("events=%d body=%q", got, recorder.Body.String())
	}
}

func TestPerformanceFixtureRejectsUnsafeParameters(t *testing.T) {
	for _, target := range []string{
		"/perf/download/1073741825",
		"/perf/sse?events=0",
		"/perf/delay?duration=6m",
	} {
		t.Run(target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			echoHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestPerformanceFixtureWebSocketEchoesConfiguredFrames(t *testing.T) {
	server := httptest.NewServer(echoHandler())
	defer server.Close()

	address := strings.TrimPrefix(server.URL, "http://")
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	stream := bufio.NewReadWriter(bufio.NewReader(connection), bufio.NewWriter(connection))
	if _, err := fmt.Fprintf(stream, "GET /perf/websocket?echoes=2 HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n", address); err != nil {
		t.Fatal(err)
	}
	if err := stream.Flush(); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/perf/websocket?echoes=2", nil)
	response, err := http.ReadResponse(stream.Reader, request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%d", response.StatusCode)
	}
	for _, payload := range []string{"one", "two"} {
		if err := writeMaskedTestFrame(stream, []byte(payload)); err != nil {
			t.Fatal(err)
		}
		if got, err := readUnmaskedTestFrame(stream.Reader); err != nil || string(got) != payload {
			t.Fatalf("payload=%q err=%v", got, err)
		}
	}
}

func writeMaskedTestFrame(writer *bufio.ReadWriter, payload []byte) error {
	if len(payload) > 125 {
		return fmt.Errorf("payload too large")
	}
	mask := [4]byte{1, 2, 3, 4}
	if _, err := writer.Write([]byte{0x81, 0x80 | byte(len(payload))}); err != nil {
		return err
	}
	if _, err := writer.Write(mask[:]); err != nil {
		return err
	}
	masked := append([]byte(nil), payload...)
	for index := range masked {
		masked[index] ^= mask[index%len(mask)]
	}
	if _, err := writer.Write(masked); err != nil {
		return err
	}
	return writer.Flush()
}

func readUnmaskedTestFrame(reader *bufio.Reader) ([]byte, error) {
	header, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	length, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if header != 0x81 || length&0x80 != 0 || length > 125 {
		return nil, fmt.Errorf("unexpected WebSocket frame")
	}
	payload := make([]byte, int(length))
	_, err = io.ReadFull(reader, payload)
	return payload, err
}
