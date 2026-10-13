package main

import "testing"

func TestRunPattern(t *testing.T) {
	for _, value := range []string{"r0-20260726", "golden-fixture-1"} {
		if !runPattern.MatchString(value) {
			t.Fatalf("valid run %q rejected", value)
		}
	}
	for _, value := range []string{"R0", "x", "run_name", "run;drop"} {
		if runPattern.MatchString(value) {
			t.Fatalf("invalid run %q accepted", value)
		}
	}
}

func TestFixtureIDsAreStableAndDistinct(t *testing.T) {
	ids := map[int64]struct{}{fixtureWebsiteID: {}, fixtureWebDomainID: {}, fixtureRequestHistoryID: {}, fixtureDeleteWebsiteID: {}, fixtureDeleteDomainID: {}}
	if len(ids) != 5 {
		t.Fatal("fixture IDs must be distinct")
	}
}
