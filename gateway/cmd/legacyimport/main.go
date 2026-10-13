// legacyimport imports a sanitized .NET/PostgreSQL database into a clean v2
// database.  The source connection is read-only and the target import is one
// transaction, so a failed import leaves the target unchanged.
//
// The importer deliberately does not select legacy plaintext passwords,
// recovery-code material, HTTP headers/bodies, or certificate/private-key
// material.  Those values are either discarded or must be re-created through
// the public runtime configuration after the cutover.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"yaginx-gateway/internal/database"
)

type tablePlan struct {
	name      string
	columns   []string
	selectSQL string
	insertSQL string
	requires  []string
	conflict  string
}

type importReport struct {
	Tables map[string]int64 `json:"tables"`
}

var plans = []tablePlan{
	{
		name: "account", columns: []string{"id", "email", "password_hash", "password_salt"},
		selectSQL: `SELECT id,email,password_hash,password_salt FROM account ORDER BY id`,
		insertSQL: `INSERT INTO account(id,email,password,password_hash,password_salt) VALUES($1,$2,NULL,$3,$4)
ON CONFLICT (id) DO UPDATE SET email=EXCLUDED.email,password=NULL,password_hash=EXCLUDED.password_hash,password_salt=EXCLUDED.password_salt`,
		requires: []string{"id", "email", "password", "password_hash", "password_salt"}, conflict: "id",
	},
	{
		name: "web_domain", columns: []string{"id", "name", "is_use_free_cert", "is_verified", "free_cert_message"},
		selectSQL: `SELECT id,name,is_use_free_cert,is_verified,free_cert_message FROM web_domain ORDER BY id`,
		insertSQL: `INSERT INTO web_domain(id,name,is_use_free_cert,is_verified,free_cert_message) VALUES($1,$2,$3,$4,$5)
ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name,is_use_free_cert=EXCLUDED.is_use_free_cert,is_verified=EXCLUDED.is_verified,free_cert_message=EXCLUDED.free_cert_message`,
		requires: []string{"id", "name", "is_use_free_cert", "is_verified", "free_cert_message"}, conflict: "id",
	},
	{
		name: "website", columns: []string{"id", "name", "specifications", "hosts", "proxy_rules", "create_time", "update_time", "proxy_transforms", "simple_responses"},
		selectSQL: `SELECT id,name,specifications,hosts,proxy_rules,create_time,update_time,proxy_transforms,simple_responses FROM website ORDER BY id`,
		insertSQL: `INSERT INTO website(id,name,specifications,hosts,proxy_rules,create_time,update_time,proxy_transforms,simple_responses,data_schema_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,2)
ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name,specifications=EXCLUDED.specifications,hosts=EXCLUDED.hosts,proxy_rules=EXCLUDED.proxy_rules,create_time=EXCLUDED.create_time,update_time=EXCLUDED.update_time,proxy_transforms=EXCLUDED.proxy_transforms,simple_responses=EXCLUDED.simple_responses,data_schema_version=2`,
		requires: []string{"id", "name", "specifications", "hosts", "proxy_rules", "create_time", "update_time", "proxy_transforms", "simple_responses"}, conflict: "id",
	},
	{
		name: "resource_monitor_info", columns: []string{"resource_uuid", "session_key", "create_ts", "data"},
		selectSQL: `SELECT resource_uuid,session_key,create_ts,data FROM resource_monitor_info ORDER BY resource_uuid,create_ts`,
		insertSQL: `INSERT INTO resource_monitor_info(resource_uuid,session_key,create_ts,data) VALUES($1,$2,$3,$4)
ON CONFLICT (resource_uuid,create_ts) DO UPDATE SET session_key=EXCLUDED.session_key,data=EXCLUDED.data`,
		requires: []string{"resource_uuid", "session_key", "create_ts", "data"}, conflict: "resource_uuid,create_ts",
	},
	{
		name: "resource_report", columns: []string{"resource_uuid", "cycle_type", "report_time", "report_ts", "request_qty", "status_code", "spider", "browser", "os", "duration", "create_time", "update_time"},
		selectSQL: `SELECT resource_uuid,cycle_type,report_time,report_ts,request_qty,status_code,spider,browser,os,duration,create_time,update_time FROM resource_report ORDER BY resource_uuid,cycle_type,report_ts`,
		insertSQL: `INSERT INTO resource_report(resource_uuid,cycle_type,report_time,report_ts,request_qty,status_code,spider,browser,os,duration,create_time,update_time) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (resource_uuid,cycle_type,report_ts) DO UPDATE SET report_time=EXCLUDED.report_time,request_qty=EXCLUDED.request_qty,status_code=EXCLUDED.status_code,spider=EXCLUDED.spider,browser=EXCLUDED.browser,os=EXCLUDED.os,duration=EXCLUDED.duration,create_time=EXCLUDED.create_time,update_time=EXCLUDED.update_time`,
		requires: []string{"resource_uuid", "cycle_type", "report_time", "report_ts", "request_qty", "status_code", "spider", "browser", "os", "duration", "create_time", "update_time"}, conflict: "resource_uuid,cycle_type,report_ts",
	},
	{
		name: "account_password_recovery", columns: []string{"id", "recovery_id", "account_id", "account_email", "email_verified", "code_expire_ts", "failed_attempts", "status", "create_ts", "update_ts"},
		selectSQL: `SELECT id,recovery_id,account_id,account_email,email_verified,code_expire_ts,failed_attempts,status,create_ts,update_ts FROM account_password_recovery ORDER BY id`,
		insertSQL: `INSERT INTO account_password_recovery(id,recovery_id,account_id,account_email,email_verified,code_hash,code_salt,code_expire_ts,failed_attempts,status,create_ts,update_ts) VALUES($1,$2,$3,$4,$5,NULL,NULL,$6,$7,$8,$9,$10)
ON CONFLICT (recovery_id) DO UPDATE SET id=EXCLUDED.id,account_id=EXCLUDED.account_id,account_email=EXCLUDED.account_email,email_verified=EXCLUDED.email_verified,code_hash=NULL,code_salt=NULL,code_expire_ts=EXCLUDED.code_expire_ts,failed_attempts=EXCLUDED.failed_attempts,status=EXCLUDED.status,create_ts=EXCLUDED.create_ts,update_ts=EXCLUDED.update_ts`,
		requires: []string{"id", "recovery_id", "account_id", "account_email", "email_verified", "code_hash", "code_salt", "code_expire_ts", "failed_attempts", "status", "create_ts", "update_ts"}, conflict: "recovery_id",
	},
	{
		name: "host_traffic", columns: []string{"id", "host_name", "period", "request_counts", "inbound_bytes", "outbound_bytes"},
		selectSQL: `SELECT id,host_name,period,request_counts,inbound_bytes,outbound_bytes FROM host_traffic ORDER BY id`,
		insertSQL: `INSERT INTO host_traffic(id,host_name,period,request_counts,inbound_bytes,outbound_bytes) VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT (host_name,period) DO UPDATE SET id=EXCLUDED.id,request_counts=EXCLUDED.request_counts,inbound_bytes=EXCLUDED.inbound_bytes,outbound_bytes=EXCLUDED.outbound_bytes`,
		requires: []string{"id", "host_name", "period", "request_counts", "inbound_bytes", "outbound_bytes"}, conflict: "host_name,period",
	},
	{
		name: "request_history", columns: []string{"id", "request_time", "request_ts", "duration_ms", "client_ip", "method", "scheme", "host", "path", "query_string", "url", "protocol", "status_code", "user_agent", "proxy_stage", "exception_stage", "forwarder_error", "request_content_type", "response_content_type", "request_body_truncated", "response_body_truncated", "exception_type"},
		// Headers, bodies, exception text and stack traces are intentionally not selected.
		selectSQL: `SELECT id,request_time,request_ts,duration_ms,client_ip,method,scheme,host,path,query_string,url,protocol,status_code,user_agent,proxy_stage,exception_stage,forwarder_error,request_content_type,response_content_type,request_body_truncated,response_body_truncated,exception_type FROM request_history ORDER BY id`,
		insertSQL: `INSERT INTO request_history(id,request_time,request_ts,duration_ms,client_ip,method,scheme,host,path,query_string,url,protocol,status_code,user_agent,proxy_stage,exception_stage,forwarder_error,request_content_type,response_content_type,request_body_truncated,response_body_truncated,exception_type) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
ON CONFLICT (id) DO UPDATE SET request_time=EXCLUDED.request_time,request_ts=EXCLUDED.request_ts,duration_ms=EXCLUDED.duration_ms,client_ip=EXCLUDED.client_ip,method=EXCLUDED.method,scheme=EXCLUDED.scheme,host=EXCLUDED.host,path=EXCLUDED.path,query_string=EXCLUDED.query_string,url=EXCLUDED.url,protocol=EXCLUDED.protocol,status_code=EXCLUDED.status_code,user_agent=EXCLUDED.user_agent,proxy_stage=EXCLUDED.proxy_stage,exception_stage=EXCLUDED.exception_stage,forwarder_error=EXCLUDED.forwarder_error,request_content_type=EXCLUDED.request_content_type,response_content_type=EXCLUDED.response_content_type,request_body_truncated=EXCLUDED.request_body_truncated,response_body_truncated=EXCLUDED.response_body_truncated,exception_type=EXCLUDED.exception_type`,
		requires: []string{"id", "request_time", "request_ts", "duration_ms", "client_ip", "method", "scheme", "host", "path", "query_string", "url", "protocol", "status_code", "user_agent", "proxy_stage", "exception_stage", "forwarder_error", "request_headers", "response_headers", "request_body_base64", "response_body_base64", "request_content_type", "response_content_type", "request_body_truncated", "response_body_truncated", "exception_type", "exception_message", "exception_stack_trace"}, conflict: "id",
	},
}

