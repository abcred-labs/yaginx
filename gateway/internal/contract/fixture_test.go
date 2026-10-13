package contract

import "testing"

func TestFixtureValidationRedactsSecrets(t *testing.T) {
	fixture := Fixture{
		Version: 1, OperationID: "accountLogin", Scenario: "success", Source: "dotnet",
		Request:  FixtureRequest{Method: "POST", Path: "/api/account/login", Body: []byte(`{"email":"${EMAIL}","password":"${PASSWORD}"}`)},
		Response: FixtureResponse{Status: 200, Body: []byte(`{"token":"${TOKEN}"}`)},
	}
	if err := fixture.Validate(); err != nil {
		t.Fatal(err)
	}
	fixture.Request.Body = []byte(`{"password":"not-redacted"}`)
	if err := fixture.Validate(); err == nil {
		t.Fatal("unredacted password was accepted")
	}
}

func TestRedactFixture(t *testing.T) {
	fixture := Fixture{
		Request:  FixtureRequest{Headers: map[string]string{"Authorization": "Bearer secret"}, Body: []byte(`{"email":"person@example.com","nested":{"password":"secret"}}`)},
		Response: FixtureResponse{Headers: map[string]string{"Set-Cookie": "token=secret", "Date": "Sun, 26 Jul 2026", "Duration": "9ms"}, Body: []byte(`{"tid":"dynamic","data":{"email":"person@example.com"}}`)},
	}
	if err := RedactFixture(&fixture); err != nil {
		t.Fatal(err)
	}
	if got := string(fixture.Request.Body); got != `{"email":"${REDACTED_EMAIL}","nested":{"password":"${REDACTED_PASSWORD}"}}` {
		t.Fatalf("request body = %s", got)
	}
	if fixture.Request.Headers["Authorization"] != "${REDACTED_AUTHORIZATION}" {
		t.Fatalf("authorization header was not redacted: %#v", fixture.Request.Headers)
	}
	if fixture.Response.Headers["Date"] != "${NOW}" || fixture.Response.Headers["Duration"] != "${RUNTIME_METADATA}" {
		t.Fatalf("runtime headers were not normalized: %#v", fixture.Response.Headers)
	}
	if got := string(fixture.Response.Body); got != `{"data":{"email":"${REDACTED_EMAIL}"},"tid":"${TID}"}` {
		t.Fatalf("response body = %s", got)
	}
}

func TestRedactFixtureRedactsEmailInNonPIIField(t *testing.T) {
	fixture := Fixture{Response: FixtureResponse{Body: []byte(`{"name":"person@example.invalid"}`)}}
	if err := RedactFixture(&fixture); err != nil {
		t.Fatal(err)
	}
	if got, want := string(fixture.Response.Body), `{"name":"${REDACTED_EMAIL}"}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
	fixture.Response.Body = []byte(`{"name":"person@example.invalid"}`)
	if err := fixture.Validate(); err == nil {
		t.Fatal("unredacted email in a non-PII field was accepted")
	}
}

func TestVerifyFixtureCoverage(t *testing.T) {
	entries := []InventoryEntry{
		{Operation: Operation{OperationID: "fixture", GoHandler: "x", FixtureRequired: true}},
		{Operation: Operation{OperationID: "unmarked", GoHandler: "x"}},
	}
	fixtures := []Fixture{
		{OperationID: "fixture", Scenario: "success"},
		{OperationID: "fixture", Scenario: "auth"},
	}
	if err := VerifyFixtureCoverage(entries, fixtures, nil); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFixtureCoverage(entries[:1], fixtures[:1], nil); err == nil {
		t.Fatal("success-only fixture coverage was accepted")
	}
}

func TestApprovedAdapterFixtureRequiresMatchingApproval(t *testing.T) {
	entries := []InventoryEntry{{Operation: Operation{OperationID: "fixture", GoHandler: "x", FixtureRequired: true}}}
	fixtures := []Fixture{
		{OperationID: "fixture", Scenario: "success", Source: "dotnet"},
		{OperationID: "fixture", Scenario: "auth", Source: "approved-test-adapter", ApprovalID: "fixture-auth"},
	}
	approvals := []ApprovedDeviation{{ID: "fixture-auth", Kind: "api", OperationID: "fixture", Requirement: "failure"}}
	if err := VerifyFixtureCoverage(entries, fixtures, approvals); err != nil {
		t.Fatal(err)
	}
	fixtures[1].ApprovalID = "not-approved"
	if err := VerifyFixtureCoverage(entries, fixtures, approvals); err == nil {
		t.Fatal("unapproved adapter fixture was accepted")
	}
}

func TestFixtureScenarioMatchesHTTPOutcome(t *testing.T) {
	fixture := Fixture{
		Version: 1, OperationID: "fixture", Scenario: "success", Source: "dotnet",
		Request:  FixtureRequest{Method: "GET", Path: "/api/fixture"},
		Response: FixtureResponse{Status: 500},
	}
	if err := fixture.Validate(); err == nil {
		t.Fatal("non-success response was accepted as success")
	}
	fixture.Scenario = "business_error"
	fixture.Response.Status = 200
	if err := fixture.Validate(); err == nil {
		t.Fatal("success response was accepted as a failure scenario")
	}
}
