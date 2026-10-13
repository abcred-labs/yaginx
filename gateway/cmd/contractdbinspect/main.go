// contractdbinspect collects read-only R0 database compatibility evidence.
// It never accepts a DSN flag so a secret cannot be accidentally persisted in
// shell history, CI configuration, or a fixture file.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

var legacyTables = []string{
	"account",
	"account_password_recovery",
	"host_traffic",
	"request_history",
	"resource_monitor_info",
	"resource_report",
	"web_domain",
	"website",
}

type report struct {
	PostgreSQLMajor int                   `json:"postgresqlMajor"`
	Role            string                `json:"role"`
	Tables          map[string]tableState `json:"tables"`
}

type tableState struct {
	Exists      bool   `json:"exists"`
	ColumnCount int    `json:"columnCount"`
	RowCount    *int64 `json:"rowCount,omitempty"`
	CanSelect   bool   `json:"canSelect"`
	CanInsert   bool   `json:"canInsert"`
	CanUpdate   bool   `json:"canUpdate"`
	CanDelete   bool   `json:"canDelete"`
}

func main() {
	envName := flag.String("dsn-env", "YAGINX_CONTRACT_DATABASE_URL", "environment variable that holds the PostgreSQL DSN")
	timeout := flag.Duration("timeout", 10*time.Second, "total read-only inspection timeout")
	rowCounts := flag.Bool("row-counts", false, "include row counts for existing legacy tables")
	flag.Parse()
	dsn := os.Getenv(*envName)
	if dsn == "" {
		fatal("%s is not set", *envName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := inspect(ctx, dsn, *rowCounts)
	if err != nil {
		fatal("inspect database: %v", err)
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatal("encode report: %v", err)
	}
	fmt.Println(string(encoded))
}

func inspect(ctx context.Context, dsn string, includeRowCounts bool) (report, error) {
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return report{}, err
	}
	defer connection.Close(context.Background())
	transaction, err := connection.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return report{}, err
	}
	defer transaction.Rollback(context.Background())

	var versionNum int
	var role string
	if err := transaction.QueryRow(ctx, "SELECT current_setting('server_version_num')::integer, current_user").Scan(&versionNum, &role); err != nil {
		return report{}, err
	}
	result := report{PostgreSQLMajor: versionNum / 10000, Role: role, Tables: make(map[string]tableState, len(legacyTables))}
	tables := append([]string(nil), legacyTables...)
	sort.Strings(tables)
	for _, table := range tables {
		state, err := inspectTable(ctx, transaction, table, includeRowCounts)
		if err != nil {
			return report{}, fmt.Errorf("inspect table %q: %w", table, err)
		}
		result.Tables[table] = state
	}
	return result, nil
}

func inspectTable(ctx context.Context, transaction pgx.Tx, table string, includeRowCount bool) (tableState, error) {
	var state tableState
	var relation *string
	if err := transaction.QueryRow(ctx, `
SELECT to_regclass(current_schema() || '.' || $1)::text`, table).Scan(&relation); err != nil {
		return tableState{}, err
	}
	if relation == nil {
		return state, nil
	}
	state.Exists = true
	if err := transaction.QueryRow(ctx, `
SELECT
    has_table_privilege(current_user, $1, 'SELECT'),
    has_table_privilege(current_user, $1, 'INSERT'),
    has_table_privilege(current_user, $1, 'UPDATE'),
    has_table_privilege(current_user, $1, 'DELETE')`, *relation).Scan(
		&state.CanSelect, &state.CanInsert, &state.CanUpdate, &state.CanDelete,
	); err != nil {
		return tableState{}, err
	}
	if err := transaction.QueryRow(ctx, `
SELECT count(*)
FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = $1`, table).Scan(&state.ColumnCount); err != nil {
		return tableState{}, err
	}
	if includeRowCount && state.CanSelect {
		var count int64
		if err := transaction.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil {
			return tableState{}, err
		}
		state.RowCount = &count
	}
	return state, nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "contractdbinspect: "+format+"\n", args...)
	os.Exit(1)
}
