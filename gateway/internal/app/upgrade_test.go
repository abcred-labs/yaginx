package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestUpgradeAutoWorkerRejectsTargetVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runUpgrade([]string{"--auto-worker", "--version", "1.2.3"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "cannot be combined") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}
