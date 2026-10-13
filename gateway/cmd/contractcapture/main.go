// contractcapture records redacted .NET HTTP responses as W0 API fixtures.
// The capture plan intentionally lives outside the repository because it can
// contain operational credentials and request data.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"yaginx-gateway/internal/contract"
)

type capturePlan struct {
	Setup []setupCase   `json:"setup,omitempty"`
	Cases []captureCase `json:"cases"`
}

// setupCase performs an unrecorded bootstrap request (normally login) and
// extracts named values for later case headers/bodies. Extracted values exist
// only in memory and are never written to fixture files or stdout.
type setupCase struct {
	Name    string            `json:"name"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
	Extract map[string]string `json:"extract"`
}

type captureCase struct {
	OperationID string            `json:"operationId"`
	Scenario    string            `json:"scenario"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        json.RawMessage   `json:"body,omitempty"`
	Normalize   []normalizer      `json:"normalize,omitempty"`
}

// normalizer replaces an explicitly selected dynamic JSON value with a stable
// marker. It never redacts arbitrary IDs: capture plans must name only values
// known to vary between otherwise identical fixture runs.
type normalizer struct {
	Location string `json:"location"`
	Pointer  string `json:"pointer"`
	Value    string `json:"value"`
}

func main() {
	baseURL := flag.String("base-url", "", "base URL of the running .NET golden server")
	planPath := flag.String("plan", "", "absolute path to a non-committed capture plan JSON")
	outDir := flag.String("out", "tests/contract/fixtures", "fixture output directory")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout")
	only := flag.String("only", "", "optional comma-separated operationId.scenario cases to capture")
	flag.Parse()
	if *baseURL == "" || *planPath == "" {
		fatal("-base-url and -plan are required")
	}
	base, err := url.Parse(*baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		fatal("invalid -base-url: %q", *baseURL)
	}
	contents, err := os.ReadFile(*planPath)
	if err != nil {
		fatal("read plan: %v", err)
	}
	var plan capturePlan
	if err := json.Unmarshal(contents, &plan); err != nil {
		fatal("parse plan: %v", err)
	}
	if len(plan.Cases) == 0 {
		fatal("capture plan has no cases")
	}
	client := &http.Client{Timeout: *timeout}
	variables := make(map[string]string)
	for _, setup := range plan.Setup {
		if err := runSetup(client, base, setup, variables); err != nil {
			fatal("setup %q: %v", setup.Name, err)
		}
	}
	type pendingFixture struct {
		path     string
		contents []byte
	}
	selected, err := selectCases(plan.Cases, *only)
	if err != nil {
		fatal("select cases: %v", err)
	}
	pending := make([]pendingFixture, 0, len(selected))
	seen := make(map[string]struct{}, len(selected))
	for _, item := range selected {
		fixture, err := capture(client, base, item, variables)
		if err != nil {
			fatal("%s.%s: %v", item.OperationID, item.Scenario, err)
		}
		if err := applyNormalizers(&fixture, item.Normalize); err != nil {
			fatal("%s.%s: normalize: %v", item.OperationID, item.Scenario, err)
		}
		if err := contract.RedactFixture(&fixture); err != nil {
			fatal("%s.%s: %v", item.OperationID, item.Scenario, err)
		}
		if err := fixture.Validate(); err != nil {
			fatal("%s.%s: unsafe fixture: %v", item.OperationID, item.Scenario, err)
		}
		encoded, err := json.MarshalIndent(fixture, "", "  ")
		if err != nil {
			fatal("encode fixture: %v", err)
		}
		path := filepath.Join(*outDir, item.OperationID+"."+item.Scenario+".json")
		if _, duplicate := seen[path]; duplicate {
			fatal("duplicate fixture output %s", path)
		}
		seen[path] = struct{}{}
		pending = append(pending, pendingFixture{path: path, contents: append(encoded, '\n')})
	}
	if err := os.MkdirAll(*outDir, 0o750); err != nil {
		fatal("create output directory: %v", err)
	}
	for _, fixture := range pending {
		if err := os.WriteFile(fixture.path, fixture.contents, 0o600); err != nil {
			fatal("write %s: %v", fixture.path, err)
		}
		fmt.Printf("captured %s\n", fixture.path)
	}
}

