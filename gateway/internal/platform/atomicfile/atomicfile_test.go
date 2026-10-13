package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteCreatesPrivateAtomicFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "snapshot.bin")
	if err := Write(path, []byte("snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "snapshot" {
		t.Fatalf("file=%q err=%v", got, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("mode=%#o", info.Mode().Perm())
		}
	}
}

func TestWriteRejectsSymlinkTargetAndParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink policy is validated on Unix")
	}
	root := t.TempDir()
	target := filepath.Join(root, "outside")
	if err := os.WriteFile(target, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "snapshot.bin")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Write(link, []byte("replacement"), 0600); err == nil {
		t.Fatal("symlink target accepted")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "outside" {
		t.Fatalf("outside target changed=%q err=%v", got, err)
	}
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatal(err)
	}
	parentLink := filepath.Join(root, "runtime")
	if err := os.Symlink(realDir, parentLink); err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(parentLink, "snapshot.bin"), []byte("replacement"), 0600); err == nil {
		t.Fatal("symlink parent accepted")
	}
}
