package database

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

type v1SchemaQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// v1SchemaRequirements describes only the immediately preceding stable Go
// schema. It is intentionally not a general legacy-schema detector.
var v1SchemaRequirements = map[string][]string{
	"account":                   {"id", "email", "password", "password_hash", "password_salt"},
	"web_domain":                {"id", "name", "is_use_free_cert", "is_verified", "free_cert_message"},
	"website":                   {"id", "name", "specifications", "hosts", "proxy_rules", "create_time", "update_time", "proxy_transforms", "simple_responses"},
	"resource_monitor_info":     {"resource_uuid", "session_key", "create_ts", "data"},
	"resource_report":           {"resource_uuid", "cycle_type", "report_time", "report_ts", "request_qty", "status_code", "spider", "browser", "os", "duration", "create_time", "update_time"},
	"account_password_recovery": {"id", "recovery_id", "account_id", "account_email", "email_verified", "code_hash", "code_salt", "code_expire_ts", "failed_attempts", "status", "create_ts", "update_ts"},
	"host_traffic":              {"id", "host_name", "period", "request_counts", "inbound_bytes", "outbound_bytes"},
	"request_history":           {"id", "request_time", "request_ts", "duration_ms", "client_ip", "method", "scheme", "host", "path", "query_string", "url", "protocol", "status_code", "user_agent", "proxy_stage", "exception_stage", "forwarder_error", "request_headers", "response_headers", "request_body_base64", "response_body_base64", "request_content_type", "response_content_type", "request_body_truncated", "response_body_truncated", "exception_type", "exception_message", "exception_stack_trace"},
	"gateway_ip_blacklist":      {"id", "cidr", "profile", "blocked_count", "last_blocked_at", "created_at", "removed_at"},
}

var v1CriticalTypes = map[string]map[string]string{
	"account":                   {"id": "int8"},
	"web_domain":                {"id": "int8", "is_use_free_cert": "bool", "is_verified": "bool"},
	"website":                   {"id": "int8", "specifications": "jsonb", "hosts": "jsonb", "proxy_rules": "jsonb", "proxy_transforms": "jsonb", "simple_responses": "jsonb"},
	"resource_monitor_info":     {"create_ts": "int8", "data": "jsonb"},
	"resource_report":           {"cycle_type": "int4", "report_ts": "int8", "request_qty": "int8", "status_code": "jsonb", "spider": "jsonb", "browser": "jsonb", "os": "jsonb", "duration": "jsonb"},
	"account_password_recovery": {"id": "int8", "status": "int4", "failed_attempts": "int4"},
	"host_traffic":              {"id": "int8", "period": "int8", "request_counts": "int8", "inbound_bytes": "int8", "outbound_bytes": "int8"},
	"request_history":           {"id": "int8", "request_ts": "int8", "duration_ms": "int8", "status_code": "int4", "request_headers": "jsonb", "response_headers": "jsonb"},
}

