package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Masterminds/semver/v3"
)

const (
	// ReleaseRootURL points at immutable, versioned GitHub Release assets.
	// The moving latest manifest is fetched separately through DefaultManifestURL.
	DefaultReleaseRootURL = "https://github.com/abcred-labs/yaginx/releases/download"
	DefaultManifestURL    = "https://github.com/abcred-labs/yaginx/releases/latest/download/latest-release.json"
	// GitHubBaseURL and ReleaseAssetsBaseURL are the origins rewritten by the
	// optional download mirrors. GitHub release downloads redirect to release assets.
	GitHubBaseURL         = "https://github.com"
	ReleaseAssetsBaseURL  = "https://release-assets.githubusercontent.com"
	maxRedirects          = 5
	PackageName           = "yaginx"
	ServiceName           = "yaginx"
	VersionFile           = "/usr/share/yaginx/version.txt"
	downloadProgressEvery = 5 * time.Second
)

var ErrLockBusy = errors.New("yaginx upgrade is already in progress")

type Config struct {
	ManifestURL, LockFile, StateDirectory, ReadyURL string
	TargetVersion                                   string
	// GitHubMirror and ReleaseAssetsMirror optionally replace GitHubBaseURL and
	// ReleaseAssetsBaseURL in download and redirect URLs. Release metadata is
	// still validated against the original GitHub URLs.
	GitHubMirror, ReleaseAssetsMirror string
	DownloadTimeout, ReadyTimeout     time.Duration
}

type Release struct {
	Component      string    `json:"component"`
	Version        string    `json:"version"`
	PackageURL     string    `json:"packageUrl"`
	SHA256         string    `json:"sha256"`
	PublishedAtUTC time.Time `json:"publishedAtUtc"`
	Architecture   string    `json:"architecture"`
	GitSHA         string    `json:"gitSha"`
}

