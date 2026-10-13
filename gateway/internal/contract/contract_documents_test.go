package contract

import "testing"

func TestRuntimeSchemaAndPlanningDocuments(t *testing.T) {
	schema, err := FindWorkspaceFile(".", "planning/go-rewrite/contracts/runtime-config.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRuntimeSchema(schema); err != nil {
		t.Fatal(err)
	}
	root, err := FindWorkspaceFile(".", "planning/go-rewrite")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateMarkdownDocuments(root); err != nil {
		t.Fatal(err)
	}
}
