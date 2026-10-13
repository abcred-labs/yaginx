package upgrade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type exitCodeError int

func (e exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e) }
func (e exitCodeError) ExitCode() int { return int(e) }

func TestCheckReportsGitHubRelease(t *testing.T) {
	server := newServer(t, "0.1.2", time.Now().Add(-25*time.Hour), "")
	defer server.Close()
	runner := testRunner(server, "0.1.1")
	var out bytes.Buffer
	_, available, err := runner.Check(context.Background(), Config{ManifestURL: server.URL + "/latest-release.json"}, &out)
	if err != nil || !available || !strings.Contains(out.String(), "upgrade available") {
		t.Fatalf("available=%t err=%v output=%q", available, err, out.String())
	}
}

func TestCheckUsesVersionManifestForRequestedVersion(t *testing.T) {
	server := newServer(t, "1.2.3", time.Now().Add(-25*time.Hour), "")
	defer server.Close()
	runner := testRunner(server, "1.2.2")
	var out bytes.Buffer
	_, available, err := runner.Check(context.Background(), Config{ManifestURL: server.URL + "/latest-release.json", TargetVersion: "1.2.3"}, &out)
	if err != nil || !available {
		t.Fatalf("available=%t err=%v output=%q", available, err, out.String())
	}
	if !strings.Contains(out.String(), "target version: 1.2.3") {
		t.Fatalf("output=%q", out.String())
	}
}

func TestCheckRejectsRequestedManifestVersionMismatch(t *testing.T) {
	server := newServer(t, "1.2.4", time.Now().Add(-25*time.Hour), "")
	defer server.Close()
	_, _, err := testRunner(server, "1.2.2").Check(context.Background(), Config{ManifestURL: server.URL + "/latest-release.json", TargetVersion: "1.2.3"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "does not match requested version") {
		t.Fatalf("error=%v", err)
	}
}

func TestCheckRejectsInvalidRequestedVersion(t *testing.T) {
	server := newServer(t, "1.2.3", time.Now().Add(-25*time.Hour), "")
	defer server.Close()
	_, _, err := testRunner(server, "1.2.2").Check(context.Background(), Config{ManifestURL: server.URL + "/latest-release.json", TargetVersion: "v1.2.3"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "strict SemVer") {
		t.Fatalf("error=%v", err)
	}
}

func TestInstallRejectsYoungAutomaticRelease(t *testing.T) {
	server := newServer(t, "0.1.2", time.Now().Add(-time.Hour), "")
	defer server.Close()
	runner := testRunner(server, "0.1.1")
	if err := runner.Install(context.Background(), Config{ManifestURL: server.URL + "/latest-release.json", MinPublishedAge: 24 * time.Hour, DownloadTimeout: time.Minute}, true, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRejectsDowngradeAndInvalidMetadata(t *testing.T) {
	server := newServer(t, "0.1.0", time.Now().Add(-25*time.Hour), "")
	defer server.Close()
	if _, _, err := testRunner(server, "0.1.1").Check(context.Background(), Config{ManifestURL: server.URL + "/latest-release.json"}, io.Discard); err == nil {
		t.Fatal("downgrade accepted")
	}
	bad := newServer(t, "0.1.2", time.Now().Add(-25*time.Hour), "bad")
	defer bad.Close()
	if _, _, err := testRunner(bad, "0.1.1").Check(context.Background(), Config{ManifestURL: bad.URL + "/latest-release.json"}, io.Discard); err == nil {
		t.Fatal("invalid digest accepted")
	}
}

func TestInstallUsesSharedLockPolicy(t *testing.T) {
	runner := testRunner(newServer(t, "0.1.2", time.Now().Add(-25*time.Hour), ""), "0.1.1")
	runner.Lock = func(_ string, _ func() error) error { return ErrLockBusy }
	if err := runner.Install(context.Background(), DefaultConfig(), false, io.Discard); !errors.Is(err, ErrLockBusy) {
		t.Fatalf("manual install error = %v, want ErrLockBusy", err)
	}
	var out bytes.Buffer
	if err := runner.Install(context.Background(), DefaultConfig(), true, &out); err != nil {
		t.Fatalf("automatic install error = %v", err)
	}
	if !strings.Contains(out.String(), "another upgrade") {
		t.Fatalf("automatic lock output = %q", out.String())
	}
}

func TestCheckRejectsRedirectedManifest(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/latest-release.json" {
			http.Redirect(w, request, "/redirected.json", http.StatusFound)
			return
		}
		http.NotFound(w, request)
	}))
	defer server.Close()
	runner := testRunner(server, "0.1.1")
	if _, _, err := runner.Check(context.Background(), Config{ManifestURL: server.URL + "/latest-release.json"}, io.Discard); err == nil {
		t.Fatal("redirected manifest accepted")
	}
}

