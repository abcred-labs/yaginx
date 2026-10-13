//go:build linux

package upgrade

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func withLock(path string, action func() error) error {
	if path == "" {
		return fmt.Errorf("upgrade lock file is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("%w: %v", ErrLockBusy, err)
	}
	defer unix.Flock(int(file.Fd()), unix.LOCK_UN)
	return action()
}
