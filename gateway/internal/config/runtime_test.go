package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRuntimeDefaultsExposeUnifiedPorts(t *testing.T) {
	v := map[string]any{"configVersion": float64(1), "database": map[string]any{"dsn": "postgres://test@127.0.0.1/test?sslmode=disable"}}
	if err := applyRuntimeDefaults(v); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntime(v); err != nil {
		t.Fatal(err)
	}
	ports := section(v, "ports")
	if ports["bindAddress"] != "0.0.0.0" || ports["http"] != float64(8080) || ports["proxyHttps"] != float64(9443) || ports["management"] != float64(9090) {
		t.Fatalf("unified port defaults were not materialized: %#v", ports)
	}
	if got := (RuntimeDocument{Values: v}).Strings("management", "allowedCIDRs"); len(got) != 5 || got[0] != "127.0.0.1/32" || got[4] != "100.64.0.0/10" {
		t.Fatalf("management CIDR defaults = %#v", got)
	}
	if got := section(v, "database")["minimumServerMajor"]; got != float64(15) {
		t.Fatalf("minimum PostgreSQL major = %#v, want 15", got)
	}
	runtimeDefaults := RuntimeDocument{Values: v}
	if got := runtimeDefaults.Int64("proxy", "maxRequestBodyBytes"); got != 0 {
		t.Fatalf("default proxy request body limit = %d, want unlimited (0)", got)
	}
	if got, err := runtimeDefaults.Duration("proxy", "activityTimeout"); err != nil || got != 5*time.Minute {
		t.Fatalf("proxy activity timeout = %s, %v; want 5m", got, err)
	}
	if got, err := runtimeDefaults.Duration("proxy", "responseHeaderTimeout"); err != nil || got != 30*time.Second {
		t.Fatalf("proxy response header timeout = %s, %v; want 30s", got, err)
	}
	if runtimeDefaults.Bool("trafficAggregation", "enableUAParsing") || !runtimeDefaults.Bool("trafficAggregation", "updateCurrentPeriod") || !runtimeDefaults.Bool("requestHistory", "enabled") || runtimeDefaults.Int("requestHistory", "retentionHours") != 168 {
		t.Fatalf("observability audit defaults were not materialized: %#v", v)
	}
	if got, err := runtimeDefaults.Duration("resourceReports", "contributionRetention"); err != nil || got < 31*24*time.Hour {
		t.Fatalf("contribution retention = %s, %v; want at least 744h", got, err)
	}
	if got, err := runtimeDefaults.Duration("resourceReports", "minutelyReconciliationLookback"); err != nil || got != 10*time.Minute {
		t.Fatalf("minutely reconciliation lookback = %s, %v; want 10m", got, err)
	}
	if got, err := runtimeDefaults.Duration("resourceReports", "materializationStatementTimeout"); err != nil || got != 2*time.Minute {
		t.Fatalf("materialization statement timeout = %s, %v; want 2m", got, err)
	}
	if runtimeDefaults.Int("resourceReports", "minutelyRetentionDays") != 30 || runtimeDefaults.Int("resourceReports", "hourlyRetentionDays") != 365 || runtimeDefaults.Int("resourceReports", "dailyRetentionDays") != 730 {
		t.Fatalf("report retention defaults were not materialized: %#v", section(v, "resourceReports"))
	}
	runtime := RuntimeDocument{Values: v}
	if got, err := runtime.ListenerAddress("http"); err != nil || got != "0.0.0.0:8080" {
		t.Fatalf("HTTP address = %q, %v", got, err)
	}
	if got, err := runtime.ManagementAddress(); err != nil || got != "0.0.0.0:9090" {
		t.Fatalf("management address = %q, %v", got, err)
	}
	if got, err := runtime.HTTP3Address(); err != nil || got != "0.0.0.0:8443" {
		t.Fatalf("HTTP/3 address = %q, %v", got, err)
	}
}

