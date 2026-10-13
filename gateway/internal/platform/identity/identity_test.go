package identity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestLoadOrCreateWritesCanonicalUUIDAndIsStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	first, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(first.NodeID); err != nil || len(first.NodeID) != 36 {
		t.Fatalf("new node ID = %q, parse error = %v", first.NodeID, err)
	}
	second, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if second.NodeID != first.NodeID {
		t.Fatalf("node ID changed from %q to %q", first.NodeID, second.NodeID)
	}
}

func TestLoadOrCreateAcceptsLegacyCompactUUID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	const legacy = "0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(path, []byte(`{"nodeId":"`+legacy+`","createdAt":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if identity.NodeID != legacy {
		t.Fatalf("legacy identity changed to %q", identity.NodeID)
	}
}

func TestLoadDoesNotCreateMissingIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	if _, err := Load(path); !os.IsNotExist(err) {
		t.Fatalf("Load error=%v, want not-exist", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Load created identity file: stat error=%v", err)
	}
}
