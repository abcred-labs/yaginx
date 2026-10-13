package main

import (
	"net/url"
	"testing"

	"yaginx-gateway/internal/contract"
)

func TestApplyNormalizers(t *testing.T) {
	fixture := contract.Fixture{Response: contract.FixtureResponse{Body: []byte(`{"tid":"dynamic","rows":[{"id":"42"}]}`)}}
	if err := applyNormalizers(&fixture, []normalizer{
		{Location: "response.body", Pointer: "/tid", Value: "${TID}"},
		{Location: "response.body", Pointer: "/rows/0/id", Value: "${RANDOM_ID}"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := string(fixture.Response.Body); got != `{"rows":[{"id":"${RANDOM_ID}"}],"tid":"${TID}"}` {
		t.Fatalf("normalized body = %s", got)
	}
}

func TestAPITargetPreservesQuery(t *testing.T) {
	base, err := url.Parse("http://127.0.0.1:18080")
	if err != nil {
		t.Fatal(err)
	}
	target, err := apiTarget(base, "/api/website/get?id=42")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := target.String(), "http://127.0.0.1:18080/yaginx_console/api/website/get?id=42"; got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

func TestSelectCases(t *testing.T) {
	cases := []captureCase{{OperationID: "one", Scenario: "success"}, {OperationID: "one", Scenario: "auth"}, {OperationID: "two", Scenario: "success"}}
	selected, err := selectCases(cases, "two.success,one.auth")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[0].Scenario != "auth" || selected[1].OperationID != "two" {
		t.Fatalf("unexpected selected cases: %#v", selected)
	}
	if _, err := selectCases(cases, "missing.success"); err == nil {
		t.Fatal("missing case was accepted")
	}
}

func TestResolveStringUsesOnlyDeclaredVariables(t *testing.T) {
	resolved, err := resolveString("Bearer ${token}", map[string]string{"token": "jwt-value"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "Bearer jwt-value" {
		t.Fatalf("resolved = %q", resolved)
	}
	if _, err := resolveString("Bearer ${missing}", nil); err == nil {
		t.Fatal("missing variable was accepted")
	}
}

func TestExtractJSONPointer(t *testing.T) {
	value, err := extractJSONPointer([]byte(`{"data":{"token":"jwt-value"}}`), "/data/token")
	if err != nil {
		t.Fatal(err)
	}
	if value != "jwt-value" {
		t.Fatalf("value = %q", value)
	}
	if _, err := extractJSONPointer([]byte(`{"data":{}}`), "/data/token"); err == nil {
		t.Fatal("missing pointer was accepted")
	}
}

func TestApplyNormalizersRejectsMissingPointer(t *testing.T) {
	fixture := contract.Fixture{Response: contract.FixtureResponse{Body: []byte(`{"tid":"dynamic"}`)}}
	if err := applyNormalizers(&fixture, []normalizer{{Location: "response.body", Pointer: "/missing", Value: "${VALUE}"}}); err == nil {
		t.Fatal("missing JSON pointer was accepted")
	}
}
