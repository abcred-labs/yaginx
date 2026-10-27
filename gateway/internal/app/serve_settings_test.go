package app

import (
	"testing"

	"yaginx-gateway/internal/config"
)

func TestParseServeSettingsAcceptsThreeDayContributionRetention(t *testing.T) {
	values, err := config.NewRuntimeValues("postgres://test@127.0.0.1/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	values["resourceReports"].(map[string]any)["contributionRetention"] = "72h"
	if _, err := parseServeSettings(config.RuntimeDocument{Values: values}); err != nil {
		t.Fatalf("parse 72h contribution retention: %v", err)
	}
}

func TestParseServeSettingsRejectsContributionRetentionBelowThreeDays(t *testing.T) {
	values, err := config.NewRuntimeValues("postgres://test@127.0.0.1/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	values["resourceReports"].(map[string]any)["contributionRetention"] = "71h"
	if _, err := parseServeSettings(config.RuntimeDocument{Values: values}); err == nil {
		t.Fatal("parse accepted contribution retention below 72h")
	}
}

func TestParseServeSettingsRejectsContributionRetentionAboveOneYear(t *testing.T) {
	values, err := config.NewRuntimeValues("postgres://test@127.0.0.1/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	values["resourceReports"].(map[string]any)["contributionRetention"] = "8761h"
	if _, err := parseServeSettings(config.RuntimeDocument{Values: values}); err == nil {
		t.Fatal("parse accepted contribution retention above 8760h")
	}
}
