//go:build integration

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"yaginx-gateway/internal/database"
)

func TestSyntheticDotNetImportIsTransactionalRestartableAndRedacted(t *testing.T) {
	base := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	// Use a numeric suffix so concurrent CI runs cannot collide.
	var sourceSchema, targetSchema string
	sourceSchema = "yaginx_net_source_" + strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")
	targetSchema = "yaginx_net_target_" + strings.ReplaceAll(time.Now().Add(time.Nanosecond).Format("150405.000000000"), ".", "")
	for _, schema := range []string{sourceSchema, targetSchema} {
		if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
			t.Fatal(err)
		}
	}
	sourceDSN, err := database.WithSearchPath(base, sourceSchema)
	if err != nil {
		t.Fatal(err)
	}
	targetDSN, err := database.WithSearchPath(base, targetSchema)
	if err != nil {
		t.Fatal(err)
	}
	source, err := pgx.Connect(ctx, sourceDSN)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "net-fixture.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(ctx, string(fixture)); err != nil {
		source.Close(context.Background())
		t.Fatal(err)
	}
	source.Close(context.Background())
	if err := database.InitializeV2(ctx, targetDSN, nilWriter{}); err != nil {
		t.Fatal(err)
	}
	report, err := importDatabase(ctx, sourceDSN, targetDSN)
	if err != nil {
		t.Fatal(err)
	}
	if report.Tables["account"] != 1 || report.Tables["request_history"] != 1 {
		t.Fatalf("unexpected import counts: %#v", report.Tables)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "plain-password-must-not-be-read") || strings.Contains(string(encoded), "private-stack-trace") {
		t.Fatal("import report contains fixture secret")
	}
	check, err := pgx.Connect(ctx, targetDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close(context.Background())
	var password, body, exception *string
	if err := check.QueryRow(ctx, "SELECT password,request_body_base64,exception_message FROM account CROSS JOIN request_history").Scan(&password, &body, &exception); err != nil {
		t.Fatal(err)
	}
	if password != nil || body != nil || exception != nil {
		t.Fatalf("sensitive columns were imported: password=%v body=%v exception=%v", password, body, exception)
	}
	if _, err := importDatabase(ctx, sourceDSN, targetDSN); err != nil {
		t.Fatalf("repeat import failed: %v", err)
	}
}
