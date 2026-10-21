package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yaginx-gateway/internal/config"
)

func TestUpgradeCheckPrintsSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate")
	if err := os.WriteFile(path, []byte("candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runUpgrade([]string{"check", "--candidate", path}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "sha256=") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestRollbackPreservesCurrentBinary(t *testing.T) {
	dir := t.TempDir()
	current, previous := filepath.Join(dir, "yaginx"), filepath.Join(dir, "previous")
	if err := os.WriteFile(current, []byte("current"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(previous, []byte("previous"), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runUpgrade([]string{"rollback", "--current", current, "--previous", previous}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	active, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(current + ".before-rollback")
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "previous" || string(backup) != "current" {
		t.Fatalf("active=%q backup=%q", active, backup)
	}
}

func TestUpgradeUsageIncludesDatabaseMigrationFlag(t *testing.T) {
	var stderr bytes.Buffer
	if code := upgradeUsage(&stderr); code != 2 {
		t.Fatalf("usage code=%d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--migrate-database") {
		t.Fatalf("usage=%q", stderr.String())
	}
}

func TestRetiredAutomaticUpgradeEntrypointsHaveNoSideEffects(t *testing.T) {
	for _, args := range [][]string{
		{"auto"}, {"auto", "status"}, {"auto", "on"}, {"auto", "off"},
		{"--auto-worker"}, {"--auto-worker=true"},
		{"--auto-worker", "--version", "1.2.3"},
		{"--check", "--auto-worker"}, {"--package-only", "--auto-worker"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			original := []byte(`{"configVersion":1,"upgrade":{"enabled":true}}`)
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("YAGINX_CONFIG", path)
			var stdout, stderr bytes.Buffer
			if code := runUpgrade(append(args, "--config", path), &stdout, &stderr); code != 2 {
				t.Fatalf("code=%d stderr=%q", code, stderr.String())
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), "usage:") {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, original) {
				t.Fatalf("configuration changed: %s, err=%v", actual, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("unexpected files: %v, err=%v", entries, err)
			}
		})
	}
}

func TestManualUpgradeFlagsStillParse(t *testing.T) {
	for _, args := range [][]string{
		{"--check", "--version", "1.2.3"}, {"--version", "1.2.3"},
		{"--migrate-database"}, {"--no-restart"}, {"--skip-preflight-health"},
		{"--check", "--github-mirror", "https://github-mirror.example.invalid", "--release-assets-mirror", "https://assets-mirror.example.invalid"},
	} {
		var stdout, stderr bytes.Buffer
		args = append(args, "--config", filepath.Join(t.TempDir(), "missing.json"))
		if code := runUpgrade(args, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "load upgrade configuration") {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr.String())
		}
	}
}

func TestUpgradeUsageOnlyAdvertisesManualOptions(t *testing.T) {
	var stderr bytes.Buffer
	upgradeUsage(&stderr)
	for _, removed := range []string{"auto", "--auto-worker"} {
		if strings.Contains(stderr.String(), removed) {
			t.Fatalf("usage advertises %q: %s", removed, stderr.String())
		}
	}
}

func TestPackageOnlyUpgradeSkipsUnavailableRuntimeConfiguration(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")
	var stdout, stderr bytes.Buffer
	code := runUpgrade([]string{
		"--package-only", "--config", missing, "--version", "invalid",
		"--state-directory", dir, "--lock-file", filepath.Join(dir, "upgrade.lock"),
	}, &stdout, &stderr)
	// An invalid target/platform must return an ordinary upgrade error, never
	// panic or fall back to configuration loading during package repair.
	if code != 1 || strings.Contains(stderr.String(), "load upgrade configuration") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("package repair touched runtime configuration: %v", err)
	}
}

func TestPackageOnlyUpgradeHasNoDatabaseErrorRecorder(t *testing.T) {
	record, closeRecorder := newUpgradeSystemErrorRecorder(context.Background(), config.RuntimeDocument{})
	defer closeRecorder()
	if record != nil {
		t.Fatal("package-only repair must not connect to a database error recorder")
	}
}
