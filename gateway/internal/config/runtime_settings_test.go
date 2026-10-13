package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractSharedRuntimeAndBootstrapValues(t *testing.T) {
	values := map[string]any{
		"configVersion": float64(1),
		"database":      map[string]any{"dsn": "postgres://secret"},
		"node":          map[string]any{"identityFile": "/var/lib/yaginx/node/identity.json"},
		"ports":         map[string]any{"management": float64(9090)},
		"proxy":         map[string]any{"maxConcurrentRequests": float64(42)},
		"acme":          map[string]any{"enabled": true},
	}
	shared, err := ExtractSharedRuntime(values)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := shared["proxy"]; !ok {
		t.Fatalf("shared settings=%#v", shared)
	}
	if _, ok := shared["database"]; ok {
		t.Fatalf("database leaked into shared settings=%#v", shared)
	}
	values["listeners"] = map[string]any{"http3": map[string]any{"enabled": false}}
	values["observability"] = map[string]any{"allowedCIDRs": []any{"192.0.2.0/24"}, "logLevel": "info"}
	shared, err = ExtractSharedRuntime(values)
	if _, ok := shared["listeners"]; ok {
		t.Fatalf("listeners leaked into shared settings=%#v", shared)
	}
	if observability := shared["observability"].(map[string]any); observability["allowedCIDRs"] != nil {
		t.Fatalf("management CIDRs leaked into shared settings=%#v", shared)
	}
	bootstrap, err := BootstrapValues(values)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bootstrap["proxy"]; ok {
		t.Fatalf("proxy leaked into bootstrap settings=%#v", bootstrap)
	}
	if _, ok := bootstrap["database"]; !ok {
		t.Fatalf("database missing from bootstrap settings=%#v", bootstrap)
	}
}

func TestApplySharedRuntimePreservesLocalNetworkSettings(t *testing.T) {
	values := map[string]any{
		"configVersion": float64(1),
		"database":      map[string]any{"dsn": "postgres://test@127.0.0.1/test"},
		"management":    map[string]any{"allowedCIDRs": []any{"192.0.2.0/24"}},
		"ports":         map[string]any{"management": float64(19090)},
		"listeners":     map[string]any{"http3": map[string]any{"enabled": false}},
	}
	if err := applyRuntimeDefaults(values); err != nil {
		t.Fatal(err)
	}
	shared := map[string]any{
		"listeners":     map[string]any{"http3": map[string]any{"enabled": true}},
		"observability": map[string]any{"allowedCIDRs": []any{"0.0.0.0/0", "::/0"}, "logLevel": "debug"},
	}
	if err := ApplySharedRuntime(shared, values); err != nil {
		t.Fatal(err)
	}
	if got := (RuntimeDocument{Values: values}).Strings("management", "allowedCIDRs"); len(got) != 1 || got[0] != "192.0.2.0/24" {
		t.Fatalf("management CIDRs were overridden: %#v", got)
	}
	if got := (RuntimeDocument{Values: values}).ListenerPort("management"); got != 19090 {
		t.Fatalf("management port was overridden: %d", got)
	}
	if (RuntimeDocument{Values: values}).Bool("listeners.http3", "enabled") {
		t.Fatal("local HTTP/3 setting was overridden")
	}
}

func TestMigrateFileToBootstrapBacksUpAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := map[string]any{
		"configVersion": float64(1),
		"database":      map[string]any{"dsn": "postgres://secret"},
		"node":          map[string]any{"identityFile": "/var/lib/yaginx/node/identity.json"},
		"proxy":         map[string]any{"maxConcurrentRequests": float64(42)},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateFileToBootstrap(path, legacy); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path + ".pre-runtime-migration.bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(raw) {
		t.Fatalf("backup=%s want=%s", backup, raw)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(updated, &value); err != nil {
		t.Fatal(err)
	}
	if _, ok := value["proxy"]; ok {
		t.Fatalf("proxy remained in migrated file=%#v", value)
	}
	if err := MigrateFileToBootstrap(path, legacy); err != nil {
		t.Fatal(err)
	}
	backupAgain, err := os.ReadFile(path + ".pre-runtime-migration.bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backupAgain) != string(raw) {
		t.Fatalf("backup changed on second migration")
	}
}
