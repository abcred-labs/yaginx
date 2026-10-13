//go:build integration

package repository

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/database"
)

func TestACMEKeyValuePersistsOverwritesExpiresAndPrunes(t *testing.T) {
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
	schema := "yaginx_goal_acme_kv_" + uuid.NewString()[:10]
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
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

	if err := store.SetACMEKeyValue(ctx, "token_1", "first", time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetACMEKeyValue(ctx, "token_1", "second", time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, found, err := store.GetACMEKeyValue(ctx, "token_1"); err != nil || !found || got != "second" {
		t.Fatalf("read value=%q found=%v err=%v", got, found, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE gateway_acme_key_value SET expires_at=clock_timestamp()-interval '1 second' WHERE key='token_1'`); err != nil {
		t.Fatal(err)
	}
	if got, found, err := store.GetACMEKeyValue(ctx, "token_1"); err != nil || found || got != "" {
		t.Fatalf("expired read value=%q found=%v err=%v", got, found, err)
	}
	if err := store.PruneACMEKeyValues(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM gateway_acme_key_value`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expired rows remaining=%d", count)
	}
}
