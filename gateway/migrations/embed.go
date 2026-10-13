package migrations

import (
	"embed"
	"io/fs"
	"sort"
)

// Files is the migration source used by the future migration runner.
//
//go:embed *.sql
var Files embed.FS

func Names() ([]string, error) {
	entries, err := fs.ReadDir(Files, ".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
