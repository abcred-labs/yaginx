package app

import "testing"

func TestVersionStartsAtOnePointZero(t *testing.T) {
	if BaseVersion != "1.0" || Version != "1.0.0" {
		t.Fatalf("version = %q (base %q), want 1.0.0 (base 1.0)", Version, BaseVersion)
	}
}
