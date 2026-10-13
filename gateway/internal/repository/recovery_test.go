package repository

import (
	"regexp"
	"testing"
)

func TestPasswordHashIsLegacyCompatibleShape(t *testing.T) {
	hash := passwordHash("secret", "salt")
	if hash != "F84FA2149DBB62ED4E0CF1F550D2949B33A6513D3A7707E08502511C79CCB0EE" {
		t.Fatalf("hash=%s", hash)
	}
}

func TestRecoveryHelpers(t *testing.T) {
	code, err := randomCode()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
		t.Fatalf("code=%q", code)
	}
	if got := maskEmail("admin@example.invalid"); got != "a***n@example.invalid" {
		t.Fatalf("masked=%q", got)
	}
}
