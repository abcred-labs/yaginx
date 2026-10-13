package database

import "testing"

func TestCurrentV1RequirementsAreComplete(t *testing.T) {
	if len(v1SchemaRequirements) != 9 {
		t.Fatalf("v1SchemaRequirements has %d tables, want 9", len(v1SchemaRequirements))
	}
	for table, columns := range v1SchemaRequirements {
		if len(columns) == 0 {
			t.Fatalf("current-v1 requirement for %q has no columns", table)
		}
	}
}
