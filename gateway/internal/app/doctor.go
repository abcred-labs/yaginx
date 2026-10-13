package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/database"
)

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := configPath(fs)
	checkDatabase := fs.Bool("check-db", false, "test the configured PostgreSQL connection")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.LoadRuntime(*path)
	if err != nil {
		fmt.Fprintf(stderr, "configuration: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "configuration: ok (%s)\n", *path)
	ports := []int{cfg.ListenerPort("http"), cfg.ListenerPort("proxyHttp"), cfg.ListenerPort("https"), cfg.ListenerPort("proxyHttps"), cfg.ListenerPort("management")}
	fmt.Fprintln(stdout, "database configuration: loaded")
	fmt.Fprintf(stdout, "listeners: %d, %d, %d, %d, %d\n", ports[0], ports[1], ports[2], ports[3], ports[4])

	if !*checkDatabase {
		fmt.Fprintln(stdout, "database connectivity: skipped")
		return 0
	}

	checkContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := database.Check(checkContext, cfg.DatabaseDSN(), cfg.Int("database", "minimumServerMajor")); err != nil {
		fmt.Fprintf(stderr, "database connectivity: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "database connectivity: ok")
	return 0
}
