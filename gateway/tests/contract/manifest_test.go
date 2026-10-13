package contract_test

import (
	"testing"

	"yaginx-gateway/internal/contract"
)

// The OpenAPI document is the only operation inventory.  The former 36-entry
// JSON manifest is intentionally not read here: it cannot silently drift from
// the 41-operation compatibility contract.
func TestManagementAPIManifest(t *testing.T) {
	path, err := contract.FindWorkspaceFile(".", "planning/go-rewrite/contracts/management-api.openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := contract.LoadOpenAPI(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.ValidateManagementContract(); err != nil {
		t.Fatal(err)
	}
}
