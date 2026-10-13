package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
		return runUpgradeAuto(args[1:], stdout, stderr)
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
	autoWorker := flags.Bool("auto-worker", false, "internal systemd timer worker")
	path := flags.String("config", runtimeConfigPath(), "runtime configuration")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 0 {
		return upgradeUsage(stderr)
	}
	if *autoWorker && strings.TrimSpace(*targetVersion) != "" {
		fmt.Fprintln(stderr, "--auto-worker cannot be combined with --version")
		return 2
	}
	if *packageOnly && (*migrateDatabase || *noRestart || *autoWorker) {
		fmt.Fprintln(stderr, "--package-only cannot be combined with --migrate-database, --no-restart, or --auto-worker")
		return 2
	}
	if *noRestart && *migrateDatabase {
		fmt.Fprintln(stderr, "--no-restart cannot be combined with --migrate-database")
		return 2
	}
	var cfg loadedUpgradeConfig
	var err error
	if *packageOnly {
		cfg.Config = upgrade.Config{ManifestURL: *manifestURL, LockFile: *lockFile, StateDirectory: *stateDirectory, DownloadTimeout: 10 * time.Minute}
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
	cfg.Config.TargetVersion = strings.TrimSpace(*targetVersion)
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
	if *autoWorker {
		if !cfg.Enabled {
			fmt.Fprintln(stdout, "automatic upgrades are disabled")
			return 0
		}
		if err := runner.InstallWithOptions(context.Background(), cfg.Config, true, upgrade.InstallOptions{MigrateDatabase: *migrateDatabase}, stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if *check {
		_, _, err = runner.Check(context.Background(), cfg.Config, stdout)
		if err != nil && recorder != nil {
			recorder("check", "UPGRADE_RELEASE_CHECK_FAILED", err)
		}
	} else {
		err = runner.InstallWithOptions(context.Background(), cfg.Config, false, upgrade.InstallOptions{MigrateDatabase: *migrateDatabase, SkipPreflightHealth: *skipPreflight, NoRestart: *noRestart, PackageOnly: *packageOnly}, stdout)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func newUpgradeSystemErrorRecorder(ctx context.Context, runtime config.RuntimeDocument) (func(string, string, error), func()) {
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
	Config        upgrade.Config
	Runtime       config.RuntimeDocument
	Enabled       bool
	CheckInterval time.Duration
}

func upgradeConfig(path string) (loadedUpgradeConfig, error) {
	runtime, err := config.LoadRuntime(path)
	if err != nil {
		return loadedUpgradeConfig{}, fmt.Errorf("load upgrade configuration %q: %w; restore the file or pass --config <valid-file> (for example a reviewed .bak), then run upgrade again", path, err)
	}
	minAge, err := runtime.Duration("upgrade", "minPublishedAge")
	if err != nil {
		return loadedUpgradeConfig{}, err
	}
	timeout, err := runtime.Duration("upgrade", "downloadTimeout")
	if err != nil {
		return loadedUpgradeConfig{}, err
	}
	readyTimeout, err := runtime.Duration("upgrade", "readinessTimeout")
	if err != nil {
		return loadedUpgradeConfig{}, err
	}
	checkInterval, err := runtime.Duration("upgrade", "checkInterval")
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
	return loadedUpgradeConfig{Config: upgrade.Config{ManifestURL: runtime.String("upgrade", "manifestURL"), LockFile: runtime.String("upgrade", "lockFile"), StateDirectory: runtime.String("upgrade", "stateDirectory"), MinPublishedAge: minAge, DownloadTimeout: timeout, ReadyTimeout: readyTimeout, ReadyURL: readyURL}, Runtime: runtime, Enabled: runtime.Bool("upgrade", "enabled"), CheckInterval: checkInterval}, nil
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

func runUpgradeAuto(args []string, stdout, stderr io.Writer) int {
	path := runtimeConfigPath()
	modes := make([]string, 0, 1)
	for index := 0; index < len(args); index++ {
		if args[index] == "--config" {
			if index+1 == len(args) {
				return upgradeUsage(stderr)
			}
			path = args[index+1]
			index++
			continue
		}
		modes = append(modes, args[index])
	}
	if len(modes) > 1 {
		return upgradeUsage(stderr)
	}
	loaded, err := upgradeConfig(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	mode := "status"
	if len(modes) == 1 {
		mode = modes[0]
	}
	switch mode {
	case "status":
		state := "disabled"
		if loaded.Enabled {
			state = "enabled"
		}
		fmt.Fprintf(stdout, "automatic upgrades: %s\nminimum release age: %s\ncheck interval: %s\n", state, loaded.Config.MinPublishedAge, loaded.CheckInterval)
		return 0
	case "on", "off":
		if os.Geteuid() != 0 {
			fmt.Fprintln(stderr, "yaginx upgrade auto requires root")
			return 1
		}
		if err := patchUpgradeEnabled(path, mode == "on"); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		action := "disable"
		if mode == "on" {
			action = "enable"
		}
		if _, err := upgradeSystemCommand(context.Background(), "systemctl", action, "--now", "yaginx-upgrade.timer"); err != nil {
			fmt.Fprintf(stderr, "systemctl %s yaginx-upgrade.timer: %v\n", action, err)
			return 1
		}
		if mode == "on" {
			fmt.Fprintln(stdout, "automatic upgrades enabled")
		} else {
			fmt.Fprintln(stdout, "automatic upgrades disabled")
		}
		return 0
	default:
		return upgradeUsage(stderr)
	}
}

func patchUpgradeEnabled(path string, enabled bool) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		return err
	}
	section, _ := document["upgrade"].(map[string]any)
	if section == nil {
		section = map[string]any{}
		document["upgrade"] = section
	}
	section["enabled"] = enabled
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	temp := path + ".new"
	if err := os.WriteFile(temp, encoded, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
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
	fmt.Fprintln(stderr, "usage: yaginx upgrade [--check] [--version <semver>] [--migrate-database] [--skip-preflight-health] [--no-restart] [--package-only] [--manifest-url <url>] [--state-directory <dir>] [--lock-file <file>] [--config <file>] | verify --candidate <file> | rollback --current <file> --previous <file> | auto [status|on|off] [--config <file>]")
	return 2
}