func TestVersionValidationRequiresStrictSemVer(t *testing.T) {
	for _, value := range []string{"0.1", "v0.1.2", "01.1.2", "0.1.2.3"} {
		if validVersion(value) {
			t.Fatalf("accepted non-strict version %q", value)
		}
	}
	if !validVersion("0.1.2") {
		t.Fatal("rejected strict SemVer")
	}
}

func TestInstallRestartsActiveServiceOnceAfterInstalling(t *testing.T) {
	const currentVersion = "0.1.1"
	const targetVersion = "0.1.2"
	installedVersion := currentVersion
	server := newInstallServer(t, currentVersion, targetVersion, &installedVersion)
	defer server.Close()

	var calls []string
	runner := &Runner{
		Client: server.Client(), GOOS: "linux", GOARCH: "amd64", Geteuid: func() int { return 0 }, Now: time.Now,
		ReleaseRootURL: server.URL, Lock: func(_ string, action func() error) error { return action() },
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			switch name {
			case "dpkg-query":
				return []byte(currentVersion), nil
			case "systemctl":
				return nil, nil
			case "dpkg":
				installedVersion = targetVersion
				return nil, nil
			default:
				return nil, fmt.Errorf("unexpected command %s", name)
			}
		},
	}
	state := t.TempDir()
	if err := runner.Install(context.Background(), Config{
		ManifestURL: server.URL + "/latest-release.json", LockFile: state + "/upgrade.lock", StateDirectory: state,
		DownloadTimeout: time.Minute, ReadyURL: server.URL + "/health/ready", ReadyTimeout: time.Second,
	}, false, io.Discard); err != nil {
		t.Fatal(err)
	}

	if got := countCommand(calls, "systemctl restart yaginx"); got != 1 {
		t.Fatalf("restart calls=%d commands=%#v", got, calls)
	}
	installIndex := indexCommand(calls, "dpkg --force-confdef --force-confold -i")
	restartIndex := indexCommand(calls, "systemctl restart yaginx")
	if installIndex < 0 || restartIndex < 0 || installIndex > restartIndex {
		t.Fatalf("restart must follow package install: %#v", calls)
	}
}

func TestInstallSkipsRestartWhenServiceIsInactive(t *testing.T) {
	const currentVersion = "0.1.1"
	const targetVersion = "0.1.2"
	installedVersion := currentVersion
	server := newInstallServer(t, currentVersion, targetVersion, &installedVersion)
	defer server.Close()

	var calls []string
	runner := &Runner{
		Client: server.Client(), GOOS: "linux", GOARCH: "amd64", Geteuid: func() int { return 0 }, Now: time.Now,
		ReleaseRootURL: server.URL, Lock: func(_ string, action func() error) error { return action() },
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			switch name {
			case "dpkg-query":
				return []byte(currentVersion), nil
			case "systemctl":
				if strings.Join(args, " ") == "is-active --quiet yaginx" {
					return nil, exitCodeError(3)
				}
				return nil, fmt.Errorf("unexpected systemctl call %q", args)
			case "dpkg":
				installedVersion = targetVersion
				return nil, nil
			default:
				return nil, fmt.Errorf("unexpected command %s", name)
			}
		},
	}
	state := t.TempDir()
	var output bytes.Buffer
	if err := runner.Install(context.Background(), Config{
		ManifestURL: server.URL + "/latest-release.json", LockFile: state + "/upgrade.lock", StateDirectory: state,
		DownloadTimeout: time.Minute, ReadyURL: server.URL + "/health/ready", ReadyTimeout: time.Second,
	}, false, &output); err != nil {
		t.Fatal(err)
	}

	if got := countCommand(calls, "systemctl restart yaginx"); got != 0 {
		t.Fatalf("inactive service restarted: %#v", calls)
	}
	if !strings.Contains(output.String(), "service was not active, restart skipped") {
		t.Fatalf("output=%q", output.String())
	}
}

