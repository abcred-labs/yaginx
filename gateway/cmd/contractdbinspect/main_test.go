package main

import "testing"

func TestLegacyTableInventory(t *testing.T) {
	if len(legacyTables) != 8 {
		t.Fatalf("legacy table count = %d, want 8", len(legacyTables))
	}
	seen := map[string]struct{}{}
	for _, table := range legacyTables {
		if table == "" {
			t.Fatal("empty table name")
		}
		if _, duplicate := seen[table]; duplicate {
			t.Fatalf("duplicate table %q", table)
		}
		seen[table] = struct{}{}
	}
}
