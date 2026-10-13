package acme

import "testing"

func TestNormalizeDomain(t *testing.T) {
	if normalizeDomain(" Example.TEST. ") != "example.test" {
		t.Fatal("normalization failed")
	}
	if normalizeDomain("*.example.test") != "" {
		t.Fatal("accepted wildcard HTTP-01")
	}
	if normalizeDomain("BÜCHER.example.") != "xn--bcher-kva.example" {
		t.Fatalf("IDNA normalization = %q", normalizeDomain("BÜCHER.example."))
	}
}
