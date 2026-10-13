// contractcheck validates the v2 operation inventory and required fixtures.
package main

import (
	"flag"
	"fmt"
	"os"

	"yaginx-gateway/internal/contract"
)

func main() {
	fixtureDir := flag.String("fixtures", "tests/contract/fixtures", "directory containing captured API fixtures")
	flag.Parse()
	openAPIPath, err := contract.FindWorkspaceFile(".", "planning/go-rewrite/contracts/management-api.openapi.yaml")
	if err != nil {
		fatal("find OpenAPI: %v", err)
	}
	doc, err := contract.LoadOpenAPI(openAPIPath)
	if err != nil {
		fatal("load OpenAPI: %v", err)
	}
	if err := doc.ValidateManagementContract(); err != nil {
		fatal("OpenAPI contract: %v", err)
	}
	entries := doc.Inventory()
	fixtureRequired := 0
	for _, entry := range entries {
		if entry.FixtureRequired {
			fixtureRequired++
		}
	}
	fixtures, err := contract.LoadFixtures(*fixtureDir)
	if err != nil {
		fatal("load fixtures: %v", err)
	}
	fmt.Printf("OpenAPI operations: %d (%d fixture-required)\n", len(entries), fixtureRequired)
	fmt.Printf("Captured API fixtures: %d\n", len(fixtures))
	workspaceRoot, err := contract.FindWorkspaceRoot(".")
	if err != nil {
		fatal("find workspace root: %v", err)
	}
	approvalsPath, err := contract.FindWorkspaceFile(".", "planning/go-rewrite/contracts/approved-deviations.json")
	if err != nil {
		fatal("find approved deviations: %v", err)
	}
	approvals, err := contract.LoadApprovedDeviations(approvalsPath, workspaceRoot)
	if err != nil {
		fatal("load approved deviations: %v", err)
	}
	if err := contract.VerifyFixtureCoverage(entries, fixtures, approvals); err != nil {
		fatal("R0 gate: %v", err)
	}
	fmt.Printf("Approved deviations: %d\n", len(approvals))
	fmt.Println("v2 API fixture coverage: complete")
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "contractcheck: "+format+"\n", args...)
	os.Exit(1)
}