type readyResponse struct {
	Ready      bool     `json:"ready"`
	Version    string   `json:"version"`
	NodeID     string   `json:"nodeId"`
	NodeStatus string   `json:"nodeStatus"`
	Reasons    []string `json:"reasons"`
	StatusCode int      `json:"-"`
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Command func(context.Context, string, ...string) ([]byte, error)
type StreamCommand func(context.Context, io.Writer, string, ...string) error
type Locker func(string, func() error) error

type InstallOptions struct {
	MigrateDatabase     bool
	SkipPreflightHealth bool
	NoRestart           bool
	PackageOnly         bool
}

type Runner struct {
	Client         HTTPDoer
	Command        Command
	StreamCommand  StreamCommand
	Lock           Locker
	GOOS, GOARCH   string
	Geteuid        func() int
	Now            func() time.Time
	VersionFile    string
	ReleaseRootURL string
	LogFile        string
	// MigrateDatabase runs the target version's database migration after the
	// package is installed and before the service is restarted.
	MigrateDatabase func(context.Context) ([]byte, error)
	// RecordSystemError is optional and is invoked for Yaginx-owned upgrade
	// failures. It must never be used for proxy or upstream request errors.
	RecordSystemError func(operation, errorCode string, err error)
}

func New() *Runner {
	return &Runner{
		Client:  &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		Command: combinedOutput, StreamCommand: streamCommand, Lock: withLock, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Geteuid: os.Geteuid, Now: time.Now, VersionFile: VersionFile, ReleaseRootURL: DefaultReleaseRootURL, LogFile: DefaultLogFile,
	}
}

func (r *Runner) Check(ctx context.Context, cfg Config, out io.Writer) (Release, bool, error) {
	return r.check(ctx, cfg, out, nil)
}

func (r *Runner) check(ctx context.Context, cfg Config, out io.Writer, logger *operationLogger) (Release, bool, error) {
	if err := r.validatePlatform(true); err != nil {
		return Release{}, false, err
	}
	r.logMirrors(cfg, out, logger)
	logger.Event("check.current", "detecting installed version")
	current, err := r.currentVersion(ctx)
	if err != nil {
		return Release{}, false, err
	}
	target := strings.TrimSpace(cfg.TargetVersion)
	if target != "" && !validVersion(target) {
		return Release{}, false, errors.New("target version must be a strict SemVer")
	}
	logger.Event("check.manifest", "fetching release metadata target=%s", target)
	release, err := r.fetch(ctx, cfg)
	if err != nil {
		return Release{}, false, err
	}
	cmp, err := compare(current, release.Version)
	if err != nil {
		return Release{}, false, err
	}
	if cmp > 0 {
		return Release{}, false, fmt.Errorf("refusing downgrade from %s to %s", current, release.Version)
	}
	logger.Event("check.result", "current=%s target=%s available=%t", current, release.Version, cmp < 0)
	if out != nil {
		label := "latest"
		if target != "" {
			label = "target"
		}
		fmt.Fprintf(out, "current version: %s\n%s version: %s\n", current, label, release.Version)
	}
	if cmp == 0 {
		if out != nil {
			if target != "" {
				fmt.Fprintln(out, "yaginx is already at the requested version.")
			} else {
				fmt.Fprintln(out, "yaginx is already up to date.")
			}
		}
		return release, false, nil
	}
	if out != nil {
		fmt.Fprintln(out, "upgrade available.")
	}
	return release, true, nil
}

func (r *Runner) Install(ctx context.Context, cfg Config, out io.Writer) error {
	return r.InstallWithOptions(ctx, cfg, InstallOptions{}, out)
}

func (r *Runner) InstallWithOptions(ctx context.Context, cfg Config, options InstallOptions, out io.Writer) error {
	if err := r.validatePlatform(false); err != nil {
		return err
	}
	logger := newOperationLogger(out, r.logFile(), r.now)
	defer logger.Close()
	logger.Event("upgrade.start", "administrator requested upgrade")
	lock := r.Lock
	if lock == nil {
		return errors.New("upgrade lock is unavailable")
	}
	logger.Event("lock.acquire", "acquiring upgrade lock")
	err := lock(cfg.LockFile, func() error { return r.installLocked(ctx, cfg, options, out, logger) })
	if err != nil {
		if r.RecordSystemError != nil {
			r.RecordSystemError("install", upgradeErrorCode(err), err)
		}
		logger.Event("upgrade.failed", "%v", err)
	} else {
		logger.Event("upgrade.complete", "completed")
	}
	return err
}

func upgradeErrorCode(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "database migration"):
		return "UPGRADE_DATABASE_MIGRATION_FAILED"
	case strings.Contains(message, "rollback"):
		return "UPGRADE_ROLLBACK_FAILED"
	case strings.Contains(message, "readiness") || strings.Contains(message, "service did not become ready"):
		return "UPGRADE_READINESS_FAILED"
	case strings.Contains(message, "download") || strings.Contains(message, "sha-256"):
		return "UPGRADE_DOWNLOAD_FAILED"
	case strings.Contains(message, "manifest") || strings.Contains(message, "release"):
		return "UPGRADE_RELEASE_CHECK_FAILED"
	default:
		return "UPGRADE_FAILED"
	}
}

