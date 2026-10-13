package database

import (
	"io/fs"
	"regexp"
	"strconv"
	"testing"

	v2migrations "yaginx-gateway/migrations/v2"
)

func TestLatestV2MigrationMatchesEmbeddedLineage(t *testing.T) {
	var highest int64
	pattern := regexp.MustCompile(`^(\d+)_.*\.sql$`)
	if err := fs.WalkDir(v2migrations.Files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		match := pattern.FindStringSubmatch(entry.Name())
		if len(match) == 0 {
			return nil
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return err
		}
		if version > highest {
			highest = version
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if highest != latestV2Migration {
		t.Fatalf("latestV2Migration=%d, but embedded migration lineage ends at %d", latestV2Migration, highest)
	}
}