func selectCases(cases []captureCase, only string) ([]captureCase, error) {
	if strings.TrimSpace(only) == "" {
		return cases, nil
	}
	wanted := make(map[string]struct{})
	for _, value := range strings.Split(only, ",") {
		value = strings.TrimSpace(value)
		if value == "" || !strings.Contains(value, ".") {
			return nil, fmt.Errorf("case %q must be operationId.scenario", value)
		}
		wanted[value] = struct{}{}
	}
	result := make([]captureCase, 0, len(wanted))
	for _, item := range cases {
		key := item.OperationID + "." + item.Scenario
		if _, ok := wanted[key]; ok {
			result = append(result, item)
			delete(wanted, key)
		}
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for key := range wanted {
			missing = append(missing, key)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("cases not found: %s", strings.Join(missing, ", "))
	}
	return result, nil
}

func capture(client *http.Client, base *url.URL, item captureCase, variables map[string]string) (contract.Fixture, error) {
	if item.OperationID == "" || item.Scenario == "" || item.Method == "" || !strings.HasPrefix(item.Path, "/api/") {
		return contract.Fixture{}, fmt.Errorf("operationId, scenario, method, and /api path are required")
	}
	wireBody, err := resolveBody(item.Body, variables)
	if err != nil {
		return contract.Fixture{}, err
	}
	wireHeaders, err := resolveHeaders(item.Headers, variables)
	if err != nil {
		return contract.Fixture{}, err
	}
	target, err := apiTarget(base, item.Path)
	if err != nil {
		return contract.Fixture{}, err
	}
	request, err := http.NewRequest(item.Method, target.String(), bytes.NewReader(wireBody))
	if err != nil {
		return contract.Fixture{}, err
	}
	for name, value := range wireHeaders {
		request.Header.Set(name, value)
	}
	if len(item.Body) > 0 && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return contract.Fixture{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return contract.Fixture{}, err
	}
	return contract.Fixture{
		Version: 1, OperationID: item.OperationID, Scenario: item.Scenario, Source: "dotnet",
		Request:  contract.FixtureRequest{Method: request.Method, Path: item.Path, Headers: flattenHeaders(request.Header), Body: item.Body},
		Response: contract.FixtureResponse{Status: response.StatusCode, Headers: flattenHeaders(response.Header), Body: body},
	}, nil
}

func runSetup(client *http.Client, base *url.URL, setup setupCase, variables map[string]string) error {
	if setup.Name == "" || setup.Method == "" || !strings.HasPrefix(setup.Path, "/api/") || len(setup.Extract) == 0 {
		return fmt.Errorf("name, method, /api path, and at least one extract are required")
	}
	body, err := resolveBody(setup.Body, variables)
	if err != nil {
		return err
	}
	headers, err := resolveHeaders(setup.Headers, variables)
	if err != nil {
		return err
	}
	target, err := apiTarget(base, setup.Path)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(setup.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	if len(body) > 0 && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected status %d", response.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return err
	}
	for name, pointer := range setup.Extract {
		value, err := extractJSONPointer(responseBody, pointer)
		if err != nil {
			return fmt.Errorf("extract %q: %w", name, err)
		}
		variables[name] = value
	}
	return nil
}

// apiTarget retains the query component in a capture-plan path. Building a
// url.URL with Path directly would encode '?' as %3F and capture a framework
// 404 instead of the intended legacy endpoint behavior.
func apiTarget(base *url.URL, apiPath string) (*url.URL, error) {
	relative, err := url.Parse("/yaginx_console" + apiPath)
	if err != nil {
		return nil, fmt.Errorf("parse API path: %w", err)
	}
	return base.ResolveReference(relative), nil
}

var placeholder = regexp.MustCompile(`\$\{([^}]+)\}`)

func resolveHeaders(headers map[string]string, variables map[string]string) (map[string]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	result := make(map[string]string, len(headers))
	for name, value := range headers {
		resolved, err := resolveString(value, variables)
		if err != nil {
			return nil, fmt.Errorf("header %q: %w", name, err)
		}
		result[name] = resolved
	}
	return result, nil
}

func resolveBody(body json.RawMessage, variables map[string]string) ([]byte, error) {
	if len(body) == 0 {
		return nil, nil
	}
	resolved, err := resolveString(string(body), variables)
	if err != nil {
		return nil, err
	}
	return []byte(resolved), nil
}

func resolveString(value string, variables map[string]string) (string, error) {
	var resolveError error
	result := placeholder.ReplaceAllStringFunc(value, func(match string) string {
		key := placeholder.FindStringSubmatch(match)[1]
		if strings.HasPrefix(key, "ENV:") {
			resolved := os.Getenv(strings.TrimPrefix(key, "ENV:"))
			if resolved == "" {
				resolveError = fmt.Errorf("environment variable %q is not set", key)
			}
			return resolved
		}
		resolved, ok := variables[key]
		if !ok || resolved == "" {
			resolveError = fmt.Errorf("variable %q is not set", key)
		}
		return resolved
	})
	if resolveError != nil {
		return "", resolveError
	}
	return result, nil
}

func extractJSONPointer(body []byte, pointer string) (string, error) {
	if !strings.HasPrefix(pointer, "/") {
		return "", fmt.Errorf("pointer must start with /")
	}
	var document any
	if err := json.Unmarshal(body, &document); err != nil {
		return "", err
	}
	current := document
	for _, segment := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		switch typed := current.(type) {
		case map[string]any:
			var found bool
			current, found = typed[segment]
			if !found {
				return "", fmt.Errorf("object key not found")
			}
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(typed) {
				return "", fmt.Errorf("array index not found")
			}
			current = typed[index]
		default:
			return "", fmt.Errorf("cannot descend into JSON value")
		}
	}
	value, ok := current.(string)
	if !ok || value == "" {
		return "", fmt.Errorf("pointer does not resolve to a non-empty string")
	}
	return value, nil
}

