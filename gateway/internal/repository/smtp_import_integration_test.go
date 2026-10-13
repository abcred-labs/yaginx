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
	"yaginx-gateway/internal/management"
)

func TestImportSMTPOnlyWhenDatabaseSettingIsAbsent(t *testing.T) {
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
	schema := "yaginx_goal_smtp" + uuid.NewString()[:10]
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
	legacy := map[string]any{"enabled": true, "host": "legacy.example.invalid", "port": float64(25), "useSsl": true, "userName": "legacy", "password": "secret", "fromEmail": "legacy@example.invalid", "fromName": "Legacy"}
	created, err := store.ImportSMTPIfAbsent(ctx, legacy)
	if err != nil || !created {
		t.Fatalf("created=%t err=%v", created, err)
	}
	loaded, err := store.GetSMTP(ctx)
	if err != nil || loaded == nil || loaded.Host == nil || *loaded.Host != "legacy.example.invalid" {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	managedHost := "managed.example.invalid"
	if err := store.SetSMTP(ctx, management.SMTPSettings{Enabled: true, Host: &managedHost, Port: 25, UseSSL: true}); err != nil {
		t.Fatal(err)
	}
	created, err = store.ImportSMTPIfAbsent(ctx, legacy)
	if err != nil || created {
		t.Fatalf("second created=%t err=%v", created, err)
	}
	loaded, err = store.GetSMTP(ctx)
	if err != nil || loaded == nil || loaded.Host == nil || *loaded.Host != "managed.example.invalid" {
		t.Fatalf("database SMTP overwritten: %#v err=%v", loaded, err)
	}
}