func v1TableNames() []string {
	names := make([]string, 0, len(v1SchemaRequirements))
	for name := range v1SchemaRequirements {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidateCurrentV1Schema identifies the historical stable Go schema so 1.x
// can provide a precise upgrade message instead of treating it as unknown.
func ValidateCurrentV1Schema(ctx context.Context, db *sql.DB) error {
	return validateCurrentV1Schema(ctx, db)
}

func validateCurrentV1Schema(ctx context.Context, db v1SchemaQueryer) error {
	for _, table := range v1TableNames() {
		columns, err := tableColumns(ctx, db, table)
		if err != nil {
			return err
		}
		missing := make([]string, 0)
		for _, required := range v1SchemaRequirements[table] {
			if _, ok := columns[required]; !ok {
				missing = append(missing, required)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("current-v1 table %q is missing columns: %s", table, strings.Join(missing, ", "))
		}
		if err := validateV1ColumnTypes(ctx, db, table); err != nil {
			return err
		}
	}

	var duplicates bool
	if err := db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM host_traffic
  GROUP BY host_name, period
  HAVING count(*) > 1
)`).Scan(&duplicates); err != nil {
		return fmt.Errorf("inspect current-v1 host_traffic duplicates: %w", err)
	}
	if duplicates {
		return fmt.Errorf("current-v1 host_traffic contains duplicate (host_name, period) rows; resolve them before v2 migration")
	}
	return nil
}

func validateV1ColumnTypes(ctx context.Context, db v1SchemaQueryer, table string) error {
	for column, expected := range v1CriticalTypes[table] {
		var actual string
		err := db.QueryRowContext(ctx, `SELECT udt_name FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name=$2`, table, column).Scan(&actual)
		if err != nil {
			return fmt.Errorf("inspect current-v1 type %q.%q: %w", table, column, err)
		}
		if actual != expected {
			return fmt.Errorf("current-v1 type %q.%q: got %s, want %s; inspect data and repair manually before v2 migration", table, column, actual, expected)
		}
	}
	return nil
}

func ValidateGatewaySchema(ctx context.Context, db *sql.DB) error {
	requirements := map[string][]string{
		"gateway_node":                 {"node_id", "machine_fingerprint", "status", "node_number", "reload_generation", "observed_reload_generation"},
		"gateway_config_revision":      {"singleton_id", "revision", "updated_at"},
		"gateway_config_event":         {"id", "resource_type", "resource_id", "operation", "transaction_id", "created_at"},
		"gateway_certificate":          {"id", "fingerprint_sha256", "dns_names", "certificate_pem", "private_key_pem", "is_active", "replaced_by_id"},
		"gateway_acme_account":         {"id", "directory_url", "account_url", "private_key_pem", "registration_json"},
		"gateway_certificate_order":    {"id", "domain_name", "status", "account_id", "node_id", "certificate_id", "error_class", "next_attempt_at"},
		"gateway_system_setting":       {"setting_key", "value", "content_type", "updated_by_account_id"},
		"gateway_runtime_material":     {"singleton_id", "jwt_current", "jwt_previous"},
		"gateway_traffic_contribution": {"node_id", "process_id", "host_name", "cycle_type", "period", "request_qty", "duration_buckets", "is_final"},
		"gateway_ip_blacklist":         {"id", "cidr", "profile", "blocked_count", "created_at"},
	}
	for table, required := range requirements {
		columns, err := tableColumns(ctx, db, table)
		if err != nil {
			return err
		}
		if len(columns) == 0 {
			return fmt.Errorf("gateway table %q is missing", table)
		}
		for _, column := range required {
			if _, ok := columns[column]; !ok {
				return fmt.Errorf("gateway table %q is missing column %q", table, column)
			}
		}
	}
	return nil
}

func ValidateAPICredentialSchema(ctx context.Context, db *sql.DB) error {
	columns, err := tableColumns(ctx, db, "gateway_api_credential")
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		return fmt.Errorf("gateway table %q is missing", "gateway_api_credential")
	}
	for _, column := range []string{"id", "name", "purpose", "token_prefix", "token_hash", "status", "created_at", "updated_at", "revoked_at"} {
		if _, ok := columns[column]; !ok {
			return fmt.Errorf("gateway table %q is missing column %q", "gateway_api_credential", column)
		}
	}
	return nil
}

// ValidateV2Schema verifies the final lifecycle contract, including the
// database objects that make configuration changes observable at runtime.
func ValidateV2Schema(ctx context.Context, db *sql.DB) error {
	if err := ValidateGatewaySchema(ctx, db); err != nil {
		return err
	}
	if err := ValidateAPICredentialSchema(ctx, db); err != nil {
		return err
	}
	for table, forbidden := range map[string]string{
		"gateway_config_revision": "singleton",
	} {
		columns, err := tableColumns(ctx, db, table)
		if err != nil {
			return err
		}
		if _, exists := columns[forbidden]; exists {
			return fmt.Errorf("v2 table %q still has legacy column %q", table, forbidden)
		}
	}
	for _, constraint := range []string{
		"ck_gateway_config_event_operation",
		"ck_gateway_config_event_resource_type",
		"gateway_config_revision_pkey",
	} {
		var exists bool
		if err := db.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM pg_constraint c
    JOIN pg_namespace n ON n.oid = c.connamespace
    WHERE n.nspname = current_schema() AND c.conname = $1
)`, constraint).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("v2 constraint %q is missing", constraint)
		}
	}
	for trigger, expected := range map[string]struct {
		table string
		args  string
	}{
		"trg_gateway_ip_blacklist_config_event":     {table: "gateway_ip_blacklist", args: "'id', 'gateway_ip_blacklist'"},
		"trg_gateway_runtime_material_config_event": {table: "gateway_runtime_material", args: "'singleton_id', 'gateway_runtime_material'"},
		"trg_website_config_event":                  {table: "website", args: "'id', 'website'"},
		"trg_web_domain_config_event":               {table: "web_domain", args: "'id', 'web_domain'"},
	} {
		var definition string
		err := db.QueryRowContext(ctx, `
SELECT pg_get_triggerdef(t.oid)
FROM pg_trigger t
JOIN pg_class c ON c.oid = t.tgrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = current_schema()
  AND c.relname = $1
  AND t.tgname = $2
  AND NOT t.tgisinternal`, expected.table, trigger).Scan(&definition)
		if err == sql.ErrNoRows {
			return fmt.Errorf("v2 trigger %q is missing", trigger)
		}
		if err != nil {
			return err
		}
		if !strings.Contains(definition, "EXECUTE FUNCTION gateway_emit_config_event("+expected.args+")") {
			return fmt.Errorf("v2 trigger %q has unexpected binding: %s", trigger, definition)
		}
	}
	var legacyFunction bool
	if err := db.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM pg_proc p
    JOIN pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname = current_schema()
      AND p.proname = 'yaginx_publish_config_event'
)`).Scan(&legacyFunction); err != nil {
		return err
	}
	if legacyFunction {
		return fmt.Errorf("v2 still has yaginx_publish_config_event")
	}
	return nil
}

func tableColumns(ctx context.Context, db v1SchemaQueryer, table string) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, `
SELECT column_name
FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = $1`, table)
	if err != nil {
		return nil, fmt.Errorf("inspect columns for %q: %w", table, err)
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}
