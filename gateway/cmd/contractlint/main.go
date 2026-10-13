// contractlint validates the non-runtime parts of the Go rewrite contract.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"yaginx-gateway/internal/contract"
)

func main() {
	openAPIPath, err := contract.FindWorkspaceFile(".", "planning/go-rewrite/contracts/management-api.openapi.yaml")
	if err != nil {
		fatal("find OpenAPI: %v", err)
	}
	doc, err := contract.LoadOpenAPI(openAPIPath)
	if err != nil {
		fatal("load OpenAPI: %v", err)
	}
	if err := doc.ValidateManagementContract(); err != nil {
		fatal("validate OpenAPI inventory: %v", err)
	}
	if err := contract.ValidateOpenAPIReferences(openAPIPath); err != nil {
		fatal("validate OpenAPI refs: %v", err)
	}
	moduleDir, err := contract.FindWorkspaceFile(".", "go.mod")
	if err != nil {
		fatal("find Go module: %v", err)
	}
	if err := doc.ValidateHandlerPackages(filepath.Dir(moduleDir)); err != nil {
		fatal("validate OpenAPI handler packages: %v", err)
	}
	schemaPath, err := contract.FindWorkspaceFile(".", "planning/go-rewrite/contracts/runtime-config.schema.json")
	if err != nil {
		fatal("find runtime schema: %v", err)
	}
	if err := contract.ValidateRuntimeSchema(schemaPath); err != nil {
		fatal("validate runtime schema: %v", err)
	}
	planningPath, err := contract.FindWorkspaceFile(".", "planning/go-rewrite")
	if err != nil {
		fatal("find planning root: %v", err)
	}
	if err := contract.ValidateMarkdownDocuments(planningPath); err != nil {
		fatal("validate planning Markdown: %v", err)
	}
	fmt.Println("Go rewrite contract lint: ok")
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "contractlint: "+format+"\n", args...)
	os.Exit(1)
}
