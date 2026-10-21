package repocheck

import (
	"fmt"
	"os"
	"path/filepath"
)

// findWorkspaceFile walks up from start until the repository-relative path
// exists, so the tests run from any package directory.
func findWorkspaceFile(start, relative string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, relative)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find %s from %s", relative, start)
		}
		dir = parent
	}
}