func main() {
	sourceEnv := flag.String("source-dsn-env", "YAGINX_LEGACY_DATABASE_URL", "environment variable containing the legacy PostgreSQL DSN")
	targetEnv := flag.String("target-dsn-env", "YAGINX_TARGET_DATABASE_URL", "environment variable containing the public PostgreSQL DSN")
	timeout := flag.Duration("timeout", 10*time.Minute, "maximum import duration")
	flag.Parse()
	source, target := os.Getenv(*sourceEnv), os.Getenv(*targetEnv)
	if source == "" || target == "" {
		fatal("both %s and %s must be set", *sourceEnv, *targetEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := importDatabase(ctx, source, target)
	if err != nil {
		fatal("import failed: %v", err)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatal("encode report: %v", err)
	}
	fmt.Println(string(encoded))
}

func importDatabase(ctx context.Context, sourceDSN, targetDSN string) (importReport, error) {
	sourceConfig, err := pgx.ParseConfig(sourceDSN)
	if err != nil {
		return importReport{}, fmt.Errorf("parse source database configuration: %w", err)
	}
	if sourceConfig.RuntimeParams == nil {
		sourceConfig.RuntimeParams = map[string]string{}
	}
	sourceConfig.RuntimeParams["default_transaction_read_only"] = "on"
	sourceConn, err := pgx.ConnectConfig(ctx, sourceConfig)
	if err != nil {
		return importReport{}, fmt.Errorf("connect source database: %w", err)
	}
	defer sourceConn.Close(context.Background())
	sourceTx, err := sourceConn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return importReport{}, fmt.Errorf("begin read-only source transaction: %w", err)
	}
	defer sourceTx.Rollback(context.Background())
	if err := validateSource(ctx, sourceTx); err != nil {
		return importReport{}, err
	}

	state, err := database.DatabaseState(ctx, targetDSN)
	if err != nil {
		return importReport{}, fmt.Errorf("inspect target database: %w", err)
	}
	switch state {
	case database.StateUninitialized:
		if err := database.InitializeV2(ctx, targetDSN, nilWriter{}); err != nil {
			return importReport{}, fmt.Errorf("initialize target v2 database: %w", err)
		}
	case database.StateCurrentV2, database.StateCurrentV2Pending:
		if err := database.MigrateV2(ctx, targetDSN, nilWriter{}); err != nil {
			return importReport{}, fmt.Errorf("upgrade target v2 database: %w", err)
		}
	default:
		return importReport{}, fmt.Errorf("target database must be empty or current-v2; found %s", state)
	}

	targetConn, err := pgx.Connect(ctx, targetDSN)
	if err != nil {
		return importReport{}, fmt.Errorf("connect target database: %w", err)
	}
	defer targetConn.Close(context.Background())
	targetTx, err := targetConn.Begin(ctx)
	if err != nil {
		return importReport{}, fmt.Errorf("begin target transaction: %w", err)
	}
	defer targetTx.Rollback(context.Background())
	result := importReport{Tables: make(map[string]int64, len(plans))}
	for _, plan := range plans {
		count, err := copyTable(ctx, sourceTx, targetTx, plan)
		if err != nil {
			return importReport{}, fmt.Errorf("copy %s: %w", plan.name, err)
		}
		result.Tables[plan.name] = count
	}
	if err := repairSequences(ctx, targetTx); err != nil {
		return importReport{}, fmt.Errorf("repair target sequences: %w", err)
	}
	if err := targetTx.Commit(ctx); err != nil {
		return importReport{}, fmt.Errorf("commit target transaction: %w", err)
	}
	return result, nil
}

func validateSource(ctx context.Context, tx pgx.Tx) error {
	for _, plan := range plans {
		rows, err := tx.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1`, plan.name)
		if err != nil {
			return fmt.Errorf("inspect source table %q: %w", plan.name, err)
		}
		seen := map[string]bool{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return err
			}
			seen[name] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return fmt.Errorf("read source table %q columns: %w", plan.name, err)
		}
		missing := make([]string, 0)
		for _, required := range plan.requires {
			if !seen[required] {
				missing = append(missing, required)
			}
		}
		if len(missing) != 0 {
			return fmt.Errorf("source table %q is missing required columns: %s", plan.name, strings.Join(missing, ", "))
		}
	}
	for _, check := range []struct{ table, key, predicate string }{
		{"web_domain", "name", "name IS NOT NULL"},
		{"website", "name", "name IS NOT NULL"},
		{"host_traffic", "host_name, period", "host_name IS NOT NULL"},
	} {
		var duplicate bool
		query := fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE %s GROUP BY %s HAVING count(*) > 1)", pgx.Identifier{check.table}.Sanitize(), check.predicate, check.key)
		if err := tx.QueryRow(ctx, query).Scan(&duplicate); err != nil {
			return fmt.Errorf("check source %s duplicates: %w", check.table, err)
		}
		if duplicate {
			return fmt.Errorf("source table %q contains duplicate %s rows", check.table, check.key)
		}
	}
	return nil
}

func copyTable(ctx context.Context, source pgx.Tx, target pgx.Tx, plan tablePlan) (int64, error) {
	rows, err := source.Query(ctx, plan.selectSQL)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var count int64
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return 0, err
		}
		if len(values) != len(plan.columns) {
			return 0, fmt.Errorf("source returned %d columns, expected %d", len(values), len(plan.columns))
		}
		if _, err := target.Exec(ctx, plan.insertSQL, values...); err != nil {
			return 0, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func repairSequences(ctx context.Context, tx pgx.Tx) error {
	for _, table := range []string{"account", "web_domain", "website", "account_password_recovery", "host_traffic"} {
		query := fmt.Sprintf(`SELECT setval(pg_get_serial_sequence('%s','id'), GREATEST(COALESCE((SELECT max(id) FROM %s),0),1), (SELECT count(*) > 0 FROM %s))`, table, pgx.Identifier{table}.Sanitize(), pgx.Identifier{table}.Sanitize())
		if _, err := tx.Exec(ctx, query); err != nil {
			return fmt.Errorf("set %s sequence: %w", table, err)
		}
	}
	if _, err := tx.Exec(ctx, `SELECT setval('gateway_request_history_id_seq', GREATEST(COALESCE((SELECT max(id) FROM request_history),0),1000000000000000), true)`); err != nil {
		return fmt.Errorf("set request history sequence: %w", err)
	}
	return nil
}

// nilWriter prevents migration diagnostics from becoming an accidental data
// channel. The command prints only table counts after the import commits.
type nilWriter struct{}

func (nilWriter) Write(p []byte) (int, error) { return len(p), nil }

func fatal(format string, args ...any) {
	// Error text contains table names and operation context only; never values.
	fmt.Fprintf(os.Stderr, "legacyimport: "+format+"\n", args...)
	os.Exit(1)
}
