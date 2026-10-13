package contract

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProxyMatrixRow identifies a data row in the authoritative Markdown matrix.
// Line numbers are stable enough for review and avoid duplicating the matrix in
// a second hand-maintained manifest.
type ProxyMatrixRow struct {
	Line int
	Text string
}

// ProxyFixtureRequirement identifies a matrix behavior that must have
// executable proxy evidence. The capture plan is deliberately separate from
// the prose matrix: many matrix rows define field-level semantics that are
// exercised together by one end-to-end scenario.
type ProxyFixtureRequirement struct {
	MatrixLine int    `json:"matrixLine"`
	Name       string `json:"name"`
}

// ProxyFixture records which matrix row a captured E2E case proves.
type ProxyFixture struct {
	Version    int                      `json:"version"`
	MatrixLine int                      `json:"matrixLine"`
	Name       string                   `json:"name"`
	Source     string                   `json:"source"`
	ApprovalID string                   `json:"approvalId,omitempty"`
	Listener   ProxyListener            `json:"listener"`
	Config     json.RawMessage          `json:"config"`
	Client     ProxyClientObservation   `json:"client"`
	Upstream   ProxyUpstreamObservation `json:"upstream"`
	History    ProxyHistoryObservation  `json:"history"`
}

// ProxyListener identifies the wire listener exercised by a golden E2E case.
// It deliberately records the transport separately from the port because the
// HTTPS listener has both TCP and UDP/QUIC contract rows.
type ProxyListener struct {
	Port      int    `json:"port"`
	Transport string `json:"transport"`
}

// ProxyClientObservation is the request sent to the golden listener and the
// response returned to its caller. Bodies are stored as UTF-8 test payloads,
// never captured production traffic.
type ProxyClientObservation struct {
	Method     string            `json:"method"`
	Host       string            `json:"host"`
	Path       string            `json:"path"`
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers,omitempty"`
	Trailers   map[string]string `json:"trailers,omitempty"`
	Body       json.RawMessage   `json:"body,omitempty"`
	BodyBytes  int64             `json:"bodyBytes,omitempty"`
	BodySHA256 string            `json:"bodySha256,omitempty"`
}

