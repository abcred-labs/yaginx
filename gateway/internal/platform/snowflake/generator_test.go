package snowflake

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestGeneratorLayoutAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snowflake.state")
	g, err := LoadOrCreate(7, path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.UnixMilli(Epoch + 42)
	g.now = func() time.Time { return base }
	first, err := g.Next()
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.Next()
	if err != nil || second != first+1 {
		t.Fatalf("next=%d err=%v first=%d", second, err, first)
	}
	if first>>22 != 42 || (first>>12)&0x3ff != 7 {
		t.Fatalf("layout=%d", first)
	}
	if err := g.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadOrCreate(7, path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.now = func() time.Time { return base.Add(-10 * time.Millisecond) }
	if _, err := reloaded.Next(); !errors.Is(err, ErrClockRollback) {
		t.Fatalf("clock rollback error=%v", err)
	}
}