func TestInstallNoRestartSkipsSystemdAndReadiness(t *testing.T) {
	server := newInstallServer(t, "0.1.1", "0.1.2", new(string))
	defer server.Close()
	runner := testRunner(server, "0.1.1")
	runner.ReleaseRootURL = server.URL
	state := t.TempDir()
	runner.Lock = func(_ string, action func() error) error { return action() }
	runner.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "dpkg" && len(args) > 0 && args[0] == "--force-confdef" {
			return nil, nil
		}
		if name == "dpkg-query" {
			return []byte("0.1.1"), nil
		}
		return nil, fmt.Errorf("unexpected command %s %v", name, args)
	}
	if err := runner.InstallWithOptions(context.Background(), Config{ManifestURL: server.URL + "/latest-release.json", LockFile: filepath.Join(state, "lock"), StateDirectory: state, DownloadTimeout: time.Minute}, false, InstallOptions{NoRestart: true}, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestInstallPackageOnlySkipsSystemdAndReadiness(t *testing.T) {
	server := newInstallServer(t, "0.1.1", "0.1.2", new(string))
	defer server.Close()
	runner := testRunner(server, "0.1.1")
	runner.ReleaseRootURL = server.URL
	state := t.TempDir()
	runner.Lock = func(_ string, action func() error) error { return action() }
	runner.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "dpkg" && len(args) > 0 && args[0] == "--force-confdef" {
			return nil, nil
		}
		if name == "dpkg-query" {
			return []byte("0.1.1"), nil
		}
		return nil, fmt.Errorf("unexpected command %s %v", name, args)
	}
	if err := runner.InstallWithOptions(context.Background(), Config{ManifestURL: server.URL + "/latest-release.json", LockFile: filepath.Join(state, "lock"), StateDirectory: state, DownloadTimeout: time.Minute}, false, InstallOptions{PackageOnly: true}, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestInstallMigratesDatabaseBeforeRestart(t *testing.T) {
	const currentVersion = "0.1.1"
	const targetVersion = "0.1.2"
	installedVersion := currentVersion
	server := newInstallServer(t, currentVersion, targetVersion, &installedVersion)
	defer server.Close()

	var calls []string
	runner := &Runner{
		Client: server.Client(), GOOS: "linux", GOARCH: "amd64", Geteuid: func() int { return 0 }, Now: time.Now,
		ReleaseRootURL: server.URL, Lock: func(_ string, action func() error) error { return action() },
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			switch name {
			case "dpkg-query":
				return []byte(currentVersion), nil
			case "systemctl":
				return nil, nil
			case "dpkg":
				installedVersion = targetVersion
				return nil, nil
			default:
				return nil, fmt.Errorf("unexpected command %s", name)
			}
		},
		MigrateDatabase: func(context.Context) ([]byte, error) {
			calls = append(calls, "database migrate")
			return []byte("migration complete"), nil
		},
	}
	state := t.TempDir()
	err := runner.InstallWithOptions(context.Background(), Config{
		ManifestURL: server.URL + "/latest-release.json", LockFile: state + "/upgrade.lock", StateDirectory: state,
		DownloadTimeout: time.Minute, ReadyURL: server.URL + "/health/ready", ReadyTimeout: time.Second,
	}, false, InstallOptions{MigrateDatabase: true}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	migrateIndex := indexCommand(calls, "database migrate")
	restartIndex := indexCommand(calls, "systemctl restart yaginx")
	if migrateIndex < 0 || restartIndex < 0 || migrateIndex > restartIndex {
		t.Fatalf("migration must precede restart: %#v", calls)
	}
}

func TestInstallRollsBackPackageWithoutRestartWhenDatabaseMigrationFails(t *testing.T) {
	const currentVersion = "0.1.1"
	const targetVersion = "0.1.2"
	installedVersion := currentVersion
	server := newInstallServer(t, currentVersion, targetVersion, &installedVersion)
	defer server.Close()

	dpkgCalls := 0
	var calls []string
	runner := &Runner{
		Client: server.Client(), GOOS: "linux", GOARCH: "amd64", Geteuid: func() int { return 0 }, Now: time.Now,
		ReleaseRootURL: server.URL, Lock: func(_ string, action func() error) error { return action() },
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			switch name {
			case "dpkg-query":
				return []byte(currentVersion), nil
			case "systemctl":
				return nil, nil
			case "dpkg":
				dpkgCalls++
				if strings.Contains(args[len(args)-1], "rollback") {
					installedVersion = currentVersion
				}
				return nil, nil
			default:
				return nil, fmt.Errorf("unexpected command %s", name)
			}
		},
		MigrateDatabase: func(context.Context) ([]byte, error) { return nil, errors.New("missing migration privilege") },
	}
	state := t.TempDir()
	err := runner.InstallWithOptions(context.Background(), Config{
		ManifestURL: server.URL + "/latest-release.json", LockFile: state + "/upgrade.lock", StateDirectory: state,
		DownloadTimeout: time.Minute, ReadyURL: server.URL + "/health/ready", ReadyTimeout: time.Second,
	}, false, InstallOptions{MigrateDatabase: true}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "database may be partially migrated") {
		t.Fatalf("error = %v, want migration warning", err)
	}
	if dpkgCalls != 2 {
		t.Fatalf("dpkg calls = %d, want candidate and rollback", dpkgCalls)
	}
	if indexCommand(calls, "systemctl restart yaginx") >= 0 {
		t.Fatalf("candidate was restarted after migration failure: %#v", calls)
	}
}

func TestInstallRollsBackWhenTargetNeverBecomesReady(t *testing.T) {
	currentVersion := "0.1.1"
	installedVersion := currentVersion
	oldPackage, newPackage := []byte("old-deb"), []byte("new-deb")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		root := "https://" + request.Host
		switch request.URL.Path {
		case "/latest-release.json":
			digest := sha256.Sum256(newPackage)
			fmt.Fprintf(w, `{"component":"yaginx","version":"0.1.2","packageUrl":"%s/yaginx_0.1.2_amd64.deb","sha256":"%x","publishedAtUtc":"%s","architecture":"amd64","gitSha":"test"}`, root, digest, time.Now().Add(-25*time.Hour).UTC().Format(time.RFC3339))
		case "/checksums-0.1.1.txt":
			digest := sha256.Sum256(oldPackage)
			fmt.Fprintf(w, "%x  yaginx_0.1.1_amd64.deb\n", digest)
		case "/yaginx_0.1.1_amd64.deb":
			_, _ = w.Write(oldPackage)
		case "/yaginx_0.1.2_amd64.deb":
			_, _ = w.Write(newPackage)
		case "/health/ready":
			if installedVersion == "0.1.0" {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprint(w, `{"ready":false,"version":"0.1.0","nodeId":"node-1","nodeStatus":"approved","reasons":["snapshot_unavailable","listeners_unavailable"]}`)
				return
			}
			_, _ = fmt.Fprintf(w, `{"ready":true,"version":"%s","nodeId":"node-1"}`, installedVersion)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	dpkgCalls := 0
	var calls []string
	runner := &Runner{
		Client: server.Client(), GOOS: "linux", GOARCH: "amd64", Geteuid: func() int { return 0 }, Now: time.Now,
		ReleaseRootURL: server.URL, Lock: func(_ string, action func() error) error { return action() },
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			switch name {
			case "dpkg-query":
				return []byte(currentVersion), nil
			case "systemctl":
				return nil, nil
			case "dpkg":
				dpkgCalls++
				if strings.Contains(args[len(args)-1], "rollback") {
					installedVersion = currentVersion
				} else {
					installedVersion = "0.1.0"
				}
				return nil, nil
			case "journalctl":
				return []byte("candidate startup diagnostic\n"), nil
			default:
				return nil, fmt.Errorf("unexpected command %s", name)
			}
		},
	}
	state := t.TempDir()
	runner.LogFile = filepath.Join(state, "upgrade.log")
	err := runner.Install(context.Background(), Config{ManifestURL: server.URL + "/latest-release.json", LockFile: state + "/upgrade.lock", StateDirectory: state, DownloadTimeout: time.Minute, ReadyURL: server.URL + "/health/ready", ReadyTimeout: time.Nanosecond}, false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "snapshot_unavailable") || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("install error = %v, want rollback result", err)
	}
	if dpkgCalls != 2 {
		t.Fatalf("dpkg calls = %d, want target and rollback", dpkgCalls)
	}
	if got := countCommand(calls, "systemctl restart yaginx"); got != 2 {
		t.Fatalf("restart calls=%d commands=%#v", got, calls)
	}
	if _, err := os.Stat(filepath.Join(state, "rollback", "yaginx_0.1.1_amd64.deb")); err != nil {
		t.Fatalf("rollback package was not persisted: %v", err)
	}
	if indexCommand(calls, "journalctl -u yaginx.service") < 0 {
		t.Fatalf("missing candidate journal capture: %#v", calls)
	}
	logs, err := os.ReadFile(runner.LogFile)
	if err != nil || !strings.Contains(string(logs), "candidate startup diagnostic") || !strings.Contains(string(logs), "snapshot_unavailable") {
		t.Fatalf("logs=%q err=%v", logs, err)
	}
}

