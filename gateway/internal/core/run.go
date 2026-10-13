package core

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"yaginx-gateway/internal/listeners"
)

// Run starts the standalone rescue data plane from one local immutable input.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("yaginx-core", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("snapshot", DefaultSnapshotPath, "core runtime snapshot")
	validateOnly := flags.Bool("validate", false, "validate the core runtime snapshot without binding listeners")
	version := flags.Bool("version", false, "print build metadata")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 0 {
		return 2
	}
	if *version {
		fmt.Fprintln(stdout, VersionString())
		return 0
	}
	value, err := Load(*path)
	if err != nil {
		fmt.Fprintf(stderr, "load core runtime: %v\n", err)
		return 1
	}
	_, config, err := value.Build()
	if err != nil {
		fmt.Fprintf(stderr, "validate core runtime: %v\n", err)
		return 1
	}
	if *validateOnly {
		fmt.Fprintln(stdout, "core runtime snapshot: ok")
		return 0
	}
	config.Logger = slog.New(slog.NewJSONHandler(stderr, nil))
	lifecycle, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	running, err := listeners.Start(*config)
	if err != nil {
		fmt.Fprintf(stderr, "start core listeners: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "core gateway listeners started")
	<-lifecycle.Done()
	shutdown, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	_ = running.Close(shutdown)
	return 0
}
