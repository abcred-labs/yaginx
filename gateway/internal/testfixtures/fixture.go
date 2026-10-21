package testfixtures

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
)

var emailValue = regexp.MustCompile(`(?i)^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// Fixture is a redacted management API exchange replayed through the Go
// router. It is transport-neutral so request and response can be compared
// without a live listener.
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

// Validate rejects unsafe or incomplete fixture data. Dynamic values are stored
// as ${...} placeholders, never retained.
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
	if fixture.Source != "captured" && fixture.Source != "adapter" {
		return fmt.Errorf("fixture source = %q, want captured or adapter", fixture.Source)
	}
	if fixture.Source == "captured" && fixture.ApprovalID != "" {
		return fmt.Errorf("captured fixture must not carry approvalId")
	}
	if fixture.Source == "adapter" && fixture.ApprovalID == "" {
		return fmt.Errorf("adapter fixture requires approvalId")
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

func loadManagement(fsys fs.FS, directory string) ([]Fixture, error) {
	entries, err := fs.ReadDir(fsys, directory)
	if err != nil {
		return nil, err
	}
	fixtures := make([]Fixture, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".json" {
			continue
		}
		contents, err := fs.ReadFile(fsys, path.Join(directory, entry.Name()))
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
