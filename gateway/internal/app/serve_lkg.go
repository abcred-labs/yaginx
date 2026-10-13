package app

// Last known good runtime state on disk. A node that cannot reach the control
// plane serves from these files, so every successful snapshot is persisted and
// a failed write never discards what is already there.

import (
	"encoding/json"
	"fmt"
	"os"

	"yaginx-gateway/internal/certificates"
	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/configsync"
	"yaginx-gateway/internal/core"
	"yaginx-gateway/internal/platform/identity"
	"yaginx-gateway/internal/proxy"
)

func loadOfflineSnapshot(cfg config.RuntimeDocument, node identity.Identity) (*proxy.Snapshot, error) {
	snapshot, _, err := loadOfflineRuntime(cfg, node)
	return snapshot, err
}

type offlineCertificate struct {
	ChainPEM []byte `json:"chainPem"`
	KeyPEM   []byte `json:"keyPem"`
}

type offlineRuntime struct {
	Version      int                  `json:"version"`
	Snapshot     proxy.Snapshot       `json:"snapshot"`
	Certificates []offlineCertificate `json:"certificates"`
}

func persistRuntimeLKG(path string, snapshot *proxy.Snapshot, entries []certificates.Entry) error {
	if snapshot == nil {
		return fmt.Errorf("LKG proxy snapshot is nil")
	}
	runtime := offlineRuntime{Version: 1, Snapshot: *snapshot, Certificates: make([]offlineCertificate, 0, len(entries))}
	for _, entry := range entries {
		chain, key, err := certificates.EncodePEM(entry)
		if err != nil {
			return err
		}
		runtime.Certificates = append(runtime.Certificates, offlineCertificate{ChainPEM: chain, KeyPEM: key})
	}
	raw, err := json.Marshal(runtime)
	if err != nil {
		return err
	}
	return configsync.SaveLKG(path, raw)
}

// persistCoreRuntime stores the static rescue input only after the normal
// runtime has accepted the same rules and certificates in memory.
func persistCoreRuntime(snapshot *proxy.Snapshot, entries []certificates.Entry, listenerPlan core.ListenerPlan, settings proxy.Settings) error {
	return persistCoreRuntimeAt(core.DefaultSnapshotPath, snapshot, entries, listenerPlan, settings)
}

func persistCoreRuntimeAt(path string, snapshot *proxy.Snapshot, entries []certificates.Entry, listenerPlan core.ListenerPlan, settings proxy.Settings) error {
	runtime, err := core.NewRuntime(snapshot, entries, listenerPlan, settings)
	if err != nil {
		return err
	}
	return core.Save(path, runtime)
}

func loadOfflineRuntime(cfg config.RuntimeDocument, node identity.Identity) (*proxy.Snapshot, []certificates.Entry, error) {
	raw, err := configsync.LoadLKG(cfg.String("node", "snapshotFile"), os.ReadFile)
	if err != nil {
		return nil, nil, err
	}
	var runtime offlineRuntime
	if err := json.Unmarshal(raw, &runtime); err == nil && runtime.Version == 1 {
		if len(runtime.Snapshot.Sites) == 0 {
			return nil, nil, fmt.Errorf("LKG snapshot has no sites")
		}
		entries := make([]certificates.Entry, 0, len(runtime.Certificates))
		for _, item := range runtime.Certificates {
			entry, err := certificates.ParsePEM(item.ChainPEM, item.KeyPEM, nil)
			if err != nil {
				return nil, nil, fmt.Errorf("decode LKG certificate: %w", err)
			}
			entries = append(entries, entry)
		}
		return &runtime.Snapshot, entries, nil
	}
	// The early plaintext format persisted only the proxy JSON object. Retain it
	// for local snapshot compatibility; the normal service path always loads
	// its current certificate material from PostgreSQL.
	var snapshot proxy.Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, nil, fmt.Errorf("decode LKG snapshot: %w", err)
	}
	if len(snapshot.Sites) == 0 {
		return nil, nil, fmt.Errorf("LKG snapshot has no sites")
	}
	return &snapshot, nil, nil
}

// spaStatic serves generated assets normally and falls back to index.html for
// client-side routes. Filesystem traversal remains handled by FileServer.
