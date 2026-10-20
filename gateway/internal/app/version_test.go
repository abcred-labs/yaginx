package app

import "testing"

func TestVersionStartsAtOnePointOne(t *testing.T) {
	if BaseVersion != "1.1" || Version != "1.1.0" {
		t.Fatalf("version = %q (base %q), want 1.1.0 (base 1.1)", Version, BaseVersion)
	}
}
