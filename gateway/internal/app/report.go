package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/repository"
)

func runReport(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: yaginx report backfill --from YYYY-MM-DD --to YYYY-MM-DD [--statement-timeout 5m] [--config path]\n       yaginx report reset --confirm-go-contributions [--config path]")
		return 2
	}
	if args[0] == "reset" {
		return runReportReset(ctx, args[1:], stdout, stderr)
	}
	if args[0] != "backfill" {
		fmt.Fprintln(stderr, "usage: yaginx report backfill --from YYYY-MM-DD --to YYYY-MM-DD [--statement-timeout 5m] [--config path]\n       yaginx report reset --confirm-go-contributions [--config path]")
		return 2
	}
	fs := flag.NewFlagSet("report backfill", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := configPath(fs)
	fromText := fs.String("from", "", "inclusive UTC report date (YYYY-MM-DD)")
	toText := fs.String("to", "", "inclusive UTC report date (YYYY-MM-DD)")
	statementTimeout := fs.Duration("statement-timeout", 5*time.Minute, "per-day PostgreSQL statement timeout")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 0 || *statementTimeout <= 0 {
		fmt.Fprintln(stderr, "usage: yaginx report backfill --from YYYY-MM-DD --to YYYY-MM-DD [--statement-timeout 5m] [--config path]")
		return 2
	}
	from, err := time.Parse("2006-01-02", *fromText)
	if err != nil {
		fmt.Fprintf(stderr, "invalid --from: %v\n", err)
		return 2
	}
	to, err := time.Parse("2006-01-02", *toText)
	if err != nil {
		fmt.Fprintf(stderr, "invalid --to: %v\n", err)
		return 2
	}
	if to.Before(from) {
		fmt.Fprintln(stderr, "--to must not be before --from")
		return 2
	}
	cfg, err := config.LoadRuntime(*path)
	if err != nil {
		fmt.Fprintf(stderr, "load configuration: %v\n", err)
		return 1
	}
	options, err := runtimePoolOptions(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "database options: %v\n", err)
		return 1
	}
	pool, err := database.NewRuntimePool(ctx, cfg.DatabaseDSN(), options)
	if err != nil {
		fmt.Fprintf(stderr, "database pool: %v\n", err)
		return 1
	}
	defer pool.Close()
	store := repository.New(pool)
	store.SetTrafficMaterializerLockNamespace(cfg.String("resourceReports", "leaderLockNamespace"))
	for day := from.UTC(); !day.After(to.UTC()); day = day.AddDate(0, 0, 1) {
		if err := store.BackfillDailyResourceReports(ctx, day, day.AddDate(0, 0, 1), *statementTimeout); err != nil {
			fmt.Fprintf(stderr, "backfill %s: %v\n", day.Format("2006-01-02"), err)
			return 1
		}
		fmt.Fprintf(stdout, "backfilled %s\n", day.Format("2006-01-02"))
	}
	return 0
}

func runReportReset(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("report reset", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := configPath(fs)
	confirmed := fs.Bool("confirm-go-contributions", false, "confirm the Go contribution source is the only active report writer")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || !*confirmed {
		fmt.Fprintln(stderr, "refusing reset: stop all non-Go report writers, verify at least five minutes without non-Go updates, then pass --confirm-go-contributions")
		return 2
	}
	cfg, err := config.LoadRuntime(*path)
	if err != nil {
		fmt.Fprintf(stderr, "load configuration: %v\n", err)
		return 1
	}
	options, err := runtimePoolOptions(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "database options: %v\n", err)
		return 1
	}
	pool, err := database.NewRuntimePool(ctx, cfg.DatabaseDSN(), options)
	if err != nil {
		fmt.Fprintf(stderr, "database pool: %v\n", err)
		return 1
	}
	defer pool.Close()
	store := repository.New(pool)
	store.SetTrafficMaterializerLockNamespace(cfg.String("resourceReports", "leaderLockNamespace"))
	if err := store.ResetTrafficStatistics(ctx); err != nil {
		fmt.Fprintf(stderr, "reset traffic statistics: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "cleared resource_report, host_traffic, gateway_traffic_contribution, and resource_monitor_info; request_history was preserved")
	return 0
}
