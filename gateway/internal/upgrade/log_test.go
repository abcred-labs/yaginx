package upgrade

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOperationLoggerRotatesAndUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.log")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), logMaxBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	var terminal bytes.Buffer
	logger := newOperationLogger(&terminal, path, time.Now)
	logger.Event("test", "rotation complete")
	logger.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions=%#o, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated log missing: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(contents), "stage=test") || terminal.String() != string(contents) {
		t.Fatalf("contents=%q terminal=%q err=%v", contents, terminal.String(), err)
	}
}

func TestRunCommandStreamsOutputToBothDestinations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.log")
	var terminal bytes.Buffer
	logger := newOperationLogger(&terminal, path, time.Now)
	defer logger.Close()
	runner := &Runner{StreamCommand: func(_ context.Context, outputWriter io.Writer, _ string, _ ...string) error {
		_, err := io.WriteString(outputWriter, "dpkg: configuring yaginx\n")
		return err
	}}
	if err := runner.runCommand(context.Background(), logger, "dpkg", "-i", "candidate.deb"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(contents), "dpkg: configuring yaginx") || !strings.Contains(terminal.String(), "command.output") {
		t.Fatalf("contents=%q terminal=%q err=%v", contents, terminal.String(), err)
	}
}

func TestOperationLoggerContinuesWhenLogFileIsUnavailable(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	var terminal bytes.Buffer
	logger := newOperationLogger(&terminal, filepath.Join(parent, "upgrade.log"), time.Now)
	logger.Event("test", "terminal output remains available")
	logger.Close()
	if !strings.Contains(terminal.String(), "upgrade log file unavailable") || !strings.Contains(terminal.String(), "terminal output remains available") {
		t.Fatalf("terminal=%q", terminal.String())
	}
}
