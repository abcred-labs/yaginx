// Package identity creates and persists a stable, opaque node identifier.
package identity

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"yaginx-gateway/internal/platform/atomicfile"
)

type Identity struct {
	NodeID    string    `json:"nodeId"`
	CreatedAt time.Time `json:"createdAt"`
}

// ClusterID derives a stable opaque identifier from the shared database
// connection. Nodes using the same PostgreSQL deployment therefore identify
// the same Yaginx cluster without exposing DSNs or database credentials.
func ClusterID(databaseDSN string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(databaseDSN)))
	return fmt.Sprintf("%x", sum[:16])
}

// Load reads an existing node identity without creating a new identity file.
// Administrative commands use it so a typo in an identity path cannot create
// an unregistered node and then operate on it.
func Load(path string) (Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Identity{}, err
	}
	var value Identity
	if err := json.Unmarshal(data, &value); err != nil {
		return Identity{}, fmt.Errorf("decode identity: %w", err)
	}
	// uuid.Parse accepts the compact 32-hex representation emitted by the
	// early Go prototype. Keep it readable so its existing LKG AAD remains
	// valid, while all newly created identities use canonical UUID text.
	if _, err := uuid.Parse(value.NodeID); err != nil {
		return Identity{}, fmt.Errorf("identity nodeId is invalid: %w", err)
	}
	return value, nil
}

// MachineFingerprint is deliberately independent from the persisted node ID.
// A copied state directory must therefore not impersonate the source host.
// Linux production nodes use machine-id; the hostname fallback only keeps
// local development environments usable and is surfaced by doctor separately.
func MachineFingerprint() (string, error) {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if value := strings.TrimSpace(string(data)); value != "" {
			sum := sha256.Sum256([]byte(value))
			return fmt.Sprintf("%x", sum), nil
		}
	}
	host, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("read machine identity: %w", err)
	}
	if strings.TrimSpace(host) == "" {
		return "", fmt.Errorf("read machine identity: hostname is empty")
	}
	sum := sha256.Sum256([]byte("hostname:" + host))
	return fmt.Sprintf("%x", sum), nil
}

func LoadOrCreate(path string) (Identity, error) {
	if value, err := Load(path); err == nil {
		return value, nil
	} else if !os.IsNotExist(err) {
		return Identity{}, err
	}
	// uuid.NewRandom uses crypto/rand and gives PostgreSQL/API callers an
	// unambiguous canonical UUID from the first node registration onward.
	id, err := uuid.NewRandom()
	if err != nil {
		return Identity{}, err
	}
	value := Identity{NodeID: id.String(), CreatedAt: time.Now().UTC()}
	data, err := json.Marshal(value)
	if err != nil {
		return Identity{}, err
	}
	if err := atomicfile.Write(path, data, 0600); err != nil {
		return Identity{}, err
	}
	return value, nil
}
