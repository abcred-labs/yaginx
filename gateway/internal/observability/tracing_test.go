package observability

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTracingExportsRedactedHTTPSpan(t *testing.T) {
	payloads := make(chan []byte, 1)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/traces" {
			t.Errorf("path=%q", request.URL.Path)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		payloads <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()
	tracing, err := NewTracing(context.Background(), OTLPSettings{
		Enabled:       true,
		Endpoint:      collector.URL + "/v1/traces",
		Protocol:      "http/protobuf",
		Insecure:      true,
		SampleRatio:   1,
		ExportTimeout: time.Second,
	}, "test-version", "test-commit")
	if err != nil {
		t.Fatal(err)
	}
	handler := tracing.Handler("https", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodPost, "https://secret.example.invalid/sensitive/path?token=forbidden", nil)
	request.Header.Set("Authorization", "Bearer forbidden")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	flush, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tracing.Shutdown(flush); err != nil {
		t.Fatal(err)
	}
	select {
	case payload := <-payloads:
		for _, forbidden := range [][]byte{[]byte("secret.example.invalid"), []byte("sensitive/path"), []byte("forbidden"), []byte("Authorization")} {
			if bytes.Contains(payload, forbidden) {
				t.Fatalf("OTLP payload contains sensitive request value %q", forbidden)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("OTLP collector did not receive a span")
	}
}

func TestTracingDisabledDoesNotCreateExporter(t *testing.T) {
	tracing, err := NewTracing(context.Background(), OTLPSettings{}, "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	if tracing.provider != nil {
		t.Fatal("disabled tracing created a provider")
	}
}
