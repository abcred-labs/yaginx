package v2

import (
	"strings"
	"testing"
)

func TestBaselineOwnsOnlyV2MigrationLineage(t *testing.T) {
	contents, err := Files.ReadFile("00001_v2_baseline.sql")
	if err != nil {
		t.Fatal(err)
	}
	baseline := string(contents)
	for _, required := range []string{
		"-- +goose Up",
		"yaginx_schema_version",
		"generation = 2",
		"gateway_emit_config_event",
		"schema_name := TG_TABLE_SCHEMA",
		"%I.gateway_config_event",
		"gateway_ip_blacklist",
	} {
		if !strings.Contains(baseline, required) {
			t.Fatalf("v2 baseline is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"gateway_schema_version",
		"DefaultHost",
		"IsWithOriginalHostHeader",
		"legacy_pfx",
		"IF NOT EXISTS",
		"ALTER TABLE",
		"yaginx_publish_config_event",
	} {
		if strings.Contains(baseline, forbidden) {
			t.Fatalf("v2 baseline retains forbidden v1 contract text %q", forbidden)
		}
	}
}

func TestBaselineDeclaresForeignKeyTargetsBeforeUse(t *testing.T) {
	contents, err := Files.ReadFile("00001_v2_baseline.sql")
	if err != nil {
		t.Fatal(err)
	}
	baseline := string(contents)
	for _, check := range []struct {
		unique  string
		foreign string
	}{
		{"CONSTRAINT ux_gateway_acme_account_id UNIQUE (id)", "FOREIGN KEY (account_id) REFERENCES gateway_acme_account(id)"},
		{"CONSTRAINT ux_gateway_certificate_id UNIQUE (id)", "FOREIGN KEY (replaced_by_id) REFERENCES gateway_certificate(id)"},
		{"CONSTRAINT ux_gateway_certificate_id UNIQUE (id)", "FOREIGN KEY (certificate_id) REFERENCES gateway_certificate(id)"},
	} {
		uniqueAt, foreignAt := strings.Index(baseline, check.unique), strings.Index(baseline, check.foreign)
		if uniqueAt < 0 || foreignAt < 0 || uniqueAt > foreignAt {
			t.Fatalf("baseline must declare %q before %q", check.unique, check.foreign)
		}
	}
	for _, redundant := range []string{
		"CREATE UNIQUE INDEX ux_gateway_acme_account_id",
		"CREATE UNIQUE INDEX ux_gateway_certificate_id",
	} {
		if strings.Contains(baseline, redundant) {
			t.Fatalf("baseline retains redundant unique index %q", redundant)
		}
	}
}

func TestSystemErrorCompatibilityMigrationIsEmbedded(t *testing.T) {
	contents, err := Files.ReadFile("00004_system_errors.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, required := range []string{
		"-- +goose Up",
		"yaginx_system_error",
		"yaginx_system_error_metric",
		"-- +goose Down",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("system error migration is missing %q", required)
		}
	}
}

func TestWebsiteUpgradeMigrationsCoverEveryCollection(t *testing.T) {
	for _, name := range []string{"00005_website_schema_version.sql", "00006_website_config_upgrade.sql", "00007_managed_web_domain_index.sql", "00008_api_credentials.sql"} {
		if _, err := Files.ReadFile(name); err != nil {
			t.Fatal(err)
		}
	}
	contents, err := Files.ReadFile("00006_website_config_upgrade.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, required := range []string{"simple_responses", "proxy_transforms", "schemaVersion", "data_schema_version=2", "website_config_migration_error"} {
		if !strings.Contains(text, required) {
			t.Fatalf("website upgrade migration is missing %q", required)
		}
	}
}
