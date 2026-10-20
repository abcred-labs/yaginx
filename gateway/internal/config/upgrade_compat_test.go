package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func legacyUpgradeSettings() map[string]any {
	// Retired values are ignored even when they no longer match their old types.
	return map[string]any{"enabled": true, "minPublishedAge": false, "checkInterval": "invalid", "downloadTimeout": "7m"}
}

func assertManualUpgradeSettings(t *testing.T, values map[string]any) {
	t.Helper()
	upgrade := values["upgrade"].(map[string]any)
	for _, key := range []string{"enabled", "minPublishedAge", "checkInterval"} {
		if _, exists := upgrade[key]; exists {
			t.Fatalf("retired upgrade.%s remains: %#v", key, upgrade)
		}
	}
	if upgrade["downloadTimeout"] != "7m" {
		t.Fatalf("manual settings changed: %#v", upgrade)
	}
}

func TestLoadLegacyUpgradeSettingsWarnsWithoutWriting(t *testing.T) {
	values := map[string]any{"configVersion": 1, "database": map[string]any{"dsn": "postgres://localhost/yaginx"}, "upgrade": legacyUpgradeSettings()}
	raw, _ := json.Marshal(values)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	assertManualUpgradeSettings(t, loaded.Values)
	if loaded.Values["configVersion"] != float64(1) || len(loaded.Warnings) != 3 {
		t.Fatalf("version=%v warnings=%v", loaded.Values["configVersion"], loaded.Warnings)
	}
	for _, warning := range loaded.Warnings {
		if !strings.Contains(warning, "automatic upgrades have been removed") {
			t.Fatalf("unexpected warning: %s", warning)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatalf("loading rewrote the file: %s, err=%v", after, err)
	}
	setup, err := RuntimeValuesForSetup(path, "postgres://test@127.0.0.1/test")
	if err != nil {
		t.Fatal(err)
	}
	assertManualUpgradeSettings(t, setup)
}

func TestSharedUpgradeCompatibilityAtEveryBoundary(t *testing.T) {
	for _, boundary := range []string{"normalize", "extract", "apply", "validate", "save", "validate-values"} {
		t.Run(boundary, func(t *testing.T) {
			values, err := NewRuntimeValues("postgres://test@127.0.0.1/test")
			if err != nil {
				t.Fatal(err)
			}
			document := map[string]any{"upgrade": legacyUpgradeSettings()}
			var result map[string]any
			switch boundary {
			case "normalize":
				var warnings []string
				result, err = NormalizeSharedRuntime(document, &warnings)
				if len(warnings) != 3 {
					t.Fatalf("shared configuration warnings=%v", warnings)
				}
			case "extract":
				result, err = ExtractSharedRuntime(document)
				if len(document["upgrade"].(map[string]any)) != 4 {
					t.Fatal("export changed its input")
				}
			case "apply":
				err = ApplySharedRuntime(document, values)
				result = values
			case "validate":
				err = ValidateSharedRuntime(document)
				result = document
			case "save", "validate-values":
				values["upgrade"] = legacyUpgradeSettings()
				result = values
				if boundary == "save" {
					path := filepath.Join(t.TempDir(), "saved.json")
					err = SaveRuntime(path, values)
					if err == nil {
						raw, readErr := os.ReadFile(path)
						if readErr != nil {
							t.Fatal(readErr)
						}
						err = json.Unmarshal(raw, &result)
					}
				} else {
					err = ValidateRuntimeValues(values)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			assertManualUpgradeSettings(t, result)
		})
	}
}

func TestNewRuntimeAndSharedExportsOmitAutomaticUpgrade(t *testing.T) {
	values, err := NewRuntimeValues("postgres://test@127.0.0.1/test")
	if err != nil {
		t.Fatal(err)
	}
	values["upgrade"].(map[string]any)["downloadTimeout"] = "7m"
	assertManualUpgradeSettings(t, values)
	shared, err := ExtractSharedRuntime(values)
	if err != nil {
		t.Fatal(err)
	}
	assertManualUpgradeSettings(t, shared)
}

func TestUpgradeCompatibilityStillRejectsUnknownFields(t *testing.T) {
	values, err := NewRuntimeValues("postgres://test@127.0.0.1/test")
	if err != nil {
		t.Fatal(err)
	}
	settings := legacyUpgradeSettings()
	settings["downloadTimout"] = "7m"
	values["upgrade"] = settings
	if err := ValidateRuntimeValues(values); err == nil {
		t.Fatal("unknown local upgrade field was accepted")
	}
	if err := ValidateSharedRuntime(map[string]any{"upgrade": settings}); err == nil {
		t.Fatal("unknown shared upgrade field was accepted")
	}
}