func (r *Runner) installLocked(ctx context.Context, cfg Config, options InstallOptions, out io.Writer, logger *operationLogger) error {
	mode := "online"
	if options.PackageOnly {
		mode = "package-only"
	} else if options.NoRestart {
		mode = "no-restart"
	} else if options.SkipPreflightHealth {
		mode = "skip-preflight-health"
	}
	logger.Event("upgrade.mode", "mode=%s", mode)
	if (options.PackageOnly || options.NoRestart) && options.MigrateDatabase {
		return errors.New("database migration requires a restarting upgrade")
	}
	release, available, err := r.check(ctx, cfg, out, logger)
	if err != nil || !available {
		return err
	}
	current, err := r.currentVersion(ctx)
	if err != nil {
		return err
	}
	active := false
	if !options.PackageOnly && !options.NoRestart {
		active, err = r.active(ctx)
		if err != nil {
			return err
		}
	}
	var prior readyResponse
	if active && !options.SkipPreflightHealth {
		logger.Event("readiness.before", "verifying current service")
		prior, err = r.ready(ctx, cfg.ReadyURL)
		if err != nil {
			return fmt.Errorf("verify current service readiness: %w", err)
		}
		logger.Event("readiness.before", "%s", prior.diagnostic())
	} else if active && options.SkipPreflightHealth {
		logger.Event("readiness.before.skipped", "preflight health check skipped")
	}
	logger.Event("rollback.prepare", "ensuring rollback package for version=%s", current)
	rollbackPath, err := r.ensureRollbackPackage(ctx, cfg, current, logger)
	if err != nil {
		return err
	}
	candidate, err := r.download(ctx, cfg, release.PackageURL, release.SHA256, "candidate", logger)
	if err != nil {
		return err
	}
	defer os.Remove(candidate)
	if err = r.runCommand(ctx, logger, "dpkg", "--force-confdef", "--force-confold", "-i", candidate); err != nil {
		return fmt.Errorf("install Debian package: %w", err)
	}
	if options.MigrateDatabase {
		if err := r.migrateDatabase(ctx, logger); err != nil {
			migrationErr := err
			logger.Event("rollback.install", "restoring version=%s after database migration failure", current)
			if rollbackErr := r.runCommand(ctx, logger, "dpkg", "--force-confdef", "--force-confold", "-i", rollbackPath); rollbackErr != nil {
				return fmt.Errorf("database migration failed: %v; rollback install failed: %w", migrationErr, rollbackErr)
			}
			return fmt.Errorf("database migration failed and package was rolled back to %s; database may be partially migrated: %w", current, migrationErr)
		}
	}
	if options.PackageOnly || options.NoRestart || !active {
		if options.PackageOnly {
			logger.Event("upgrade.package_only.complete", "upgraded yaginx to %s; service was not restarted", release.Version)
			logger.Event("upgrade.rollback.skipped", "automatic rollback is not applicable without restart")
		}
		if options.NoRestart {
			logger.Event("upgrade.restart.skipped", "upgraded yaginx to %s; service restart skipped", release.Version)
			logger.Event("upgrade.rollback.skipped", "automatic rollback is not applicable without restart")
		}
		if !options.PackageOnly && !options.NoRestart && !active {
			logger.Event("upgrade.installed", "upgraded yaginx to %s; service was not active, restart skipped", release.Version)
		}
		return nil
	}
	restartStarted := r.now()
	if err = r.restartAndWait(ctx, cfg, release.Version, prior.NodeID, logger); err == nil {
		logger.Event("upgrade.installed", "upgraded yaginx to %s", release.Version)
		return nil
	}
	installErr := err
	logger.Event("upgrade.rollback", "readiness failed; attempting rollback")
	r.captureServiceJournal(ctx, logger, restartStarted, r.now())
	logger.Event("rollback.install", "restoring version=%s", current)
	if rollbackErr := r.runCommand(ctx, logger, "dpkg", "--force-confdef", "--force-confold", "-i", rollbackPath); rollbackErr != nil {
		return fmt.Errorf("upgrade readiness failed: %v; rollback install failed: %w", installErr, rollbackErr)
	}
	if rollbackErr := r.restartAndWait(ctx, cfg, current, prior.NodeID, logger); rollbackErr != nil {
		return fmt.Errorf("upgrade readiness failed: %v; rollback readiness failed: %w", installErr, rollbackErr)
	}
	return fmt.Errorf("upgrade readiness failed and was rolled back to %s: %w", current, installErr)
}

func (r *Runner) migrateDatabase(ctx context.Context, logger *operationLogger) error {
	if r.MigrateDatabase == nil {
		err := errors.New("database migration is unavailable")
		logger.Event("database.migrate.failed", "%v", err)
		return err
	}
	logger.Event("database.migrate.start", "running target version database migration")
	output, err := r.MigrateDatabase(ctx)
	if len(output) > 0 {
		logger.Event("database.migrate.output", "%s", strings.TrimSpace(string(output)))
	}
	if err != nil {
		logger.Event("database.migrate.failed", "%v", err)
		return err
	}
	logger.Event("database.migrate.complete", "completed")
	return nil
}

