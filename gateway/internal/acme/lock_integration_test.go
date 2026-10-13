//go:build integration

package acme

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresDomainLockSerializesNormalizedDomain(t *testing.T) {
	dsn := os.Getenv("YAGINX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("YAGINX_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	firstPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer firstPool.Close()
	secondPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer secondPool.Close()

	first, acquired, err := AcquireDomainLock(ctx, firstPool, "BÜCHER.example.")
	if err != nil || !acquired {
		t.Fatalf("first lock acquired=%v err=%v", acquired, err)
	}
	defer first.Release(context.Background())
	second, acquired, err := AcquireDomainLock(ctx, secondPool, "xn--bcher-kva.example")
	if err != nil || acquired || second != nil {
		t.Fatalf("same normalized domain lock acquired=%v lock=%v err=%v", acquired, second, err)
	}
	different, acquired, err := AcquireDomainLock(ctx, secondPool, "different.example")
	if err != nil || !acquired {
		t.Fatalf("different domain lock acquired=%v err=%v", acquired, err)
	}
	different.Release(ctx)
	first.Release(ctx)
	first = nil
	retry, acquired, err := AcquireDomainLock(ctx, secondPool, "xn--bcher-kva.example")
	if err != nil || !acquired {
		t.Fatalf("released domain lock acquired=%v err=%v", acquired, err)
	}
	retry.Release(ctx)
}
