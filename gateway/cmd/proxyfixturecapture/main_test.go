package main

import (
	"testing"
)

func TestEncodeFixtureBody(t *testing.T) {
	jsonBody, err := encodeFixtureBody([]byte(`{"email":"person@example.invalid"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(jsonBody), `{"email":"person@example.invalid"}`; got != want {
		t.Fatalf("JSON body = %s, want %s", got, want)
	}
	textBody, err := encodeFixtureBody([]byte("fixture-simple-response"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(textBody), `"fixture-simple-response"`; got != want {
		t.Fatalf("text body = %s, want %s", got, want)
	}
	if _, err := encodeFixtureBody([]byte{0xff}); err == nil {
		t.Fatal("invalid UTF-8 body was accepted")
	}
}

func TestProxyV1Header(t *testing.T) {
	if got, want := proxyV1Header("19080"), "PROXY TCP4 198.51.100.10 127.0.0.1 4242 19080\r\n"; got != want {
		t.Fatalf("header = %q, want %q", got, want)
	}
}

func TestProxyV2Header(t *testing.T) {
	header, err := proxyHeader("v2", "19080")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(header), 28; got != want {
		t.Fatalf("v2 header length = %d, want %d", got, want)
	}
	if got, want := header[16:20], []byte{198, 51, 100, 11}; string(got) != string(want) {
		t.Fatalf("source address = %#v, want %#v", got, want)
	}
	if _, err := proxyHeader("v2", "0"); err == nil {
		t.Fatal("invalid v2 port was accepted")
	}
}

func TestParseLocalBaseURL(t *testing.T) {
	if _, err := parseLocalBaseURL("http://127.0.0.1:19080"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseLocalBaseURL("https://example.invalid"); err == nil {
		t.Fatal("non-loopback base URL was accepted")
	}
}