func (r *Runner) restartAndWait(ctx context.Context, cfg Config, version, nodeID string, logger *operationLogger) error {
	if err := r.runCommand(ctx, logger, "systemctl", "restart", ServiceName); err != nil {
		return fmt.Errorf("restart %s.service: %w", ServiceName, err)
	}
	if active, err := r.active(ctx); err != nil {
		return err
	} else if !active {
		return fmt.Errorf("%s.service is not active after restart", ServiceName)
	}
	if nodeID == "" {
		logger.Event("readiness.node_id", "prior node id unknown; node id match skipped, verifying version only")
	}
	logger.Event("readiness.wait", "expected_version=%s expected_node_id=%s timeout=%s", version, nodeID, cfg.ReadyTimeout)
	deadline := r.now().Add(cfg.ReadyTimeout)
	var last error
	attempt := 0
	for {
		attempt++
		ready, err := r.ready(ctx, cfg.ReadyURL)
		if err == nil && ready.Ready && ready.Version == version && (nodeID == "" || ready.NodeID == nodeID) {
			logger.Event("readiness.poll", "attempt=%d result=ready %s", attempt, ready.diagnostic())
			return nil
		}
		if err == nil {
			err = fmt.Errorf("readiness returned %s", ready.diagnostic())
		}
		logger.Event("readiness.poll", "attempt=%d result=not_ready %s error=%v", attempt, ready.diagnostic(), err)
		last = err
		if !r.now().Before(deadline) {
			return fmt.Errorf("service did not become ready within %s: %w", cfg.ReadyTimeout, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (r *Runner) ready(ctx context.Context, readyURL string) (readyResponse, error) {
	if strings.TrimSpace(readyURL) == "" {
		return readyResponse{}, errors.New("upgrade readiness URL is empty")
	}
	response, err := r.get(ctx, Config{}, readyURL)
	if err != nil {
		return readyResponse{}, err
	}
	defer response.Body.Close()
	value := readyResponse{StatusCode: response.StatusCode}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&value); err != nil {
		return value, fmt.Errorf("decode readiness HTTP %d: %w", response.StatusCode, err)
	}
	value.StatusCode = response.StatusCode
	if response.StatusCode != http.StatusOK {
		return value, fmt.Errorf("readiness returned HTTP %d: %s", response.StatusCode, value.diagnostic())
	}
	if !value.Ready || !validVersion(value.Version) || strings.TrimSpace(value.NodeID) == "" {
		return value, fmt.Errorf("readiness response is invalid: %s", value.diagnostic())
	}
	return value, nil
}

func (r readyResponse) diagnostic() string {
	parts := []string{fmt.Sprintf("http_status=%d", r.StatusCode), fmt.Sprintf("ready=%t", r.Ready)}
	if r.Version != "" {
		parts = append(parts, fmt.Sprintf("version=%q", r.Version))
	}
	if r.NodeID != "" {
		parts = append(parts, fmt.Sprintf("node_id=%q", r.NodeID))
	}
	if r.NodeStatus != "" {
		parts = append(parts, fmt.Sprintf("node_status=%q", r.NodeStatus))
	}
	if len(r.Reasons) > 0 {
		parts = append(parts, fmt.Sprintf("reasons=%q", strings.Join(r.Reasons, ",")))
	}
	return strings.Join(parts, " ")
}

func (r *Runner) fetch(ctx context.Context, cfg Config) (Release, error) {
	manifestURL := strings.TrimSpace(cfg.ManifestURL)
	if manifestURL == "" {
		manifestURL = DefaultManifestURL
	}
	if !strings.HasPrefix(manifestURL, "https://") {
		return Release{}, errors.New("upgrade manifest URL must use HTTPS")
	}
	target := strings.TrimSpace(cfg.TargetVersion)
	if target != "" {
		if !validVersion(target) {
			return Release{}, errors.New("target version must be a strict SemVer")
		}
		parsed, err := url.Parse(manifestURL)
		if err != nil || parsed.Path == "" {
			return Release{}, errors.New("upgrade manifest URL is invalid")
		}
		// Versioned manifests are published as release-manifest-<version>.json in
		// the immutable GitHub release, not next to the moving latest manifest.
		const latestSuffix = "/releases/latest/download/latest-release.json"
		if strings.HasSuffix(parsed.Path, latestSuffix) {
			parsed.Path = strings.TrimSuffix(parsed.Path, latestSuffix) + "/releases/download/v" + target + "/release-manifest-" + target + ".json"
		} else {
			index := strings.LastIndex(parsed.Path, "/")
			parsed.Path = parsed.Path[:index+1] + "release-manifest-" + target + ".json"
		}
		manifestURL = parsed.String()
	}
	response, err := r.get(ctx, cfg, manifestURL)
	if err != nil {
		return Release{}, fmt.Errorf("fetch latest release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("fetch latest release: HTTP %d", response.StatusCode)
	}
	var value Release
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&value); err != nil {
		return Release{}, fmt.Errorf("decode latest release: %w", err)
	}
	if value.Component != PackageName || value.Architecture != "amd64" || strings.TrimSpace(value.GitSHA) == "" || value.PublishedAtUTC.IsZero() || !validVersion(value.Version) || len(value.SHA256) != 64 {
		return Release{}, errors.New("release metadata is invalid")
	}
	if target != "" && value.Version != target {
		return Release{}, fmt.Errorf("release manifest version %s does not match requested version %s", value.Version, target)
	}
	if _, err := hex.DecodeString(value.SHA256); err != nil {
		return Release{}, errors.New("latest release SHA-256 is invalid")
	}
	if value.PackageURL != r.packageURL(value.Version) || strings.Contains(strings.ToLower(value.PackageURL), "_latest") {
		return Release{}, errors.New("latest release package URL is not immutable Yaginx Debian artifact")
	}
	if value.PublishedAtUTC.After(r.now().Add(5 * time.Minute)) {
		return Release{}, errors.New("latest release published time is in the future")
	}
	return value, nil
}

func (r *Runner) ensureRollbackPackage(ctx context.Context, cfg Config, version string, logger *operationLogger) (string, error) {
	if strings.TrimSpace(cfg.StateDirectory) == "" {
		return "", errors.New("upgrade state directory is empty")
	}
	rollbackDir := filepath.Join(cfg.StateDirectory, "rollback")
	if err := os.MkdirAll(rollbackDir, 0o700); err != nil {
		return "", fmt.Errorf("create rollback directory: %w", err)
	}
	logger.Event("rollback.checksum", "fetching checksum for version=%s", version)
	checksum, err := r.checksumFor(ctx, cfg, version)
	if err != nil {
		return "", err
	}
	path := filepath.Join(rollbackDir, filepath.Base(r.packageURL(version)))
	if actual, err := checksumFile(path); err == nil && actual == checksum {
		logger.Event("rollback.cache", "using verified cached rollback package version=%s", version)
		return path, nil
	}
	temp, err := r.download(ctx, cfg, r.packageURL(version), checksum, "rollback", logger)
	if err != nil {
		return "", fmt.Errorf("download rollback package: %w", err)
	}
	defer os.Remove(temp)
	if err := os.Rename(temp, path); err != nil {
		return "", fmt.Errorf("persist rollback package: %w", err)
	}
	logger.Event("rollback.cache", "stored verified rollback package version=%s", version)
	return path, nil
}

func (r *Runner) checksumFor(ctx context.Context, cfg Config, version string) (string, error) {
	response, err := r.get(ctx, cfg, r.checksumsURL(version))
	if err != nil {
		return "", fmt.Errorf("fetch rollback checksum: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch rollback checksum: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return "", err
	}
	name := filepath.Base(r.packageURL(version))
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name && len(fields[0]) == 64 {
			if _, err := hex.DecodeString(fields[0]); err == nil {
				return strings.ToLower(fields[0]), nil
			}
		}
	}
	return "", fmt.Errorf("rollback checksum for %s is missing", name)
}

func (r *Runner) download(ctx context.Context, cfg Config, sourceURL, expectedSHA, prefix string, logger *operationLogger) (string, error) {
	if cfg.DownloadTimeout <= 0 {
		return "", errors.New("upgrade download timeout must be positive")
	}
	if err := os.MkdirAll(cfg.StateDirectory, 0o700); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.DownloadTimeout)
	defer cancel()
	logger.Event("download.start", "kind=%s timeout=%s", prefix, cfg.DownloadTimeout)
	response, err := r.get(ctx, cfg, sourceURL)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download package: HTTP %d", response.StatusCode)
	}
	file, err := os.CreateTemp(cfg.StateDirectory, "yaginx-"+prefix+"-*.deb")
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	started := r.now()
	var downloaded atomic.Int64
	done := make(chan struct{})
	if logger != nil {
		go func() {
			ticker := time.NewTicker(downloadProgressEvery)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					logger.Event("download.progress", "kind=%s bytes=%d total=%d elapsed=%s", prefix, downloaded.Load(), response.ContentLength, r.now().Sub(started).Round(time.Second))
				}
			}
		}()
	}
	countingReader := io.TeeReader(io.LimitReader(response.Body, 2<<30), writerFunc(func(data []byte) (int, error) {
		downloaded.Add(int64(len(data)))
		return len(data), nil
	}))
	_, err = io.Copy(io.MultiWriter(file, hash), countingReader)
	close(done)
	if err != nil {
		file.Close()
		os.Remove(file.Name())
		return "", err
	}
	if err = file.Close(); err != nil {
		os.Remove(file.Name())
		return "", err
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != strings.ToLower(expectedSHA) {
		os.Remove(file.Name())
		return "", errors.New("download package SHA-256 mismatch")
	}
	logger.Event("download.complete", "kind=%s bytes=%d total=%d elapsed=%s checksum=verified", prefix, downloaded.Load(), response.ContentLength, r.now().Sub(started).Round(time.Millisecond))
	return file.Name(), nil
}

