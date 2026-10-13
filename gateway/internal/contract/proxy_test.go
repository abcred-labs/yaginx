package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProxyMatrixRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "matrix.md")
	if err := os.WriteFile(path, []byte("# Matrix\n\n| A | B |\n| --- | --- |\n| one | two |\n\ntext\n\n| C |\n| --- |\n| three |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := LoadProxyMatrixRows(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	fixture := func(line int, name, source string) ProxyFixture {
		approvalID := ""
		if source == "contract" {
			approvalID = "second"
		}
		return ProxyFixture{
			Version: 1, MatrixLine: line, Name: name, Source: source, ApprovalID: approvalID,
			Listener: ProxyListener{Port: 8080, Transport: "tcp"}, Config: json.RawMessage(`{"site":"fixture"}`),
			Client:   ProxyClientObservation{Method: "GET", Host: "fixture.example.invalid", Path: "/", Status: 200},
			Upstream: ProxyUpstreamObservation{Attempted: true, Method: "GET", Host: "upstream.fixture.invalid", Path: "/", Status: 200},
			History:  ProxyHistoryObservation{Status: 200, StatusSource: "Upstream"},
		}
	}
	approvals := []ApprovedDeviation{{ID: "second", Kind: "proxy", MatrixLine: rows[1].Line}}
	requirements := []ProxyFixtureRequirement{{MatrixLine: rows[0].Line, Name: "first"}, {MatrixLine: rows[1].Line, Name: "second"}}
	if err := VerifyProxyFixtureCoverage(rows, requirements, []ProxyFixture{fixture(rows[0].Line, "first", "dotnet"), fixture(rows[1].Line, "second", "contract")}, approvals); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyProxyFixtureCoverageRejectsInvalidPlanCases(t *testing.T) {
	rows := []ProxyMatrixRow{{Line: 10, Text: "| test |"}}
	if err := VerifyProxyFixtureCoverage(rows, []ProxyFixtureRequirement{{Name: ""}}, nil, nil); err == nil {
		t.Fatal("unnamed capture-plan case was accepted")
	}
	if err := VerifyProxyFixtureCoverage(rows, []ProxyFixtureRequirement{{Name: "first"}, {Name: "first"}}, nil, nil); err == nil {
		t.Fatal("duplicate capture-plan case was accepted")
	}
	if err := VerifyProxyFixtureCoverage(rows, []ProxyFixtureRequirement{{Name: "missing"}}, nil, nil); err == nil {
		t.Fatal("uncovered capture-plan case was accepted")
	}
}

func TestProxyFixtureRejectsMetadataOnlyEntry(t *testing.T) {
	fixture := ProxyFixture{Version: 1, MatrixLine: 1, Name: "metadata-only", Source: "dotnet"}
	if err := fixture.Validate(); err == nil {
		t.Fatal("metadata-only fixture was accepted")
	}
}

func TestProxyFixtureRejectsSecretConfig(t *testing.T) {
	fixture := ProxyFixture{
		Version: 1, MatrixLine: 1, Name: "secret", Source: "dotnet",
		Listener: ProxyListener{Port: 8080, Transport: "tcp"}, Config: json.RawMessage(`{"password":"no"}`),
		Client:   ProxyClientObservation{Method: "GET", Host: "fixture.example.invalid", Path: "/", Status: 200},
		Upstream: ProxyUpstreamObservation{Attempted: false},
		History:  ProxyHistoryObservation{Status: 200, StatusSource: "SimpleResponse"},
	}
	if err := fixture.Validate(); err == nil {
		t.Fatal("secret config was accepted")
	}
}

func TestProxyFixtureAcceptsReservedProxyStatus(t *testing.T) {
	fixture := ProxyFixture{
		Version: 1, MatrixLine: 168, Name: "request-failure", Source: "dotnet",
		Listener: ProxyListener{Port: 8080, Transport: "tcp"}, Config: json.RawMessage(`{"defaultDestination":"http://127.0.0.1:1"}`),
		Client:   ProxyClientObservation{Method: "GET", Host: "fixture.example.invalid", Path: "/", Status: 604},
		Upstream: ProxyUpstreamObservation{Attempted: false},
		History:  ProxyHistoryObservation{Status: 604, StatusSource: "YaginxProxy", ErrorCategory: "UpstreamRequestFailure"},
	}
	if err := fixture.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRedactProxyFixture(t *testing.T) {
	fixture := ProxyFixture{
		Client:   ProxyClientObservation{Headers: map[string]string{"Date": "dynamic", "Authorization": "secret"}, Trailers: map[string]string{"X-Token": "secret"}, Body: json.RawMessage(`{"email":"person@example.invalid"}`)},
		Upstream: ProxyUpstreamObservation{Headers: map[string]string{"Date": "dynamic"}},
	}
	if err := RedactProxyFixture(&fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Client.Headers["Date"] != "${NOW}" || fixture.Client.Headers["Authorization"] != "${REDACTED_AUTHORIZATION}" {
		t.Fatalf("client headers = %#v", fixture.Client.Headers)
	}
	if fixture.Upstream.Headers["Date"] != "${NOW}" {
		t.Fatalf("upstream headers = %#v", fixture.Upstream.Headers)
	}
	if fixture.Client.Trailers["X-Token"] != "${REDACTED_X_TOKEN}" {
		t.Fatalf("client trailers = %#v", fixture.Client.Trailers)
	}
	if got, want := string(fixture.Client.Body), `{"email":"${REDACTED_EMAIL}"}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestRedactProxyFixturePreservesPlainTextBody(t *testing.T) {
	fixture := ProxyFixture{
		Client: ProxyClientObservation{Body: json.RawMessage(`"fixture-simple-response"`)},
	}
	if err := RedactProxyFixture(&fixture); err != nil {
		t.Fatal(err)
	}
	if got, want := string(fixture.Client.Body), `"fixture-simple-response"`; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestLoadProxyFixturesIgnoresCaptureTemplate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "capture-plan.template.json"), []byte(`{"cases":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fixtures, err := LoadProxyFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != 0 {
		t.Fatalf("fixtures = %#v", fixtures)
	}
}
