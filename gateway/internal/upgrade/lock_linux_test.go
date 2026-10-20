//go:build linux

package upgrade

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestUpgradeLockConflictAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.lock")
	err := withLock(path, func() error {
		return withLock(path, func() error {
			t.Fatal("concurrent upgrade acquired the same lock")
			return nil
		})
	})
	if !errors.Is(err, ErrLockBusy) {
		t.Fatalf("lock conflict=%v, want ErrLockBusy", err)
	}
	if err := withLock(path, func() error { return nil }); err != nil {
		t.Fatalf("upgrade lock was not released: %v", err)
	}
}
