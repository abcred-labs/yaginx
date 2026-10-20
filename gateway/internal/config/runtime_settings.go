package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"yaginx-gateway/internal/platform/atomicfile"
)

// RuntimeSettingKey is the single PostgreSQL setting used for shared runtime
// configuration. Keeping the payload as one document makes updates atomic.
const RuntimeSettingKey = "runtime"

// SharedRuntimeSections are the configuration domains safe to share between
// approved nodes. Database connectivity and node-local paths are intentionally
// excluded because they are required before PostgreSQL can be queried.
var SharedRuntimeSections = []string{
	"management",
	"listeners",
	"security",
	"proxy",
	"ipLookup",
	"configSync",
	"acme",
	"trafficAggregation",
	"requestHistory",
	"resourceReports",
	"observability",
	"upgrade",
}

// ExtractSharedRuntime returns a deep-copyable JSON document containing the
// shared settings from a loaded runtime configuration.
func ExtractSharedRuntime(values map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(SharedRuntimeSections))
	for _, name := range SharedRuntimeSections {
		value, ok := values[name]
		if !ok {
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode runtime setting %s: %w", name, err)
		}
		var copied any
		if err := json.Unmarshal(raw, &copied); err != nil {
			return nil, fmt.Errorf("copy runtime setting %s: %w", name, err)
		}
		if name == "listeners" {
			if section, ok := copied.(map[string]any); ok {
				for key := range section {
					if key != "proxyProtocol" {
						delete(section, key)
					}
				}
			}
		}
		if name == "observability" {
			if section, ok := copied.(map[string]any); ok {
				delete(section, "allowedCIDRs")
				if len(section) == 0 {
					delete(out, name)
				}
			}
		}
		if name == "acme" {
			if section, ok := copied.(map[string]any); ok {
				delete(section, "enabled")
				delete(section, "keyValueServer")
			}
		}
		out[name] = copied
		if section, ok := copied.(map[string]any); ok && len(section) == 0 {
			delete(out, name)
		}
	}
	removeLegacyAutomaticUpgrade(out)
	return out, nil
}

// NormalizeSharedRuntime rejects local/bootstrap sections so a management
// caller cannot accidentally persist a database DSN or node-local path.
func NormalizeSharedRuntime(document map[string]any, warningSinks ...*[]string) (map[string]any, error) {
	if document == nil {
		return nil, fmt.Errorf("runtime settings must be an object")
	}
	warnings := migrateLegacyLocalNetworkSettings(document)
	for _, sink := range warningSinks {
		if sink != nil {
			*sink = append(*sink, warnings...)
		}
	}
	for key := range document {
		found := false
		for _, allowed := range SharedRuntimeSections {
			if key == allowed {
				found = true
				break
			}
		}
		if !found && key != "listeners" {
			return nil, fmt.Errorf("runtime setting section %q is not shared", key)
		}
	}
	return ExtractSharedRuntime(document)
}

// ApplySharedRuntime merges a PostgreSQL runtime document into the loaded
// configuration and validates the resulting complete document.
func ApplySharedRuntime(document map[string]any, values map[string]any) error {
	if document == nil {
		return fmt.Errorf("runtime settings must be an object")
	}
	removeLegacyAutomaticUpgrade(document)
	removeLegacyAutomaticUpgrade(values)
	for _, name := range SharedRuntimeSections {
		if value, ok := document[name]; ok {
			if name == "management" {
				values[name] = value
				continue
			}
			if name == "listeners" {
				incoming, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("runtime setting listeners must be an object")
				}
				if proxy, ok := incoming["proxyProtocol"]; ok {
					section(values, "listeners")["proxyProtocol"] = proxy
				}
				continue
			}
			if name == "observability" {
				incoming, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("runtime setting observability must be an object")
				}
				local := section(values, name)
				for key, setting := range incoming {
					if key != "allowedCIDRs" {
						local[key] = setting
					}
				}
				continue
			}
			values[name] = value
		}
	}
	return validateRuntime(values)
}

// ValidateSharedRuntime validates an API payload without requiring callers to
// know the local bootstrap fields.
func ValidateSharedRuntime(document map[string]any) error {
	// The DSN is node-local and absent from shared payloads; supply a validation
	// placeholder rather than requiring a database credential in the API.
	values := map[string]any{"configVersion": float64(1), "database": map[string]any{"dsn": "postgres://localhost/yaginx"}}
	if err := applyRuntimeDefaults(values); err != nil {
		return err
	}
	return ApplySharedRuntime(document, values)
}

// BootstrapValues keeps only the local startup contract after a successful
// migration. The returned value is safe to marshal as a new config file.
func BootstrapValues(values map[string]any) (map[string]any, error) {
	out := map[string]any{"configVersion": float64(1)}
	for _, name := range []string{"database", "node", "ports", "management", "listeners", "admin"} {
		value, ok := values[name]
		if !ok {
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode bootstrap setting %s: %w", name, err)
		}
		var copied any
		if err := json.Unmarshal(raw, &copied); err != nil {
			return nil, fmt.Errorf("copy bootstrap setting %s: %w", name, err)
		}
		out[name] = copied
	}
	if section, ok := out["management"].(map[string]any); ok {
		delete(section, "allowedCIDRs")
		if len(section) == 0 {
			delete(out, "management")
		}
	}
	if section, ok := out["listeners"].(map[string]any); ok {
		delete(section, "proxyProtocol")
		if len(section) == 0 {
			delete(out, "listeners")
		}
	}
	return out, nil
}

// MigrateFileToBootstrap backs up the legacy file once and atomically replaces
// it with only the local startup contract.
func MigrateFileToBootstrap(path string, values map[string]any) error {
	bootstrap, err := BootstrapValues(values)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(bootstrap, "", "  ")
	if err != nil {
		return fmt.Errorf("encode bootstrap configuration: %w", err)
	}
	raw = append(raw, '\n')
	current, readErr := os.ReadFile(path)
	if readErr != nil {
		return fmt.Errorf("read configuration for migration: %w", readErr)
	}
	if bytes.Equal(bytes.TrimSpace(current), bytes.TrimSpace(raw)) {
		return nil
	}
	backup := filepath.Clean(path) + ".pre-runtime-migration.bak"
	// Keep a timestamped forensic copy for every distinct migration source.
	timestamped := filepath.Clean(path) + ".pre-runtime-migration." + time.Now().UTC().Format("20060102T150405Z") + ".bak"
	if _, err := os.Stat(timestamped); os.IsNotExist(err) {
		if err := os.WriteFile(timestamped, current, 0o600); err != nil {
			return err
		}
	}
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		legacy, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read legacy configuration for backup: %w", readErr)
		}
		if err := os.WriteFile(backup, legacy, 0o600); err != nil {
			return fmt.Errorf("backup legacy configuration: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("inspect configuration backup: %w", err)
	}
	if err := atomicfile.Write(path, raw, 0o600); err != nil {
		return fmt.Errorf("write bootstrap configuration: %w", err)
	}
	return nil
}