// logMirrors reports the download mirrors in effect. The operation logger
// already writes to out during installs; check-only runs have no logger.
func (r *Runner) logMirrors(cfg Config, out io.Writer, logger *operationLogger) {
	if cfg.GitHubMirror == "" && cfg.ReleaseAssetsMirror == "" {
		return
	}
	if logger != nil {
		logger.Event("mirror.config", "github_mirror=%q release_assets_mirror=%q", cfg.GitHubMirror, cfg.ReleaseAssetsMirror)
	} else if out != nil {
		fmt.Fprintf(out, "using download mirrors: github_mirror=%q release_assets_mirror=%q\n", cfg.GitHubMirror, cfg.ReleaseAssetsMirror)
	}
}

// get follows at most maxRedirects HTTPS redirects itself so every hop can be
// rewritten to the configured download mirrors.
func (r *Runner) get(ctx context.Context, cfg Config, rawURL string) (*http.Response, error) {
	if err := ValidateMirror(cfg.GitHubMirror); err != nil {
		return nil, fmt.Errorf("GitHub mirror: %w", err)
	}
	if err := ValidateMirror(cfg.ReleaseAssetsMirror); err != nil {
		return nil, fmt.Errorf("release assets mirror: %w", err)
	}
	if r.Client == nil {
		return nil, errors.New("upgrade HTTP client is unavailable")
	}
	current := cfg.mirrored(rawURL)
	for redirects := 0; ; redirects++ {
		parsed, err := url.Parse(current)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, errors.New("upgrade URL is invalid")
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return nil, err
		}
		response, err := r.Client.Do(request)
		if err != nil {
			return nil, err
		}
		if response.Request != nil && response.Request.URL.String() != request.URL.String() {
			response.Body.Close()
			return nil, errors.New("upgrade HTTP client followed a redirect without validation")
		}
		if response.StatusCode < http.StatusMultipleChoices || response.StatusCode >= http.StatusBadRequest {
			return response, nil
		}
		location := response.Header.Get("Location")
		response.Body.Close()
		if redirects >= maxRedirects {
			return nil, errors.New("upgrade HTTP redirect limit exceeded")
		}
		next, err := request.URL.Parse(location)
		if location == "" || err != nil {
			return nil, errors.New("upgrade HTTP redirect location is invalid")
		}
		if next.Scheme != "https" {
			return nil, errors.New("upgrade HTTP redirect must use HTTPS")
		}
		current = cfg.mirrored(next.String())
	}
}

