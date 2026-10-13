// Package contract contains tooling shared by v2 contract checks and fixture
// capture commands. Production handlers are not generated from OpenAPI.
package contract

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// OpenAPI is the subset of OpenAPI used to inventory management operations.
type OpenAPI struct {
	OpenAPI string                          `yaml:"openapi"`
	Servers []Server                        `yaml:"servers"`
	Paths   map[string]map[string]Operation `yaml:"paths"`
}

type Server struct {
	URL string `yaml:"url"`
}

type Operation struct {
	OperationID     string `yaml:"operationId"`
	GoHandler       string `yaml:"x-go-handler"`
	FixtureRequired bool   `yaml:"x-fixture-required"`
}

// InventoryEntry is one concrete HTTP method/path operation.
type InventoryEntry struct {
	Method string
	Path   string
	Operation
}

var methods = map[string]struct{}{
	"get": {}, "put": {}, "post": {}, "delete": {}, "patch": {}, "head": {}, "options": {}, "trace": {},
}

// LoadOpenAPI parses the canonical YAML document.
func LoadOpenAPI(path string) (OpenAPI, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return OpenAPI{}, err
	}
	var doc OpenAPI
	if err := yaml.Unmarshal(contents, &doc); err != nil {
		return OpenAPI{}, fmt.Errorf("parse OpenAPI YAML: %w", err)
	}
	return doc, nil
}

// Inventory returns a stable, concrete method/path list.
func (doc OpenAPI) Inventory() []InventoryEntry {
	entries := make([]InventoryEntry, 0)
	for path, item := range doc.Paths {
		for method, operation := range item {
			method = strings.ToLower(method)
			if _, ok := methods[method]; !ok {
				continue
			}
			entries = append(entries, InventoryEntry{Method: strings.ToUpper(method), Path: path, Operation: operation})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path == entries[j].Path {
			return entries[i].Method < entries[j].Method
		}
		return entries[i].Path < entries[j].Path
	})
	return entries
}

// ValidateManagementContract checks the v2 management operation inventory.
func (doc OpenAPI) ValidateManagementContract() error {
	if doc.OpenAPI != "3.1.0" {
		return fmt.Errorf("openapi version = %q, want 3.1.0", doc.OpenAPI)
	}
	if len(doc.Servers) != 1 || doc.Servers[0].URL != "/yaginx_console" {
		return fmt.Errorf("servers must contain only /yaginx_console")
	}
	entries := doc.Inventory()
	if len(entries) != 64 {
		return fmt.Errorf("operation count = %d, want 64", len(entries))
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		key := entry.Method + " " + entry.Path
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate operation %s", key)
		}
		seen[key] = struct{}{}
		if entry.OperationID == "" || entry.GoHandler == "" {
			return fmt.Errorf("%s: operationId and x-go-handler are required", key)
		}
	}
	return nil
}

// ValidateHandlerPackages asserts that every x-go-handler annotation names a
// package that actually exists under the supplied Go module directory. The
// annotation is the contract's statement of where an operation is served from,
// so an annotation pointing at a package nobody wrote is drift, not design.
func (doc OpenAPI) ValidateHandlerPackages(moduleDir string) error {
	packages := make(map[string][]string)
	for _, entry := range doc.Inventory() {
		key := entry.Method + " " + entry.Path
		pkg, receiver, found := strings.Cut(entry.GoHandler, ".")
		if !found || pkg == "" {
			return fmt.Errorf("%s: x-go-handler %q must be <package>.<Type>.<Method>", key, entry.GoHandler)
		}
		if strings.Count(receiver, ".") != 1 {
			return fmt.Errorf("%s: x-go-handler %q must be <package>.<Type>.<Method>", key, entry.GoHandler)
		}
		packages[pkg] = append(packages[pkg], key)
	}
	for pkg, operations := range packages {
		dir := filepath.Join(moduleDir, filepath.FromSlash(pkg))
		sources, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			return err
		}
		if len(sources) == 0 {
			sort.Strings(operations)
			return fmt.Errorf("x-go-handler package %s has no Go sources (claimed by %s)", pkg, strings.Join(operations, ", "))
		}
	}
	return nil
}

// ValidateOpenAPIReferences rejects unresolved local $ref values. OpenAPI has
// a large schema surface, so this catches the most damaging contract drift
// without pretending that production handlers are generated from the document.
func ValidateOpenAPIReferences(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return fmt.Errorf("parse OpenAPI YAML: %w", err)
	}
	if len(document.Content) != 1 {
		return fmt.Errorf("OpenAPI has no document root")
	}
	root := document.Content[0]
	return walkYAML(root, func(node *yaml.Node) error {
		if node.Kind != yaml.MappingNode {
			return nil
		}
		for index := 0; index+1 < len(node.Content); index += 2 {
			if node.Content[index].Value != "$ref" {
				continue
			}
			reference := node.Content[index+1].Value
			if !strings.HasPrefix(reference, "#/") {
				return fmt.Errorf("unsupported non-local $ref %q", reference)
			}
			if _, err := resolveYAMLPointer(root, reference[1:]); err != nil {
				return fmt.Errorf("unresolved $ref %q: %w", reference, err)
			}
		}
		return nil
	})
}

func walkYAML(node *yaml.Node, visit func(*yaml.Node) error) error {
	if err := visit(node); err != nil {
		return err
	}
	for _, child := range node.Content {
		if err := walkYAML(child, visit); err != nil {
			return err
		}
	}
	return nil
}

func resolveYAMLPointer(root *yaml.Node, pointer string) (*yaml.Node, error) {
	current := root
	for _, segment := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		switch current.Kind {
		case yaml.MappingNode:
			found := false
			for index := 0; index+1 < len(current.Content); index += 2 {
				if current.Content[index].Value == segment {
					current = current.Content[index+1]
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("mapping key %q not found", segment)
			}
		case yaml.SequenceNode:
			index := -1
			if _, err := fmt.Sscanf(segment, "%d", &index); err != nil || index < 0 || index >= len(current.Content) {
				return nil, fmt.Errorf("sequence index %q not found", segment)
			}
			current = current.Content[index]
		default:
			return nil, fmt.Errorf("cannot descend into YAML kind %d", current.Kind)
		}
	}
	return current, nil
}

// FindWorkspaceFile finds a repository-relative contract when invoked from a
// package test, a command, or an arbitrary child directory.
func FindWorkspaceFile(start, relative string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, relative)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find %s from %s", relative, start)
		}
		dir = parent
	}
}

// FindWorkspaceRoot returns the repository root for commands that need to
// validate repository-relative evidence paths.
func FindWorkspaceRoot(start string) (string, error) {
	planningDir, err := FindWorkspaceFile(start, "planning/go-rewrite")
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(planningDir)), nil
}
