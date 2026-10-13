package bootstrap

import (
	"bytes"
	"testing"
)

func TestNewLoggerHonorsConfiguredFormat(t *testing.T) {
	var text bytes.Buffer
	NewLogger(&text, "info", "text").Info("message")
	if bytes.HasPrefix(bytes.TrimSpace(text.Bytes()), []byte("{")) {
		t.Fatalf("text logger emitted JSON: %q", text.String())
	}
	var json bytes.Buffer
	NewLogger(&json, "info", "json").Info("message")
	if !bytes.HasPrefix(bytes.TrimSpace(json.Bytes()), []byte("{")) {
		t.Fatalf("JSON logger emitted text: %q", json.String())
	}
}
