package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestRolesGrantV2RuntimeReadAccess(t *testing.T) {
	contents, err := os.ReadFile("roles.sql")
	if err != nil {
		t.Fatal(err)
	}
	roles := string(contents)
	for _, required := range []string{
		"GRANT SELECT ON yaginx_schema_version TO yaginx_gateway;",
		"GRANT SELECT ON yaginx_schema_migration TO yaginx_gateway;",
		"GRANT USAGE, CREATE ON SCHEMA public TO yaginx_migrator;",
		"gateway_ip_blacklist",
		"account_session",
	} {
		if !strings.Contains(roles, required) {
			t.Fatalf("roles.sql is missing %q", required)
		}
	}
	if strings.Contains(roles, "gateway_schema_version") {
		t.Fatal("roles.sql grants the removed v1 migration tracker")
	}
}
