// Package atomicfile provides crash-safe replacement of private runtime files.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Write replaces path atomically.  The temporary file is created in the same
// directory so a rename never crosses filesystems.
func Write(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := rejectSymlinkComponents(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	if err := rejectSymlinkComponents(dir); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse to replace symlink target")
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("atomic file target is not regular")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect atomic file target: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".yaginx-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("set temporary mode: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		parent, err := os.Open(dir)
		if err != nil {
			return fmt.Errorf("open parent directory for sync: %w", err)
		}
		defer parent.Close()
		if err := parent.Sync(); err != nil {
			return fmt.Errorf("sync parent directory: %w", err)
		}
	}
	return nil
}

// Read returns a private runtime file without following a symlink target.
func Read(path string) ([]byte, error) {
	dir := filepath.Dir(path)
	if err := rejectSymlinkComponents(dir); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refuse to read symlink target")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("runtime file is not regular")
	}
	return os.ReadFile(path)
}

// rejectSymlinkComponents refuses an existing symlink in the target directory
// chain. Runtime snapshots and identities are security boundaries, so writing
// through a host-controlled symlink is never an acceptable convenience.
func rejectSymlinkComponents(path string) error {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	current := volume + string(filepath.Separator)
	relative, err := filepath.Rel(current, clean)
	if err != nil {
		return fmt.Errorf("resolve parent directory: %w", err)
	}
	if relative == "." {
		return nil
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect runtime parent %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// macOS exposes its system temporary hierarchy through
			// /var -> /private/var. It is a fixed OS alias, not a host-created
			// runtime path indirection; retain strict rejection for every other
			// component.
			if runtime.GOOS == "darwin" && current == "/var" {
				target, readErr := os.Readlink(current)
				if readErr == nil && target == "private/var" {
					current = "/private/var"
					continue
				}
			}
			return fmt.Errorf("runtime parent %q is a symlink", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("runtime parent %q is not a directory", current)
		}
	}
	return nil
}