// ValidateMirror accepts an empty value or an HTTPS origin with an optional path.
func ValidateMirror(value string) error {
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("mirror must be an HTTPS URL without query or fragment")
	}
	return nil
}

func (c Config) mirrored(rawURL string) string {
	for _, m := range []struct{ base, mirror string }{{GitHubBaseURL, c.GitHubMirror}, {ReleaseAssetsBaseURL, c.ReleaseAssetsMirror}} {
		if m.mirror != "" && (rawURL == m.base || strings.HasPrefix(rawURL, m.base+"/")) {
			return strings.TrimRight(m.mirror, "/") + strings.TrimPrefix(rawURL, m.base)
		}
	}
	return rawURL
}

func (r *Runner) validatePlatform(check bool) error {
	if r.GOOS != "linux" || r.GOARCH != "amd64" {
		return errors.New("yaginx upgrade is supported only on linux/amd64")
	}
	if !check && (r.Geteuid == nil || r.Geteuid() != 0) {
		return errors.New("yaginx upgrade must run as root")
	}
	return nil
}
func (r *Runner) currentVersion(ctx context.Context) (string, error) {
	if output, err := r.command(ctx, "dpkg-query", "-W", "--showformat=${Version}", PackageName); err == nil && validVersion(strings.TrimSpace(string(output))) {
		return strings.TrimSpace(string(output)), nil
	}
	if b, err := os.ReadFile(r.VersionFile); err == nil && validVersion(strings.TrimSpace(string(b))) {
		return strings.TrimSpace(string(b)), nil
	}
	return "", errors.New("detect current yaginx version")
}
func (r *Runner) active(ctx context.Context) (bool, error) {
	_, err := r.command(ctx, "systemctl", "is-active", "--quiet", ServiceName)
	if err == nil {
		return true, nil
	}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) && exit.ExitCode() == 3 {
		return false, nil
	}
	return false, fmt.Errorf("check yaginx.service: %w", err)
}

