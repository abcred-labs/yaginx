package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/platform/identity"
	"yaginx-gateway/internal/repository"
)

func runDatabase(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("database", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := configPath(fs)
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: yaginx database <init|migrate|status> [--config <path>]")
		return 2
	}
	action := args[0]
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: yaginx database <init|migrate|status> [--config <path>]")
		return 2
	}

	cfg, err := config.LoadRuntime(*path)
	if err != nil {
		fmt.Fprintf(stderr, "load configuration: %v\n", err)
		return 1
	}
	switch action {
	case "init":
		if err := database.InitializeV2(ctx, cfg.DatabaseDSN(), stdout); err != nil {
			recordDatabaseCommandError(cfg, "init", "DATABASE_INITIALIZATION_FAILED", err)
			fmt.Fprintf(stderr, "database initialization: %v\n", err)
			return 1
		}
	case "migrate":
		if err := database.MigrateV2(ctx, cfg.DatabaseDSN(), stdout); err != nil {
			recordDatabaseCommandError(cfg, "migrate", "DATABASE_MIGRATION_FAILED", err)
			fmt.Fprintf(stderr, "database migration: %v\n", err)
			return 1
		}
	case "status":
		state, err := database.DatabaseState(ctx, cfg.DatabaseDSN())
		if err != nil {
			fmt.Fprintf(stderr, "database status: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, state)
	default:
		fmt.Fprintln(stderr, "usage: yaginx database <init|migrate|status> [--config <path>]")
		return 2
	}
	return 0
}

func recordDatabaseCommandError(cfg config.RuntimeDocument, operation, code string, original error) {
	if original == nil {
		return
	}
	options, err := runtimePoolOptions(cfg)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pool, err := database.NewRuntimePool(ctx, cfg.DatabaseDSN(), options)
	if err != nil {
		return
	}
	defer pool.Close()
	store := repository.New(pool)
	if node, err := identity.Load(cfg.String("node", "identityFile")); err == nil {
		store.SetRuntimeIdentity(node.NodeID, Version, Commit)
	}
	if err := store.EnsureSystemErrorSchema(ctx); err != nil {
		return
	}
	_, _ = store.RecordSystemError(ctx, repository.SystemError{Component: "database", Operation: operation, Severity: "critical", ErrorCode: code, Message: original.Error()})
}
