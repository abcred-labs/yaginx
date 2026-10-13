package main

import (
	"strings"
	"testing"
)

func TestPlansNeverReadKnownSecretPayloads(t *testing.T) {
	for _, plan := range plans {
		query := strings.ToLower(plan.selectSQL)
		for _, forbidden := range []string{
			"password,", "private_key", "certificate_pem", "code_hash", "code_salt",
			"request_headers", "response_headers", "request_body_base64", "response_body_base64",
			"exception_message", "exception_stack_trace",
		} {
			if strings.Contains(query, forbidden) {
				t.Fatalf("plan %s selects forbidden payload %q: %s", plan.name, forbidden, plan.selectSQL)
			}
		}
	}
	if !strings.Contains(plans[0].insertSQL, "password=NULL") {
		t.Fatal("account import must clear legacy plaintext password")
	}
	if !strings.Contains(plans[5].insertSQL, "code_hash=NULL") {
		t.Fatal("recovery import must clear recovery code material")
	}
}

func TestPlansUseStableConflictKeys(t *testing.T) {
	for _, plan := range plans {
		if plan.name == "" || plan.conflict == "" || len(plan.columns) == 0 || len(plan.requires) == 0 {
			t.Fatalf("incomplete import plan: %#v", plan)
		}
		if !strings.Contains(strings.ToLower(plan.insertSQL), "on conflict") {
			t.Fatalf("plan %s is not restartable", plan.name)
		}
	}
}