func TestReadyIncludesNon200Diagnostics(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, `{"ready":false,"version":"0.1.432","nodeId":"node-1","nodeStatus":"approved","reasons":["snapshot_unavailable"]}`)
	}))
	defer server.Close()
	runner := &Runner{Client: server.Client()}
	ready, err := runner.ready(context.Background(), server.URL)
	if err == nil || ready.StatusCode != http.StatusServiceUnavailable || !strings.Contains(err.Error(), "snapshot_unavailable") || !strings.Contains(ready.diagnostic(), "node_status=\"approved\"") {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
}

func TestJournalTimestampUsesJournalctlFormat(t *testing.T) {
	value := time.Date(2026, 9, 14, 12, 21, 3, 0, time.FixedZone("CST", 8*60*60))
	if got, want := journalTimestamp(value), "2026-09-14 04:21:03 UTC"; got != want {
		t.Fatalf("journal timestamp=%q, want %q", got, want)
	}
}

func newInstallServer(t *testing.T, currentVersion, targetVersion string, installedVersion *string) *httptest.Server {
	t.Helper()
	oldPackage := []byte("old-deb")
	newPackage := []byte("new-deb")
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		root := "https://" + request.Host
		switch request.URL.Path {
		case "/latest-release.json":
			digest := sha256.Sum256(newPackage)
			fmt.Fprintf(w, `{"component":"yaginx","version":"%s","packageUrl":"%s/yaginx_%s_amd64.deb","sha256":"%x","publishedAtUtc":"%s","architecture":"amd64","gitSha":"test"}`, targetVersion, root, targetVersion, digest, time.Now().Add(-25*time.Hour).UTC().Format(time.RFC3339))
		case "/checksums-" + currentVersion + ".txt":
			digest := sha256.Sum256(oldPackage)
			fmt.Fprintf(w, "%x  yaginx_%s_amd64.deb\n", digest, currentVersion)
		case "/yaginx_" + currentVersion + "_amd64.deb":
			_, _ = w.Write(oldPackage)
		case "/yaginx_" + targetVersion + "_amd64.deb":
			_, _ = w.Write(newPackage)
		case "/health/ready":
			_, _ = fmt.Fprintf(w, `{"ready":true,"version":"%s","nodeId":"node-1"}`, *installedVersion)
		default:
			http.NotFound(w, request)
		}
	}))
	return server
}

