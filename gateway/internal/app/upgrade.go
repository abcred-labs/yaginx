package app

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/platform/identity"
	"yaginx-gateway/internal/repository"
	"yaginx-gateway/internal/upgrade"
)

func runUpgrade(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "verify" {
		return runUpgradeVerify(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "check" {
		return runUpgradeVerify(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "rollback" {
		return runUpgradeRollback(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "auto" {
		fmt.Fprintln(stderr, "yaginx upgrade auto has been removed: automatic upgrades no longer exist; run `yaginx upgrade` manually and remove any yaginx-upgrade.timer/service")
		return upgradeUsage(stderr)
	}
	for _, arg := range args {
		if arg == "--auto-worker" || strings.HasPrefix(arg, "--auto-worker=") {
			fmt.Fprintln(stderr, "--auto-worker has been removed: automatic upgrades no longer exist; disable yaginx-upgrade.timer and run `yaginx upgrade` manually")
			return upgradeUsage(stderr)
		}
	}
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	flags.SetOutput(stderr)
	check := flags.Bool("check", false, "check the latest release without installing")
	targetVersion := flags.String("version", "", "target release version (strict SemVer)")
	migrateDatabase := flags.Bool("migrate-database", false, "run the target version database migration before restart")
	skipPreflight := flags.Bool("skip-preflight-health", false, "skip readiness check of the current service before upgrade")
	noRestart := flags.Bool("no-restart", false, "install the package without restarting the service")
	packageOnly := flags.Bool("package-only", false, "install the package without reading configuration or touching the service")
	manifestURL := flags.String("manifest-url", "", "release manifest URL (package-only mode)")
	stateDirectory := flags.String("state-directory", "", "upgrade state directory (package-only mode)")
	lockFile := flags.String("lock-file", "", "upgrade lock file (package-only mode)")
	githubMirror := flags.String("github-mirror", "", "GitHub mirror base URL (overrides upgrade.githubMirror and YAGINX_GITHUB_MIRROR)")
	releaseAssetsMirror := flags.String("release-assets-mirror", "", "release assets mirror base URL (overrides upgrade.releaseAssetsMirror and YAGINX_RELEASE_ASSETS_MIRROR)")
	path := flags.String("config", runtimeConfigPath(), "runtime configuration")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 0 {
		return upgradeUsage(stderr)
	}
	if *packageOnly && (*migrateDatabase || *noRestart) {
		fmt.Fprintln(stderr, "--package-only cannot be combined with --migrate-database or --no-restart")
		return 2
	}
	if *noRestart && *migrateDatabase {
		fmt.Fprintln(stderr, "--no-restart cannot be combined with --migrate-database")
		return 2
	}
	var cfg loadedUpgradeConfig
	var err error
	if *packageOnly {
		cfg.Config = upgrade.Config{ManifestURL: *manifestURL, LockFile: *lockFile, StateDirectory: *stateDirectory, DownloadTimeout: 10 * time.Minute, GitHubMirror: os.Getenv("YAGINX_GITHUB_MIRROR"), ReleaseAssetsMirror: os.Getenv("YAGINX_RELEASE_ASSETS_MIRROR")}
		if cfg.Config.ManifestURL == "" {
			cfg.Config.ManifestURL = upgrade.DefaultManifestURL
		}
		if cfg.Config.LockFile == "" {
			cfg.Config.LockFile = "/run/yaginx/upgrade.lock"
		}
		if cfg.Config.StateDirectory == "" {
			cfg.Config.StateDirectory = "/var/lib/yaginx/upgrade"
		}
	} else {
		cfg, err = upgradeConfig(*path)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	for _, warning := range cfg.Runtime.Warnings {
		fmt.Fprintf(stderr, "configuration warning: %s\n", warning)
	}
	cfg.Config.TargetVersion = strings.TrimSpace(*targetVersion)
	if value := strings.TrimSpace(*githubMirror); value != "" {
		cfg.Config.GitHubMirror = value
	}
	if value := strings.TrimSpace(*releaseAssetsMirror); value != "" {
		cfg.Config.ReleaseAssetsMirror = value
	}
	runner := upgrade.New()
	if *migrateDatabase {
		executable, executableErr := os.Executable()
		if executableErr != nil {
			fmt.Fprintln(stderr, "locate yaginx executable:", executableErr)
			return 1
		}
		runner.MigrateDatabase = func(ctx context.Context) ([]byte, error) {
			return upgradeSystemCommand(ctx, executable, "database", "migrate", "--config", *path)
		}
	}
	recorder, closeRecorder := newUpgradeSystemErrorRecorder(context.Background(), cfg.Runtime)
	defer closeRecorder()
	runner.RecordSystemError = recorder
	if *check {
		_, _, err = runner.Check(context.Background(), cfg.Config, stdout)
		if err != nil && recorder != nil {
			recorder("check", "UPGRADE_RELEASE_CHECK_FAILED", err)
		}
	} else {
		err = runner.InstallWithOptions(context.Background(), cfg.Config, upgrade.InstallOptions{MigrateDatabase: *migrateDatabase, SkipPreflightHealth: *skipPreflight, NoRestart: *noRestart, PackageOnly: *packageOnly}, stdout)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func newUpgradeSystemErrorRecorder(ctx context.Context, runtime config.RuntimeDocument) (func(string, string, error), func()) {
	// Package-only repair deliberately has no runtime configuration and must
	// not read database settings or connect to PostgreSQL.
	if runtime.Values == nil {
		return nil, func() {}
	}
	// Upgrade must remain usable when PostgreSQL is unavailable. In that case
	// recording is best-effort and never changes the upgrade result.
	options, err := runtimePoolOptions(runtime)
	if err != nil {
		return nil, func() {}
	}
	connectCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	pool, err := database.NewRuntimePool(connectCtx, runtime.DatabaseDSN(), options)
	cancel()
	if err != nil {
		return nil, func() {}
	}
	store := repository.New(pool)
	if node, e := identity.Load(runtime.String("node", "identityFile")); e == nil {
		store.SetRuntimeIdentity(node.NodeID, Version, Commit)
	}
	if e := store.EnsureSystemErrorSchema(context.Background()); e != nil {
		pool.Close()
		return nil, func() {}
	}
	record := func(operation, code string, original error) {
		if original == nil {
			return
		}
		recordCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_, _ = store.RecordSystemError(recordCtx, repository.SystemError{Component: "upgrade", Operation: operation, Severity: "error", ErrorCode: code, Message: original.Error()})
	}
	return record, pool.Close
}

type loadedUpgradeConfig struct {
	Config  upgrade.Config
	Runtime config.RuntimeDocument
}

func upgradeConfig(path string) (loadedUpgradeConfig, error) {
	runtime, err := config.LoadRuntime(path)
	if err != nil {
		return loadedUpgradeConfig{}, fmt.Errorf("load upgrade configuration %q: %w; restore the file or pass --config <valid-file> (for example a reviewed .bak), then run upgrade again", path, err)
	}
	timeout, err := runtime.Duration("upgrade", "downloadTimeout")
	if err != nil {
		return loadedUpgradeConfig{}, err
	}
	readyTimeout, err := runtime.Duration("upgrade", "readinessTimeout")
	if err != nil {
		return loadedUpgradeConfig{}, err
	}
	management, err := runtime.ManagementAddress()
	if err != nil {
		return loadedUpgradeConfig{}, err
	}
	readyURL, err := localReadyURL(management, runtime.String("observability", "readyPath"))
	if err != nil {
		return loadedUpgradeConfig{}, err
	}
	return loadedUpgradeConfig{Config: upgrade.Config{ManifestURL: runtime.String("upgrade", "manifestURL"), LockFile: runtime.String("upgrade", "lockFile"), StateDirectory: runtime.String("upgrade", "stateDirectory"), DownloadTimeout: timeout, ReadyTimeout: readyTimeout, ReadyURL: readyURL, GitHubMirror: runtime.String("upgrade", "githubMirror"), ReleaseAssetsMirror: runtime.String("upgrade", "releaseAssetsMirror")}, Runtime: runtime}, nil
}

func localReadyURL(address, path string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("split management address: %w", err)
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::", "[::]":
		host = "::1"
	}
	if path == "" || path[0] != '/' {
		return "", fmt.Errorf("observability.readyPath must be absolute")
	}
	return "http://" + net.JoinHostPort(host, port) + path, nil
}

func runUpgradeVerify(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("upgrade verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	candidate := flags.String("candidate", "", "candidate file")
	if flags.Parse(args) != nil || *candidate == "" {
		return upgradeUsage(stderr)
	}
	data, err := os.ReadFile(*candidate)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	info, err := os.Stat(*candidate)
	if err != nil || info.IsDir() {
		fmt.Fprintln(stderr, "candidate is not a regular file")
		return 1
	}
	fmt.Fprintf(stdout, "candidate: ok size=%d sha256=%x\n", info.Size(), sha256.Sum256(data))
	return 0
}
func runUpgradeRollback(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("upgrade rollback", flag.ContinueOnError)
	flags.SetOutput(stderr)
	current := flags.String("current", "", "current binary")
	previous := flags.String("previous", "", "previous binary")
	if flags.Parse(args) != nil || *current == "" || *previous == "" {
		return upgradeUsage(stderr)
	}
	return rollback(*current, *previous, stdout, stderr)
}
func rollback(current, previous string, stdout, stderr io.Writer) int {
	current, _ = filepath.Abs(current)
	previous, _ = filepath.Abs(previous)
	if current == previous {
		fmt.Fprintln(stderr, "current and previous must differ")
		return 1
	}
	if info, err := os.Stat(previous); err != nil || info.IsDir() {
		fmt.Fprintln(stderr, "previous binary is unavailable")
		return 1
	}
	backup := current + ".before-rollback"
	if _, err := os.Stat(backup); err == nil {
		fmt.Fprintln(stderr, "rollback backup already exists")
		return 1
	}
	if err := os.Rename(current, backup); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := os.Rename(previous, current); err != nil {
		_ = os.Rename(backup, current)
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "rollback: activated %s; prior binary saved as %s\n", current, backup)
	return 0
}
func runtimeConfigPath() string {
	if path := strings.TrimSpace(os.Getenv("YAGINX_CONFIG")); path != "" {
		return path
	}
	return config.DefaultRuntimePath
}
func upgradeSystemCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return upgrade.New().Command(ctx, name, args...)
}
func upgradeUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, "usage: yaginx upgrade [--check] [--version <semver>] [--migrate-database] [--skip-preflight-health] [--no-restart] [--package-only] [--manifest-url <url>] [--github-mirror <url>] [--release-assets-mirror <url>] [--state-directory <dir>] [--lock-file <file>] [--config <file>] | verify --candidate <file> | rollback --current <file> --previous <file>")
	return 2
}