func TestLoadRuntimeMovesLegacyManagementCIDRsToLocalSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	values := map[string]any{
		"configVersion": float64(1),
		"database":      map[string]any{"dsn": "postgres://test@127.0.0.1/test"},
		"observability": map[string]any{"allowedCIDRs": []any{"192.0.2.0/24"}},
	}
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := runtime.Strings("management", "allowedCIDRs"); len(got) != 1 || got[0] != "192.0.2.0/24" {
		t.Fatalf("management CIDRs = %#v", got)
	}
	if got := runtime.Strings("observability", "allowedCIDRs"); len(got) != 0 {
		t.Fatalf("legacy observability CIDRs remained: %#v", got)
	}
}

func TestSaveRuntimeIsAtomicAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	values, err := NewRuntimeValues("postgres://test@127.0.0.1/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveRuntime(path, values); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DatabaseDSN() != values["database"].(map[string]any)["dsn"] {
		t.Fatalf("saved DSN=%q", loaded.DatabaseDSN())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode=%o, want 600", info.Mode().Perm())
	}
}

func TestRuntimeRejectsDuplicateTCPPorts(t *testing.T) {
	v := map[string]any{"configVersion": float64(1), "database": map[string]any{"dsn": "postgres://test@127.0.0.1/test?sslmode=disable"}, "ports": map[string]any{"http": float64(8080), "management": float64(8080)}}
	if err := applyRuntimeDefaults(v); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntime(v); err == nil {
		t.Fatal("validateRuntime accepted duplicate TCP ports")
	}
}

func TestRuntimeRejectsRemovedSettingsSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"configVersion":1,"database":{"dsn":"postgres://test@127.0.0.1/test?sslmode=disable"},"legacy":{"sourceDirectory":"/data/yaginx","settingsFile":"../appsettings.Production.json"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRuntime(path); err == nil {
		t.Fatal("LoadRuntime accepted removed settings section")
	}
}

func TestEmbeddedRuntimeSchemaMatchesContract(t *testing.T) {
	contractPath := filepath.Join("..", "..", "..", "planning", "go-rewrite", "contracts", "runtime-config.schema.json")
	want, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(runtimeConfigSchema, want) {
		t.Fatal("embedded runtime schema differs from planning contract; copy the authoritative contract before release")
	}
}

func TestExampleRuntimeConfigurationUsesUnifiedPorts(t *testing.T) {
	t.Setenv("YAGINX_MANAGEMENT_PORT", "")
	examplePath := filepath.Join("..", "..", "config", "yaginx.example.json")
	runtime, err := LoadRuntime(examplePath)
	if err != nil {
		t.Fatalf("load example runtime configuration: %v", err)
	}
	if got, err := runtime.ManagementAddress(); err != nil || got != "127.0.0.1:29090" {
		t.Fatalf("example management address = %q, %v", got, err)
	}
}

func TestReplacementExampleUsesHAProxyTopology(t *testing.T) {
	for _, key := range []string{
		"YAGINX_HTTP_PORT", "YAGINX_PROXY_HTTP_PORT", "YAGINX_HTTPS_PORT",
		"YAGINX_PROXY_HTTPS_PORT", "YAGINX_MANAGEMENT_PORT", "YAGINX_HTTP3_ENABLED",
		"YAGINX_ACME_ENABLED", "YAGINX_PROXY_TRUSTED_CIDRS",
	} {
		t.Setenv(key, "")
	}
	examplePath := filepath.Join("..", "..", "config", "yaginx-example-ha-proxy.json")
	runtime, err := LoadRuntime(examplePath)
	if err != nil {
		t.Fatalf("load replacement example: %v", err)
	}
	if got, err := runtime.ManagementAddress(); err != nil || got != "0.0.0.0:9090" {
		t.Fatalf("management address = %q, %v", got, err)
	}
	if got := runtime.Strings("listeners.proxyProtocol", "trustedCIDRs"); len(got) != 1 || got[0] != "127.0.0.1/32" {
		t.Fatalf("trusted proxy CIDRs = %#v", got)
	}
	if runtime.Bool("listeners.http3", "enabled") || runtime.Bool("upgrade", "enabled") {
		t.Fatalf("replacement example enables an initial-cutover feature: %#v", runtime.Values)
	}
	if got := runtime.Strings("management", "allowedCIDRs"); len(got) != 3 || got[0] != "198.51.100.0/24" {
		t.Fatalf("management CIDRs = %#v", got)
	}
}

