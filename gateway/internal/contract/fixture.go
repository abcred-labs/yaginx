package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var emailValue = regexp.MustCompile(`(?i)^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// Fixture is a redacted HTTP exchange captured from the .NET golden runtime.
// It is intentionally transport-neutral so the same data can drive Go/API
// comparison tests later in W3/W5.
type Fixture struct {
	Version     int             `json:"version"`
	OperationID string          `json:"operationId"`
	Scenario    string          `json:"scenario"`
	ApprovalID  string          `json:"approvalId,omitempty"`
	Request     FixtureRequest  `json:"request"`
	Response    FixtureResponse `json:"response"`
	Source      string          `json:"source"`
}

type FixtureRequest struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

type FixtureResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

var fixtureScenarios = map[string]struct{}{
	"success": {}, "auth": {}, "parameter_error": {}, "business_error": {}, "missing_row": {}, "db_failure": {},
}

// Validate rejects unsafe capture output before it can become committed golden
// data. Dynamic values must be normalized by the capture plan, never retained.
func (fixture Fixture) Validate() error {
	if fixture.Version != 1 {
		return fmt.Errorf("fixture version = %d, want 1", fixture.Version)
	}
	if fixture.OperationID == "" {
		return fmt.Errorf("operationId is required")
	}
	if _, ok := fixtureScenarios[fixture.Scenario]; !ok {
		return fmt.Errorf("unsupported scenario %q", fixture.Scenario)
	}
	if fixture.Request.Method == "" || !strings.HasPrefix(fixture.Request.Path, "/api/") {
		return fmt.Errorf("request method and /api path are required")
	}
	if fixture.Response.Status < 100 || fixture.Response.Status > 599 {
		return fmt.Errorf("invalid HTTP status %d", fixture.Response.Status)
	}
	if fixture.Scenario == "success" && (fixture.Response.Status < 200 || fixture.Response.Status >= 300) {
		return fmt.Errorf("success scenario has non-success HTTP status %d", fixture.Response.Status)
	}
	if fixture.Scenario != "success" && fixture.Response.Status >= 200 && fixture.Response.Status < 300 {
		return fmt.Errorf("%s scenario has success HTTP status %d", fixture.Scenario, fixture.Response.Status)
	}
	if fixture.Source != "dotnet" && fixture.Source != "approved-test-adapter" {
		return fmt.Errorf("fixture source = %q, want dotnet or approved-test-adapter", fixture.Source)
	}
	if fixture.Source == "dotnet" && fixture.ApprovalID != "" {
		return fmt.Errorf("dotnet fixture must not carry approvalId")
	}
	if fixture.Source == "approved-test-adapter" && fixture.ApprovalID == "" {
		return fmt.Errorf("approved-test-adapter fixture requires approvalId")
	}
	for location, value := range map[string]json.RawMessage{"request body": fixture.Request.Body, "response body": fixture.Response.Body} {
		if len(value) == 0 || string(value) == "null" {
			continue
		}
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			return fmt.Errorf("%s is not JSON: %w", location, err)
		}
		if err := rejectSensitiveValue(decoded); err != nil {
			return fmt.Errorf("%s: %w", location, err)
		}
	}
	for name, value := range fixture.Request.Headers {
		if isSensitiveKey(name) && !strings.HasPrefix(value, "${") {
			return fmt.Errorf("request header %q contains an unredacted secret", name)
		}
	}
	for name, value := range fixture.Response.Headers {
		if isSensitiveKey(name) && !strings.HasPrefix(value, "${") {
			return fmt.Errorf("response header %q contains an unredacted secret", name)
		}
	}
	return nil
}

func rejectSensitiveValue(value any) error {
	switch typed := value.(type) {
	case string:
		if emailValue.MatchString(typed) && !strings.HasPrefix(typed, "${") {
			return fmt.Errorf("value contains an unredacted email address")
		}
	case map[string]any:
		for key, item := range typed {
			if isSensitiveKey(key) {
				if text, ok := item.(string); !ok || !strings.HasPrefix(text, "${") {
					return fmt.Errorf("field %q contains an unredacted secret or PII", key)
				}
			}
			if err := rejectSensitiveValue(item); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range typed {
			if err := rejectSensitiveValue(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	for _, marker := range []string{"password", "secret", "token", "authorization", "cookie", "email", "phone", "recovery", "smtp"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

// RedactFixture removes credential and PII values from a captured exchange.
// It intentionally leaves non-sensitive response semantics intact for golden
// comparison. Callers still need to normalize known run-specific values (such
// as tid and generated IDs) in their capture plan.
func RedactFixture(fixture *Fixture) error {
	var err error
	fixture.Request.Headers = redactHeaders(fixture.Request.Headers)
	fixture.Response.Headers = redactHeaders(fixture.Response.Headers)
	if fixture.Request.Body, err = redactJSON(fixture.Request.Body); err != nil {
		return fmt.Errorf("redact request body: %w", err)
	}
	if fixture.Response.Body, err = redactJSON(fixture.Response.Body); err != nil {
		return fmt.Errorf("redact response body: %w", err)
	}
	return nil
}

func redactHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	redacted := make(map[string]string, len(headers))
	for name, value := range headers {
		switch strings.ToLower(name) {
		case "date":
			redacted[name] = "${NOW}"
		case "act-trace-id", "traceparent", "x-request-id", "x-correlation-id":
			redacted[name] = "${RANDOM_ID}"
		case "duration", "handler-node":
			// These are runtime telemetry/deployment headers, not management API
			// semantics. Preserve their existence without pinning a host or timing.
			redacted[name] = "${RUNTIME_METADATA}"
		case "set-cookie":
			redacted[name] = "${REDACTED_SET_COOKIE}"
		case "authorization", "cookie":
			redacted[name] = "${REDACTED_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "}"
		default:
			if isSensitiveKey(name) {
				redacted[name] = "${REDACTED_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "}"
			} else {
				redacted[name] = value
			}
		}
	}
	return redacted
}

func redactJSON(body json.RawMessage) (json.RawMessage, error) {
	if len(body) == 0 || string(body) == "null" {
		return body, nil
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, err
	}
	redactValue(value)
	result, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func redactValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			normalizedKey := strings.ToLower(strings.ReplaceAll(key, "_", ""))
			if normalizedKey == "tid" {
				typed[key] = "${TID}"
				continue
			}
			if normalizedKey == "requestid" || normalizedKey == "traceid" || normalizedKey == "spanid" {
				typed[key] = "${RANDOM_ID}"
				continue
			}
			if isSensitiveKey(key) {
				typed[key] = "${REDACTED_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_")) + "}"
				continue
			}
			if text, ok := item.(string); ok && emailValue.MatchString(text) {
				typed[key] = "${REDACTED_EMAIL}"
				continue
			}
			redactValue(item)
		}
	case []any:
		for index, item := range typed {
			if text, ok := item.(string); ok && emailValue.MatchString(text) {
				typed[index] = "${REDACTED_EMAIL}"
				continue
			}
			redactValue(item)
		}
	}
}

// LoadFixtures reads captured exchanges from a fixture directory. README files
// and subdirectories are ignored so fixture documentation can live beside data.
func LoadFixtures(directory string) ([]Fixture, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	fixtures := make([]Fixture, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		var fixture Fixture
		if err := json.Unmarshal(contents, &fixture); err != nil {
			return nil, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		if err := fixture.Validate(); err != nil {
			return nil, fmt.Errorf("validate %s: %w", entry.Name(), err)
		}
		fixtures = append(fixtures, fixture)
	}
	return fixtures, nil
}

// VerifyFixtureCoverage requires both success and non-success fixtures for
// operations explicitly marked as fixture-required in the v2 contract.
func VerifyFixtureCoverage(entries []InventoryEntry, fixtures []Fixture, approvals []ApprovedDeviation) error {
	byOperation := make(map[string]map[string]struct{})
	approvalByID := approvalIndex(approvals, "api")
	used := make(map[string]struct{})
	for _, fixture := range fixtures {
		if fixture.Source == "approved-test-adapter" {
			approval, ok := approvalByID[fixture.ApprovalID]
			if !ok || approval.OperationID != fixture.OperationID || approval.Requirement != fixture.ScenarioRequirement() {
				return fmt.Errorf("fixture %s/%s uses unapproved or mismatched approvalId %q", fixture.OperationID, fixture.Scenario, fixture.ApprovalID)
			}
			used[approval.ID] = struct{}{}
		}
		if byOperation[fixture.OperationID] == nil {
			byOperation[fixture.OperationID] = make(map[string]struct{})
		}
		byOperation[fixture.OperationID][fixture.Scenario] = struct{}{}
	}
	if unused := sortedUnusedApprovals(filterApprovals(approvals, "api"), used); len(unused) > 0 {
		return fmt.Errorf("approved API deviations have no matching fixture: %s", strings.Join(unused, ", "))
	}
	missing := make([]string, 0)
	for _, entry := range entries {
		if !entry.FixtureRequired {
			continue
		}
		scenarios := byOperation[entry.OperationID]
		if _, ok := scenarios["success"]; !ok {
			missing = append(missing, entry.OperationID+":success")
		}
		hasFailure := false
		for scenario := range scenarios {
			if scenario != "success" {
				hasFailure = true
				break
			}
		}
		if !hasFailure {
			missing = append(missing, entry.OperationID+":failure")
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("fixture coverage is incomplete (%d missing): %s", len(missing), strings.Join(missing, ", "))
	}
	return nil
}

func (fixture Fixture) ScenarioRequirement() string {
	if fixture.Scenario == "success" {
		return "success"
	}
	return "failure"
}

func filterApprovals(approvals []ApprovedDeviation, kind string) []ApprovedDeviation {
	filtered := make([]ApprovedDeviation, 0, len(approvals))
	for _, approval := range approvals {
		if approval.Kind == kind {
			filtered = append(filtered, approval)
		}
	}
	return filtered
}