// ProxyUpstreamObservation records whether an upstream was contacted and, if
// so, the request/result observed by the isolated test upstream. A direct
// simple-response fixture has Attempted=false and no upstream method/path.
type ProxyUpstreamObservation struct {
	Attempted  bool              `json:"attempted"`
	Method     string            `json:"method,omitempty"`
	Host       string            `json:"host,omitempty"`
	Path       string            `json:"path,omitempty"`
	Status     int               `json:"status,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	BodyBytes  int64             `json:"bodyBytes,omitempty"`
	BodySHA256 string            `json:"bodySha256,omitempty"`
}

// ProxyHistoryObservation is the expected request-history classification for
// the captured input. W0 validates it against the golden source separately
// when request-history collection is enabled; it must not be mistaken for a
// record read by the capture command itself.
type ProxyHistoryObservation struct {
	Status        int    `json:"status"`
	StatusSource  string `json:"statusSource"`
	ErrorCategory string `json:"errorCategory"`
}

// RedactProxyFixture applies the same secret/PII and runtime-header policy as
// management fixtures before proxy evidence is written to the repository.
func RedactProxyFixture(fixture *ProxyFixture) error {
	fixture.Client.Headers = redactHeaders(fixture.Client.Headers)
	fixture.Client.Trailers = redactHeaders(fixture.Client.Trailers)
	fixture.Upstream.Headers = redactHeaders(fixture.Upstream.Headers)
	// Proxy fixture bodies are stored as JSON values: structured responses stay
	// structured while plain-text responses are JSON strings. Only structured
	// JSON has PII fields the fixture redactor can safely inspect.
	if json.Valid(fixture.Client.Body) {
		body, err := redactJSON(fixture.Client.Body)
		if err != nil {
			return fmt.Errorf("redact client body: %w", err)
		}
		fixture.Client.Body = body
	}
	return nil
}

// LoadProxyMatrixRows extracts all non-header Markdown-table rows. Narrative
// bullets intentionally remain out of scope; the compatibility matrix tables
// are the fixture inventory source of truth.
func LoadProxyMatrixRows(path string) ([]ProxyMatrixRow, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(contents), "\n")
	rows := make([]ProxyMatrixRow, 0)
	inTable := false
	headerPending := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
			inTable = false
			headerPending = false
			continue
		}
		if !inTable {
			inTable = true
			headerPending = true
			continue
		}
		if headerPending {
			if isMarkdownSeparator(trimmed) {
				headerPending = false
			}
			continue
		}
		rows = append(rows, ProxyMatrixRow{Line: index + 1, Text: trimmed})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no Markdown matrix rows found")
	}
	return rows, nil
}

// LoadProxyFixtureRequirements reads the checked-in capture plan rather than
// assuming every explanatory table row requires a standalone fixture.
func LoadProxyFixtureRequirements(path string) ([]ProxyFixtureRequirement, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var plan struct {
		Cases []ProxyFixtureRequirement `json:"cases"`
	}
	if err := json.Unmarshal(contents, &plan); err != nil {
		return nil, fmt.Errorf("parse proxy capture plan: %w", err)
	}
	if len(plan.Cases) == 0 {
		return nil, fmt.Errorf("proxy capture plan has no cases")
	}
	return plan.Cases, nil
}

func isMarkdownSeparator(line string) bool {
	for _, cell := range strings.Split(strings.Trim(line, "|"), "|") {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			return false
		}
		for _, character := range cell {
			if character != '-' && character != ':' && character != ' ' {
				return false
			}
		}
	}
	return true
}

func LoadProxyFixtures(directory string) ([]ProxyFixture, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	fixtures := make([]ProxyFixture, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || strings.HasSuffix(entry.Name(), ".template.json") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		var fixture ProxyFixture
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

// Validate rejects metadata-only proxy entries. Every row must retain the
// input listener/config plus client, isolated-upstream, and the expected
// history classification required to compare a later Go implementation with
// the .NET golden host.
func (fixture ProxyFixture) Validate() error {
	if fixture.Version != 1 || fixture.MatrixLine < 1 || fixture.Name == "" || (fixture.Source != "dotnet" && fixture.Source != "contract") {
		return fmt.Errorf("invalid proxy fixture metadata")
	}
	if fixture.Source == "dotnet" && fixture.ApprovalID != "" {
		return fmt.Errorf("dotnet proxy fixture must not carry approvalId")
	}
	if fixture.Source == "contract" && fixture.ApprovalID == "" {
		return fmt.Errorf("contract proxy fixture requires approvalId")
	}
	if fixture.Listener.Port < 1 || fixture.Listener.Port > 65535 {
		return fmt.Errorf("listener port is invalid")
	}
	if fixture.Listener.Transport != "tcp" && fixture.Listener.Transport != "udp" {
		return fmt.Errorf("listener transport must be tcp or udp")
	}
	if len(fixture.Config) == 0 || !json.Valid(fixture.Config) {
		return fmt.Errorf("config must be JSON")
	}
	var config any
	if err := json.Unmarshal(fixture.Config, &config); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if err := rejectSensitiveValue(config); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if fixture.Client.Method == "" || fixture.Client.Host == "" || !strings.HasPrefix(fixture.Client.Path, "/") || fixture.Client.Status < 100 || fixture.Client.Status > 610 {
		return fmt.Errorf("client observation is incomplete")
	}
	if err := validateBodySummary(fixture.Client.BodyBytes, fixture.Client.BodySHA256); err != nil {
		return fmt.Errorf("client body summary: %w", err)
	}
	if fixture.Upstream.Attempted {
		if fixture.Upstream.Method == "" || fixture.Upstream.Host == "" || !strings.HasPrefix(fixture.Upstream.Path, "/") || fixture.Upstream.Status < 100 || fixture.Upstream.Status > 599 {
			return fmt.Errorf("attempted upstream observation is incomplete")
		}
		if err := validateBodySummary(fixture.Upstream.BodyBytes, fixture.Upstream.BodySHA256); err != nil {
			return fmt.Errorf("upstream body summary: %w", err)
		}
	} else if fixture.Upstream.Method != "" || fixture.Upstream.Host != "" || fixture.Upstream.Path != "" || fixture.Upstream.Status != 0 {
		return fmt.Errorf("unattempted upstream must not contain request data")
	}
	if fixture.History.Status < 100 || fixture.History.Status > 610 || fixture.History.StatusSource == "" {
		return fmt.Errorf("history observation is incomplete")
	}
	for location, headers := range map[string]map[string]string{"client headers": fixture.Client.Headers, "client trailers": fixture.Client.Trailers, "upstream headers": fixture.Upstream.Headers} {
		for name, value := range headers {
			if isSensitiveKey(name) && !strings.HasPrefix(value, "${") {
				return fmt.Errorf("%s contain unredacted %q", location, name)
			}
		}
	}
	for location, body := range map[string]json.RawMessage{"client body": fixture.Client.Body} {
		if len(body) == 0 || !json.Valid(body) {
			continue
		}
		var value any
		if err := json.Unmarshal(body, &value); err != nil {
			return fmt.Errorf("parse %s: %w", location, err)
		}
		if err := rejectSensitiveValue(value); err != nil {
			return fmt.Errorf("%s: %w", location, err)
		}
	}
	return nil
}

func validateBodySummary(bytes int64, hash string) error {
	if bytes < 0 {
		return fmt.Errorf("byte count is negative")
	}
	if hash == "" {
		return nil
	}
	if bytes == 0 || len(hash) != 64 {
		return fmt.Errorf("hash requires a non-zero byte count and 64 hex characters")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return fmt.Errorf("hash is not hexadecimal: %w", err)
	}
	return nil
}

// VerifyProxyFixtureCoverage requires at least one fixture for every explicit
// capture-plan behavior. Scenario names are the stable key because rendered
// Markdown line numbers change whenever explanatory rows are edited; the
// retained matrixLine fields are historical provenance, not identifiers.
func VerifyProxyFixtureCoverage(rows []ProxyMatrixRow, requirements []ProxyFixtureRequirement, fixtures []ProxyFixture, approvals []ApprovedDeviation) error {
	if len(rows) == 0 {
		return fmt.Errorf("proxy matrix has no data rows")
	}
	required := make(map[string]struct{}, len(requirements))
	for _, requirement := range requirements {
		if strings.TrimSpace(requirement.Name) == "" {
			return fmt.Errorf("proxy capture plan has an unnamed case")
		}
		if _, exists := required[requirement.Name]; exists {
			return fmt.Errorf("proxy capture plan duplicates case %q", requirement.Name)
		}
		required[requirement.Name] = struct{}{}
	}
	covered := make(map[string]struct{}, len(fixtures))
	approvalByID := approvalIndex(approvals, "proxy")
	used := make(map[string]struct{})
	for _, fixture := range fixtures {
		if fixture.Source == "contract" {
			approval, ok := approvalByID[fixture.ApprovalID]
			if !ok || approval.MatrixLine != fixture.MatrixLine {
				return fmt.Errorf("proxy fixture %q uses unapproved or mismatched approvalId %q", fixture.Name, fixture.ApprovalID)
			}
			used[approval.ID] = struct{}{}
		}
		covered[fixture.Name] = struct{}{}
	}
	if unused := sortedUnusedApprovals(filterApprovals(approvals, "proxy"), used); len(unused) > 0 {
		return fmt.Errorf("approved proxy deviations have no matching fixture: %s", strings.Join(unused, ", "))
	}
	missing := make([]string, 0)
	for name := range required {
		if _, ok := covered[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("proxy fixture coverage is incomplete (%d missing capture-plan cases): %s", len(missing), strings.Join(missing, ", "))
	}
	return nil
}
