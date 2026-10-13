package repository

import (
	"crypto/sha256"
	"strings"
	"testing"
)

func TestNewAPITokenIsPrefixedAndHashOnly(t *testing.T) {
	token, hash, prefix, err := newAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "ygx_") {
		t.Fatalf("token prefix=%q", token)
	}
	if len(hash) != sha256.Size || prefix != token[:16] {
		t.Fatalf("hash length=%d prefix=%q token=%q", len(hash), prefix, token)
	}
	want := sha256.Sum256([]byte(token))
	if string(hash) != string(want[:]) {
		t.Fatal("stored hash does not match token")
	}
	if token == "" || strings.Contains(string(hash), token) {
		t.Fatal("raw token must not be stored as hash")
	}
}