func (r *Runner) runCommand(ctx context.Context, logger *operationLogger, name string, args ...string) error {
	logger.Event("command.start", "name=%s args=%q", name, args)
	output := logger.Output("command.output")
	err := r.commandStream(ctx, output, name, args...)
	_ = output.Close()
	if err != nil {
		logger.Event("command.failed", "name=%s error=%v", name, err)
		return err
	}
	logger.Event("command.complete", "name=%s", name)
	return nil
}

func (r *Runner) commandStream(ctx context.Context, output io.Writer, name string, args ...string) error {
	if r.StreamCommand != nil {
		return r.StreamCommand(ctx, output, name, args...)
	}
	result, err := r.command(ctx, name, args...)
	if len(result) > 0 && output != nil {
		_, _ = output.Write(result)
	}
	return err
}

func (r *Runner) captureServiceJournal(ctx context.Context, logger *operationLogger, since, until time.Time) {
	journalSince := journalTimestamp(since)
	journalUntil := journalTimestamp(until)
	logger.Event("journal.capture", "service=%s.service since=%s until=%s lines=100", ServiceName, journalSince, journalUntil)
	output := logger.Output("journal.yaginx")
	err := r.commandStream(ctx, output, "journalctl", "-u", ServiceName+".service", "--since", journalSince, "--until", journalUntil, "-n", "100", "--no-pager")
	_ = output.Close()
	if err != nil {
		logger.Event("journal.capture_warning", "service=%s.service error=%v", ServiceName, err)
	}
}

func journalTimestamp(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05 UTC")
}

func (r *Runner) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	if r.Command == nil {
		return nil, errors.New("command runner is unavailable")
	}
	return r.Command(ctx, name, args...)
}

func (r *Runner) logFile() string {
	return r.LogFile
}
func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}
func (r *Runner) releaseRootURL() string {
	if strings.TrimSpace(r.ReleaseRootURL) != "" {
		return r.ReleaseRootURL
	}
	return DefaultReleaseRootURL
}
func (r *Runner) packageURL(version string) string {
	root := strings.TrimRight(r.releaseRootURL(), "/")
	if strings.HasSuffix(root, "/releases/download") {
		return root + "/v" + version + "/yaginx_" + version + "_amd64.deb"
	}
	return root + "/yaginx_" + version + "_amd64.deb"
}
func (r *Runner) checksumsURL(version string) string {
	root := strings.TrimRight(r.releaseRootURL(), "/")
	if strings.HasSuffix(root, "/releases/download") {
		return root + "/v" + version + "/checksums-" + version + ".txt"
	}
	return root + "/checksums-" + version + ".txt"
}
func combinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func streamCommand(ctx context.Context, output io.Writer, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = output
	command.Stderr = output
	return command.Run()
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(data []byte) (int, error) { return f(data) }

func validVersion(value string) bool { _, err := semver.StrictNewVersion(value); return err == nil }
func compare(current, candidate string) (int, error) {
	a, err := semver.StrictNewVersion(current)
	if err != nil {
		return 0, errors.New("invalid SemVer release version")
	}
	b, err := semver.StrictNewVersion(candidate)
	if err != nil {
		return 0, errors.New("invalid SemVer release version")
	}
	return a.Compare(b), nil
}
func checksumFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func DefaultConfig() Config {
	return Config{ManifestURL: DefaultManifestURL, LockFile: "/run/yaginx/upgrade.lock", StateDirectory: "/var/lib/yaginx/upgrade", DownloadTimeout: 10 * time.Minute, ReadyTimeout: 90 * time.Second, ReadyURL: "http://127.0.0.1:9090/health/ready"}
}
func VersionPath() string { return filepath.Clean(VersionFile) }