func TestRuntimeSchemaRejectsInvalidNestedValues(t *testing.T) {
	v := map[string]any{"configVersion": float64(1), "database": map[string]any{"dsn": "postgres://test@127.0.0.1/test?sslmode=disable"}, "requestHistory": map[string]any{"recordMode": "invalid"}}
	if err := applyRuntimeDefaults(v); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntime(v); err == nil {
		t.Fatal("validateRuntime accepted an invalid nested contract value")
	}
}

func TestRuntimeAllowsCustomACMEKeyValueURLAndEnablesProviderByDefault(t *testing.T) {
	v := map[string]any{
		"configVersion": float64(1),
		"database":      map[string]any{"dsn": "postgres://test@127.0.0.1/test?sslmode=disable"},
		"acme":          map[string]any{"keyValueEndpointURL": "http://provider-node:9090"},
	}
	if err := applyRuntimeDefaults(v); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntime(v); err != nil {
		t.Fatal(err)
	}
	runtime := RuntimeDocument{Values: v}
	if got := runtime.ACMEKeyValueEndpointURL(); got != "http://provider-node:9090" {
		t.Fatalf("key-value endpoint URL=%q", got)
	}
}

func TestRuntimeRejectsLegacyACMEKeyValueBaseURLField(t *testing.T) {
	v := map[string]any{
		"configVersion": float64(1),
		"database":      map[string]any{"dsn": "postgres://test@127.0.0.1/test?sslmode=disable"},
		"acme":          map[string]any{"keyValueBaseURL": "http://provider-node:9090"},
	}
	if err := applyRuntimeDefaults(v); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntime(v); err == nil {
		t.Fatal("validateRuntime accepted the removed legacy acme.keyValueBaseURL field")
	}
}

func TestRuntimeDefaultsACMEKeyValueURLToLocalManagementAddress(t *testing.T) {
	values := map[string]any{
		"configVersion": float64(1),
		"database":      map[string]any{"dsn": "postgres://test@127.0.0.1/test?sslmode=disable"},
	}
	if err := applyRuntimeDefaults(values); err != nil {
		t.Fatal(err)
	}
	runtime := RuntimeDocument{Values: values}
	for _, tc := range []struct{ bind, want string }{
		{"0.0.0.0", "http://127.0.0.1:9090"},
		{"::", "http://127.0.0.1:9090"},
		{"198.51.100.82", "http://198.51.100.82:9090"},
	} {
		section(values, "ports")["bindAddress"] = tc.bind
		got, err := runtime.ACMEKeyValueBaseURL()
		if err != nil || got != tc.want {
			t.Fatalf("bind=%q got=%q err=%v want=%q", tc.bind, got, err, tc.want)
		}
	}
}

func TestRuntimeRejectsNonHTTPACMEKeyValueURL(t *testing.T) {
	for _, value := range []string{"relative/path", "ftp://provider-node:9090"} {
		t.Run(value, func(t *testing.T) {
			v := map[string]any{
				"configVersion": float64(1),
				"database":      map[string]any{"dsn": "postgres://test@127.0.0.1/test?sslmode=disable"},
				"acme":          map[string]any{"keyValueEndpointURL": value},
			}
			if err := applyRuntimeDefaults(v); err != nil {
				t.Fatal(err)
			}
			if err := validateRuntime(v); err == nil {
				t.Fatalf("accepted invalid key-value URL %q", value)
			}
		})
	}
}

func TestRuntimePreservesUnlimitedBodyLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{"configVersion":1,"database":{"dsn":"postgres://test@127.0.0.1/test?sslmode=disable"},"proxy":{"maxRequestBodyBytes":0}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := runtime.Int64("proxy", "maxRequestBodyBytes"); got != 0 {
		t.Fatalf("body limit=%d, want unlimited (0)", got)
	}
	if len(runtime.Warnings) != 0 {
		t.Fatalf("unexpected body limit warnings=%#v", runtime.Warnings)
	}
}

func TestRuntimeAcceptsLargeRequestBodyLimitAsInt64(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{"configVersion":1,"database":{"dsn":"postgres://test@127.0.0.1/test?sslmode=disable"},"proxy":{"maxRequestBodyBytes":1099511627776}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := LoadRuntime(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := runtime.Int64("proxy", "maxRequestBodyBytes"); got != 1<<40 {
		t.Fatalf("body limit=%d, want %d", got, int64(1<<40))
	}
}

func TestRuntimeRejectsEnabledOTLPWithoutAbsoluteEndpoint(t *testing.T) {
	v := map[string]any{"configVersion": float64(1), "database": map[string]any{"dsn": "postgres://test@127.0.0.1/test?sslmode=disable"}, "observability": map[string]any{"otlp": map[string]any{"enabled": true}}}
	if err := applyRuntimeDefaults(v); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntime(v); err == nil {
		t.Fatal("enabled OTLP without endpoint was accepted")
	}
}

func TestRuntimeNestedSectionAccess(t *testing.T) {
	v := map[string]any{"configVersion": float64(1), "database": map[string]any{"dsn": "postgres://test@127.0.0.1/test?sslmode=disable"}}
	if err := applyRuntimeDefaults(v); err != nil {
		t.Fatal(err)
	}
	if got := (RuntimeDocument{Values: v}).Strings("listeners.proxyProtocol", "trustedCIDRs"); len(got) != 5 {
		t.Fatalf("trusted CIDRs = %#v, want default nested values", got)
	}
	if got, err := (RuntimeDocument{Values: v}).Duration("listeners.proxyProtocol", "headerTimeout"); err != nil || got.String() != "30s" {
		t.Fatalf("header timeout = %s, %v", got, err)
	}
	if got, err := (RuntimeDocument{Values: v}).Duration("acme", "renewBefore"); err != nil || got != 7*24*time.Hour {
		t.Fatalf("ACME renewal window = %s, %v; want %s", got, err, 7*24*time.Hour)
	}
}

func TestLegacyNetworkAndACMEFieldsMigrateBeforeValidation(t *testing.T) {
	v := map[string]any{"configVersion": float64(1), "database": map[string]any{"dsn": "postgres://test@127.0.0.1/test?sslmode=disable"}, "observability": map[string]any{"allowedCIDRs": []any{"10.0.0.0/8"}}, "acme": map[string]any{"keyValueBaseURL": "http://provider:9090"}}
	doc := filepath.Join(t.TempDir(), "config.json")
	raw, _ := json.Marshal(v)
	if err := os.WriteFile(doc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRuntime(doc)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.String("acme", "keyValueEndpointURL") != "http://provider:9090" {
		t.Fatalf("endpoint was not migrated: %#v", loaded.Values)
	}
	if got := loaded.Strings("management", "allowedCIDRs"); len(got) != 1 || got[0] != "10.0.0.0/8" {
		t.Fatalf("management CIDRs=%#v", got)
	}
	if len(loaded.Warnings) != 2 {
		t.Fatalf("migration warnings=%#v", loaded.Warnings)
	}
}

func TestRuntimeAppliesEveryDeclaredEnvironmentOverride(t *testing.T) {
	for key, value := range map[string]string{
		"YAGINX_DATABASE_MAX_CONNECTIONS": "41",
		"YAGINX_DATABASE_MIN_CONNECTIONS": "5",
		"YAGINX_NODE_IDENTITY_FILE":       "/run/test/identity",
		"YAGINX_SNAPSHOT_FILE":            "/run/test/snapshot",
		"YAGINX_HTTP_PORT":                "18080",
		"YAGINX_PROXY_HTTP_PORT":          "19080",
		"YAGINX_HTTPS_PORT":               "18443",
		"YAGINX_PROXY_HTTPS_PORT":         "19443",
		"YAGINX_MANAGEMENT_PORT":          "19090",
		"YAGINX_HTTP3_ENABLED":            "false",
		"YAGINX_PROXY_TRUSTED_CIDRS":      "10.0.0.0/8, 2001:db8::/32",
		"YAGINX_ADMIN_UI_DIR":             "/run/test/adminui",
		"YAGINX_ACME_ENABLED":             "false",
		"YAGINX_LOG_LEVEL":                "debug",
		"YAGINX_LOG_FORMAT":               "text",
		"YAGINX_MANAGEMENT_ADDRESS":       "127.0.0.2",
		"YAGINX_UPGRADE_MANIFEST_URL":     "https://updates.example.invalid/latest",
	} {
		t.Setenv(key, value)
	}
	v := map[string]any{"configVersion": float64(1), "database": map[string]any{"dsn": "postgres://test@127.0.0.1/test?sslmode=disable"}}
	if err := applyRuntimeDefaults(v); err != nil {
		t.Fatal(err)
	}
	if err := applyRuntimeEnvironment(v); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntime(v); err != nil {
		t.Fatal(err)
	}
	runtime := RuntimeDocument{Values: v}
	if runtime.ListenerPort("http") != 18080 || runtime.ListenerPort("proxyHttp") != 19080 || runtime.ListenerPort("https") != 18443 || runtime.ListenerPort("proxyHttps") != 19443 || runtime.ListenerPort("management") != 19090 {
		t.Fatalf("port overrides = %#v", section(v, "ports"))
	}
	if runtime.Bool("listeners.http3", "enabled") || runtime.Bool("acme", "enabled") || runtime.Int("database", "maxConnections") != 41 {
		t.Fatalf("environment overrides were not materialized: %#v", v)
	}
	if got := runtime.DatabaseDSN(); got != "postgres://test@127.0.0.1/test?sslmode=disable" {
		t.Fatalf("database DSN = %q", got)
	}
	if got, err := runtime.ManagementAddress(); err != nil || got != "0.0.0.0:19090" {
		t.Fatalf("YAGINX_MANAGEMENT_ADDRESS affected shared binding: %q, %v", got, err)
	}
	if got := runtime.Strings("listeners.proxyProtocol", "trustedCIDRs"); len(got) != 2 || got[0] != "10.0.0.0/8" || got[1] != "2001:db8::/32" {
		t.Fatalf("trusted CIDRs = %#v", got)
	}
}

func TestRuntimeRequiresInlineDatabaseDSN(t *testing.T) {
	for name, document := range map[string]string{
		"missing": `{"configVersion":1}`,
		"empty":   `{"configVersion":1,"database":{"dsn":""}}`,
		"removed": `{"configVersion":1,"database":{"dsnFile":"/removed/postgres-dsn"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadRuntime(path); err == nil {
				t.Fatal("LoadRuntime accepted an invalid database configuration")
			}
		})
	}
}

func TestRuntimeDoesNotApplyRemovedDatabaseDSNFileOverride(t *testing.T) {
	t.Setenv("YAGINX_DATABASE_DSN_FILE", "/run/test/dsn")
	v := map[string]any{"configVersion": float64(1), "database": map[string]any{"dsn": "postgres://test@127.0.0.1/test?sslmode=disable"}}
	if err := applyRuntimeDefaults(v); err != nil {
		t.Fatal(err)
	}
	if err := applyRuntimeEnvironment(v); err != nil {
		t.Fatal(err)
	}
	if got := (RuntimeDocument{Values: v}).DatabaseDSN(); got != "postgres://test@127.0.0.1/test?sslmode=disable" {
		t.Fatalf("removed environment override changed database DSN to %q", got)
	}
}
