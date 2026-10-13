package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagementOpenAPIInventory(t *testing.T) {
	path, err := FindWorkspaceFile(".", "planning/go-rewrite/contracts/management-api.openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := LoadOpenAPI(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.ValidateManagementContract(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOpenAPIReferences(path); err != nil {
		t.Fatal(err)
	}
}

// ValidateHandlerPackages is what replaced the ogen drift check, so the gate
// has to be shown to fail on drift rather than merely pass on the real tree.
func TestValidateHandlerPackagesRejectsMissingPackage(t *testing.T) {
	doc := OpenAPI{Paths: map[string]map[string]Operation{
		"/api/analytics/overview": {"post": {OperationID: "analyticsOverview", GoHandler: "internal/management/nosuchpkg.Handler.Overview"}},
	}}
	err := doc.ValidateHandlerPackages(t.TempDir())
	if err == nil {
		t.Fatal("expected a missing handler package to be rejected")
	}
	if !strings.Contains(err.Error(), "internal/management/nosuchpkg") {
		t.Fatalf("error should name the missing package, got %v", err)
	}
}

func TestValidateHandlerPackagesRejectsMalformedAnnotation(t *testing.T) {
	for _, handler := range []string{"", "internal/management/analytics", "internal/management/analytics.Handler"} {
		doc := OpenAPI{Paths: map[string]map[string]Operation{
			"/api/analytics/overview": {"post": {OperationID: "analyticsOverview", GoHandler: handler}},
		}}
		if err := doc.ValidateHandlerPackages(t.TempDir()); err == nil {
			t.Fatalf("expected x-go-handler %q to be rejected", handler)
		}
	}
}

func TestValidateHandlerPackagesAcceptsPopulatedPackage(t *testing.T) {
	moduleDir := t.TempDir()
	pkgDir := filepath.Join(moduleDir, "internal", "management", "analytics")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "handler.go"), []byte("package analytics\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := OpenAPI{Paths: map[string]map[string]Operation{
		"/api/analytics/overview": {"post": {OperationID: "analyticsOverview", GoHandler: "internal/management/analytics.Handler.Overview"}},
	}}
	if err := doc.ValidateHandlerPackages(moduleDir); err != nil {
		t.Fatal(err)
	}
}

func TestFindWorkspaceFile(t *testing.T) {
	path, err := FindWorkspaceFile(".", "planning/go-rewrite/contracts/management-api.openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
