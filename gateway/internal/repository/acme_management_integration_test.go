//go:build integration

package repository

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/database"
)

func TestSearchACMEOrdersHandlesNullErrorMessage(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)

	schema := "yaginx_acme_orders_" + uuid.NewString()[:10]
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := base.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	}()

	isolation, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolation, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolation)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	store := New(pool)
	if _, err := pool.Exec(ctx, `INSERT INTO gateway_certificate_order(domains,domain_name,status,error_message)
VALUES($1,$2,'failed',NULL)`, []string{"null-error.example.invalid"}, "null-error.example.invalid"); err != nil {
		t.Fatal(err)
	}
	const message = "certificate request failed"
	if _, err := pool.Exec(ctx, `INSERT INTO gateway_certificate_order(domains,domain_name,status,error_message)
VALUES($1,$2,'failed',$3)`, []string{"message-error.example.invalid"}, "message-error.example.invalid", message); err != nil {
		t.Fatal(err)
	}

	page, err := store.SearchACMEOrders(ctx, 1, 10, "", "")
	if err != nil {
		t.Fatalf("search ACME orders: %v", err)
	}
	if page.Total != 2 || len(page.Records) != 2 {
		t.Fatalf("total=%d records=%d, want total=2 records=2", page.Total, len(page.Records))
	}
	messages := make(map[string]string, len(page.Records))
	for _, order := range page.Records {
		messages[order.Domain] = order.ErrorMessage
	}
	if got := messages["null-error.example.invalid"]; got != "" {
		t.Errorf("NULL error_message mapped to %q, want empty string", got)
	}
	if got := messages["message-error.example.invalid"]; got != message {
		t.Errorf("non-NULL error_message=%q, want %q", got, message)
	}

	filtered, err := store.SearchACMEOrders(ctx, 1, 10, "null-error.example.invalid", "failed")
	if err != nil {
		t.Fatalf("search filtered ACME orders: %v", err)
	}
	if filtered.Total != 1 || len(filtered.Records) != 1 || filtered.Records[0].ErrorMessage != "" {
		t.Fatalf("filtered page=%+v, want one NULL-message record mapped to empty string", filtered)
	}
}

func TestACMEOrderFilterPurgeAndWebsiteDelete(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	base, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close(ctx)
	schema := "yaginx_acme_orders_" + uuid.NewString()[:10]
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer base.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE")
	isolation, err := database.WithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.InitializeV2(ctx, isolation, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, isolation)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool)

	insert := func(domain, status, finishedExpr string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO gateway_certificate_order(domains,domain_name,status,finished_at) VALUES($1,$2,$3,`+finishedExpr+`)`, []string{domain}, domain, status); err != nil {
			t.Fatal(err)
		}
	}
	insert("a.cmscms.com", "failed", "NULL")
	insert("b.cmscms.com", "valid", "clock_timestamp() - interval '8 days'")
	insert("c.cmscms.com", "valid", "clock_timestamp() - interval '1 day'")
	insert("other.feinian.net", "failed", "NULL")

	page, err := store.SearchACMEOrders(ctx, 1, 50, "CMSCMS.com", "")
	if err != nil || page.Total != 3 {
		t.Fatalf("substring filter total=%d err=%v, want 3", page.Total, err)
	}

	purged, err := store.PurgeCompletedACMEOrders(ctx, ACMECompletedOrderRetention)
	if err != nil || purged != 1 {
		t.Fatalf("purged=%d err=%v, want 1", purged, err)
	}

	for _, host := range []string{"a.cmscms.com", "c.cmscms.com"} {
		if _, err := pool.Exec(ctx, `INSERT INTO website(name,specifications,hosts,data_schema_version) VALUES($1,$2,$3,2)`, "site-"+host, `{"defaultHost":"`+host+`"}`, `["`+host+`"]`); err != nil {
			t.Fatal(err)
		}
	}
	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM website WHERE name='site-a.cmscms.com'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteWebsites(ctx, []int64{id}); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM gateway_certificate_order WHERE domain_name='a.cmscms.com'`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("orders for deleted site left=%d err=%v", left, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM gateway_certificate_order WHERE domain_name IN ('c.cmscms.com','other.feinian.net')`).Scan(&left); err != nil || left != 2 {
		t.Fatalf("unrelated orders left=%d err=%v, want 2", left, err)
	}
}
