// schematest creates one explicitly named, isolated test schema and runs the
// production migration against it. It never drops schemas and never prints a
// connection string, so test evidence remains inspectable without risking
// public-schema writes or secret disclosure.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"yaginx-gateway/internal/database"
)

var schemaName = regexp.MustCompile(`^yaginx_goal_[a-z0-9_]{4,48}$`)

func main() {
	schema := flag.String("schema", "", "unique yaginx_goal_<nonce> schema")
	env := flag.String("dsn-env", "YAGINX_TEST_DATABASE_URL", "DSN environment variable")
	flag.Parse()
	if !schemaName.MatchString(*schema) {
		fatal("schema name is invalid")
	}
	dsn := os.Getenv(*env)
	if dsn == "" {
		fatal("%s is not set", *env)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fatal("connect test database: %v", err)
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{*schema}.Sanitize()); err != nil {
		fatal("create isolated schema: %v", err)
	}
	isolated, err := database.WithSearchPath(dsn, *schema)
	if err != nil {
		fatal("configure isolated search path: %v", err)
	}
	if err := database.InitializeV2(ctx, isolated, os.Stdout); err != nil {
		fatal("initialize isolated schema: %v", err)
	}
	if _, err := conn.Exec(ctx, "SET search_path TO "+pgx.Identifier{*schema}.Sanitize()); err != nil {
		fatal("set isolated schema: %v", err)
	}
	var current, migrationTableSchema, versionTableSchema string
	if err := conn.QueryRow(ctx, "SELECT current_schema()").Scan(&current); err != nil || current != *schema {
		fatal("verify isolated schema")
	}
	if err := conn.QueryRow(ctx, "SELECT table_schema FROM information_schema.tables WHERE table_schema=$1 AND table_name='yaginx_schema_migration'", *schema).Scan(&migrationTableSchema); err != nil || migrationTableSchema != *schema {
		fatal("v2 migration tracker is not in isolated schema")
	}
	if err := conn.QueryRow(ctx, "SELECT table_schema FROM information_schema.tables WHERE table_schema=$1 AND table_name='yaginx_schema_version'", *schema).Scan(&versionTableSchema); err != nil || versionTableSchema != *schema {
		fatal("v2 schema marker is not in isolated schema")
	}
	fmt.Printf("isolated migration: ok schema=%s\n", *schema)
}
func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "schematest: "+format+"\n", args...)
	os.Exit(1)
}
