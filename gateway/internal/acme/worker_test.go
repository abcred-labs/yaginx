package acme

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"yaginx-gateway/internal/repository"
)

func TestNewWorkerDefaultsRenewBeforeToSevenDays(t *testing.T) {
	worker, err := NewWorker(&repository.Postgres{}, &pgxpool.Pool{}, "node-1", Settings{
		DirectoryURL: "https://acme.example.invalid/directory",
		Email:        "operator@example.invalid",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := worker.config.RenewBefore, 7*24*time.Hour; got != want {
		t.Fatalf("renewal window = %s, want %s", got, want)
	}
}
