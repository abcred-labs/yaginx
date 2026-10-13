package repository

import "testing"

func TestSystemErrorMessageFingerprintNormalizesWhitespace(t *testing.T) {
	a := normalizeSystemErrorFingerprint("database", "load", "DB_FAILED", " Database   connection failed ")
	b := normalizeSystemErrorFingerprint("database", "load", "DB_FAILED", "database connection failed")
	if a != b {
		t.Fatalf("fingerprints differ: %q != %q", a, b)
	}
}

func TestValidSystemErrorCode(t *testing.T) {
	for _, value := range []string{"DB_CONNECTION_FAILED", "UPGRADE_READINESS_FAILED", "RUNTIME_INTERNAL_PANIC"} {
		if !validSystemErrorCode(value) {
			t.Fatalf("expected valid code %q", value)
		}
	}
	for _, value := range []string{"db_failed", "has space", "", "bad-thing"} {
		if validSystemErrorCode(value) {
			t.Fatalf("expected invalid code %q", value)
		}
	}
}