func flattenHeaders(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	result := make(map[string]string, len(headers))
	for name, values := range headers {
		result[name] = strings.Join(values, ", ")
	}
	return result
}

func applyNormalizers(fixture *contract.Fixture, normalizers []normalizer) error {
	for _, normalizer := range normalizers {
		if normalizer.Pointer == "" || !strings.HasPrefix(normalizer.Pointer, "/") || normalizer.Value == "" {
			return fmt.Errorf("location, non-root JSON pointer, and replacement value are required")
		}
		var target *json.RawMessage
		switch normalizer.Location {
		case "request.body":
			target = &fixture.Request.Body
		case "response.body":
			target = &fixture.Response.Body
		default:
			return fmt.Errorf("unsupported location %q", normalizer.Location)
		}
		if err := replaceJSONPointer(target, normalizer.Pointer, normalizer.Value); err != nil {
			return fmt.Errorf("%s %s: %w", normalizer.Location, normalizer.Pointer, err)
		}
	}
	return nil
}

func replaceJSONPointer(body *json.RawMessage, pointer, replacement string) error {
	var document any
	if err := json.Unmarshal(*body, &document); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}
	segments := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	for index := range segments {
		segments[index] = strings.ReplaceAll(strings.ReplaceAll(segments[index], "~1", "/"), "~0", "~")
	}
	current := document
	for index, segment := range segments {
		last := index == len(segments)-1
		switch typed := current.(type) {
		case map[string]any:
			if last {
				if _, found := typed[segment]; !found {
					return fmt.Errorf("object key not found")
				}
				typed[segment] = replacement
				continue
			}
			next, found := typed[segment]
			if !found {
				return fmt.Errorf("object key not found")
			}
			current = next
		case []any:
			item, err := strconv.Atoi(segment)
			if err != nil || item < 0 || item >= len(typed) {
				return fmt.Errorf("array index not found")
			}
			if last {
				typed[item] = replacement
				continue
			}
			current = typed[item]
		default:
			return fmt.Errorf("cannot descend into JSON value")
		}
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return err
	}
	*body = encoded
	return nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "contractcapture: "+format+"\n", args...)
	os.Exit(1)
}
