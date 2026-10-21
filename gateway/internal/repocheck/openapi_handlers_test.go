package repocheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var openAPIHandler = regexp.MustCompile(`(?m)^\s*x-go-handler:\s*(internal/[A-Za-z0-9_/]+)\.Handler\.([A-Za-z0-9_]+)\s*$`)

// TestOpenAPIHandlersExist keeps every x-go-handler annotation in the
// management contract pointing at a real Handler method.
func TestOpenAPIHandlersExist(t *testing.T) {
	specPath, err := findWorkspaceFile(".", "planning/go-rewrite/contracts/management-api.openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	matches := openAPIHandler.FindAllStringSubmatch(string(spec), -1)
	if len(matches) == 0 {
		t.Fatal("management contract has no x-go-handler annotations")
	}
	gatewayDir := filepath.Join(filepath.Dir(specPath), "..", "..", "..", "gateway")
	sources := map[string]string{}
	for _, match := range matches {
		pkg, method := match[1], match[2]
		source, ok := sources[pkg]
		if !ok {
			files, err := filepath.Glob(filepath.Join(gatewayDir, filepath.FromSlash(pkg), "*.go"))
			if err != nil {
				t.Fatal(err)
			}
			var builder strings.Builder
			for _, file := range files {
				if strings.HasSuffix(file, "_test.go") {
					continue
				}
				contents, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				builder.Write(contents)
			}
			source = builder.String()
			sources[pkg] = source
		}
		if source == "" {
			t.Errorf("x-go-handler package %s has no Go source files", pkg)
			continue
		}
		if !regexp.MustCompile(`(?m)^func \(\w+ \*?Handler\) ` + method + `\(`).MatchString(source) {
			t.Errorf("x-go-handler %s.Handler.%s does not exist", pkg, method)
		}
	}
}