func countCommand(calls []string, want string) int {
	count := 0
	for _, call := range calls {
		if call == want {
			count++
		}
	}
	return count
}

func indexCommand(calls []string, prefix string) int {
	for index, call := range calls {
		if strings.HasPrefix(call, prefix) {
			return index
		}
	}
	return -1
}

func newServer(t *testing.T, version string, published time.Time, checksum string) *httptest.Server {
	t.Helper()
	data := []byte("deb")
	digest := sha256.Sum256(data)
	if checksum != "" {
		digest = [32]byte{}
	}
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/release.deb", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) })
	manifest := func(w http.ResponseWriter, _ *http.Request) {
		value := hex.EncodeToString(digest[:])
		if checksum != "" {
			value = checksum
		}
		fmt.Fprintf(w, `{"component":"yaginx","version":"%s","packageUrl":"https://github.com/abcred-labs/yaginx/releases/latest/download/yaginx_%s_amd64.deb","sha256":"%s","publishedAtUtc":"%s","architecture":"amd64","gitSha":"test"}`, version, version, value, published.UTC().Format(time.RFC3339))
	}
	mux.HandleFunc("/latest-release.json", manifest)
	mux.HandleFunc("/manifest-1.2.3.json", manifest)
	server = httptest.NewTLSServer(mux)
	return server
}
func testRunner(server *httptest.Server, current string) *Runner {
	return &Runner{Client: server.Client(), GOOS: "linux", GOARCH: "amd64", Geteuid: func() int { return 0 }, Now: time.Now, ReleaseRootURL: "https://github.com/abcred-labs/yaginx/releases/latest/download", Lock: func(_ string, action func() error) error { return action() }, Command: func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "dpkg-query" {
			return []byte(current), nil
		}
		return nil, fmt.Errorf("unexpected %s", name)
	}}
}
