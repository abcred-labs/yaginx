// proxycheck reports R0 proxy-fixture coverage directly from the compatibility
// matrix rather than a separate hand-maintained list of rows.
package main

import (
	"flag"
	"fmt"
	"os"

	"yaginx-gateway/internal/contract"
)

func main() {
	fixtureDir := flag.String("fixtures", "tests/contract/proxy", "directory containing captured proxy fixture metadata")
	flag.Parse()
	matrixPath, err := contract.FindWorkspaceFile(".", "planning/go-rewrite/contracts/proxy-compatibility-matrix.md")
	if err != nil {
		fatal("find proxy matrix: %v", err)
	}
	rows, err := contract.LoadProxyMatrixRows(matrixPath)
	if err != nil {
		fatal("load proxy matrix: %v", err)
	}
	fixtures, err := contract.LoadProxyFixtures(*fixtureDir)
	if err != nil {
		fatal("load fixtures: %v", err)
	}
	capturePlanPath, err := contract.FindWorkspaceFile(".", "gateway/tests/contract/proxy/capture-plan.template.json")
	if err != nil {
		fatal("find proxy capture plan: %v", err)
	}
	requirements, err := contract.LoadProxyFixtureRequirements(capturePlanPath)
	if err != nil {
		fatal("load proxy capture plan: %v", err)
	}
	fmt.Printf("Proxy matrix rows: %d\n", len(rows))
	fmt.Printf("Required proxy E2E behaviors: %d\n", len(requirements))
	fmt.Printf("Captured proxy E2E fixtures: %d\n", len(fixtures))
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
	if err := contract.VerifyProxyFixtureCoverage(rows, requirements, fixtures, approvals); err != nil {
		fatal("R0 gate: %v", err)
	}
	fmt.Printf("Approved deviations: %d\n", len(approvals))
	fmt.Println("R0 proxy fixture coverage: complete")
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "proxycheck: "+format+"\n", args...)
	os.Exit(1)
}
