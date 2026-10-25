package config

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const runtimeSchemaID = "https://yaginx.local/contracts/runtime-config.schema.json"
const DefaultRuntimePath = "/etc/yaginx/config.json"

// runtimeConfigSchema is a release artifact copied byte-for-byte from the
// planning contract. Keeping it embedded means validation remains available
// in the single production binary instead of depending on a mutable host file.
//
//go:embed runtime-config.schema.json
var runtimeConfigSchema []byte

var (
	runtimeSchemaOnce sync.Once
	runtimeSchema     *jsonschema.Schema
	runtimeSchemaErr  error
)

// RuntimeDocument is the versioned production configuration contract. Values
// are materialized before validation, so an operator can safely keep a small
// config file and still obtain deterministic listener and security settings.
type RuntimeDocument struct {
	Values   map[string]any
	Warnings []string
}

func LoadRuntime(path string) (RuntimeDocument, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return RuntimeDocument{}, err
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		return RuntimeDocument{}, fmt.Errorf("decode runtime configuration: %w", err)
	}
	warnings := migrateLegacyLocalNetworkSettings(v)
	if err := applyRuntimeDefaults(v); err != nil {
		return RuntimeDocument{}, err
	}
	if err := applyRuntimeEnvironment(v); err != nil {
		return RuntimeDocument{}, err
	}
	if err := validateRuntime(v); err != nil {
		return RuntimeDocument{}, err
	}
	return RuntimeDocument{Values: v, Warnings: warnings}, nil
}

// ValidateRuntimeValues validates a materialized runtime document for callers
// that construct setup or migration values before writing the file.
func ValidateRuntimeValues(values map[string]any) error {
	migrateLegacyLocalNetworkSettings(values)
	return validateRuntime(values)
}

// NewRuntimeValues creates a complete validated runtime document for the
// bootstrap installer. Only the database DSN is supplied by the installer;
// all other settings use the same defaults as a normal deployment.
func NewRuntimeValues(databaseDSN string) (map[string]any, error) {
	if strings.TrimSpace(databaseDSN) == "" {
		return nil, fmt.Errorf("database DSN is required")
	}
	values := map[string]any{
		"configVersion": float64(1),
		"database":      map[string]any{"dsn": databaseDSN},
	}
	if err := applyRuntimeDefaults(values); err != nil {
		return nil, err
	}
	// A first boot must be local and non-signing until an operator explicitly
	// configures public listeners and ACME.
	section(values, "ports")["bindAddress"] = "127.0.0.1"
	section(values, "listeners.http3")["enabled"] = false
	if err := validateRuntime(values); err != nil {
		return nil, err
	}
	return values, nil
}

// RuntimeValuesForSetup preserves an existing JSON document when possible,
// changing only the database DSN. Invalid or absent documents fall back to
// the safe first-boot defaults above.
func RuntimeValuesForSetup(path, databaseDSN string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		var values map[string]any
		if json.Unmarshal(data, &values) == nil && values != nil {
			section(values, "database")["dsn"] = databaseDSN
			logLegacyWarnings(removeLegacyAutomaticUpgrade(values))
			if applyRuntimeDefaults(values) == nil && validateRuntime(values) == nil {
				return values, nil
			}
		}
	}
	return NewRuntimeValues(databaseDSN)
}

// SaveRuntime atomically replaces a root-owned runtime configuration while
// keeping the file private because it contains database credentials.
func SaveRuntime(path string, values map[string]any) error {
	logLegacyWarnings(removeLegacyAutomaticUpgrade(values))
	if err := validateRuntime(values); err != nil {
		return err
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return fmt.Errorf("encode runtime configuration: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(filepath.Clean(path))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".config.json.*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect temporary configuration: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace runtime configuration: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("protect runtime configuration: %w", err)
	}
	return nil
}
func (r RuntimeDocument) DatabaseDSN() string { return getString(r.Values, "database", "dsn") }
func (r RuntimeDocument) ListenerAddress(name string) (string, error) {
	return r.PortAddress(name)
}
func (r RuntimeDocument) PortAddress(name string) (string, error) {
	v := section(r.Values, "ports")
	host, ok := v["bindAddress"].(string)
	if !ok || host == "" {
		return "", fmt.Errorf("ports.bindAddress is required")
	}
	port, ok := v[name].(float64)
	if !ok {
		return "", fmt.Errorf("ports.%s is required", name)
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}
func (r RuntimeDocument) ListenerPort(name string) int {
	value, _ := section(r.Values, "ports")[name].(float64)
	return int(value)
}
func (r RuntimeDocument) HTTP3Address() (string, error) {
	v := section(section(r.Values, "listeners"), "http3")
	enabled, _ := v["enabled"].(bool)
	if !enabled {
		return "", nil
	}
	return r.PortAddress("https")
}
func (r RuntimeDocument) ManagementAddress() (string, error) {
	return r.PortAddress("management")
}

// ACMEKeyValueBaseURL returns the configured ACME challenge provider URL.
// An omitted URL means this node's management listener, using loopback when
// the listener is bound to a wildcard address.
func (r RuntimeDocument) ACMEKeyValueBaseURL() (string, error) {
	raw := strings.TrimSpace(getString(r.Values, "acme", "keyValueEndpointURL"))
	if raw != "" {
		parsed, err := url.ParseRequestURI(raw)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return "", fmt.Errorf("acme.keyValueEndpointURL must be an absolute http(s) URL")
		}
		return strings.TrimRight(raw, "/"), nil
	}
	host := getString(r.Values, "ports", "bindAddress")
	if host == "" {
		return "", fmt.Errorf("ports.bindAddress is required")
	}
	if host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	port := r.ListenerPort("management")
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("ports.management is required")
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func (r RuntimeDocument) ACMEKeyValueEndpointURL() string {
	return strings.TrimSpace(getString(r.Values, "acme", "keyValueEndpointURL"))
}
func (r RuntimeDocument) String(sectionName, key string) string {
	return getString(r.Values, sectionName, key)
}
func (r RuntimeDocument) Int(sectionName, key string) int {
	value, _ := section(r.Values, sectionName)[key].(float64)
	return int(value)
}
func (r RuntimeDocument) Int64(sectionName, key string) int64 {
	value, _ := section(r.Values, sectionName)[key].(float64)
	return int64(value)
}
func (r RuntimeDocument) Float(sectionName, key string) float64 {
	value, _ := section(r.Values, sectionName)[key].(float64)
	return value
}
func (r RuntimeDocument) Duration(sectionName, key string) (time.Duration, error) {
	value, ok := section(r.Values, sectionName)[key].(string)
	if !ok {
		return 0, fmt.Errorf("%s.%s must be a duration", sectionName, key)
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s.%s: %w", sectionName, key, err)
	}
	return duration, nil
}
func (r RuntimeDocument) Bool(sectionName, key string) bool {
	value, _ := section(r.Values, sectionName)[key].(bool)
	return value
}
func (r RuntimeDocument) Strings(sectionName, key string) []string {
	raw, _ := section(r.Values, sectionName)[key].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
func applyRuntimeDefaults(v map[string]any) error {
	if v == nil {
		return fmt.Errorf("runtime configuration must be an object")
	}
	if _, ok := v["configVersion"]; !ok {
		v["configVersion"] = float64(1)
	}
	merge(v, map[string]any{
		"database":           map[string]any{"minimumServerMajor": float64(15), "maxConnections": float64(8), "minConnections": float64(1), "connectTimeout": "10s", "statementTimeout": "15s", "healthQueryTimeout": "2s", "maxConnectionLifetime": "30m", "maxConnectionIdleTime": "5m"},
		"security":           map[string]any{"jwtIssuer": "Yaginx Proxy Server", "jwtAudience": "Yaingx Clients", "sessionSeconds": float64(600), "clockSkewSeconds": float64(30), "idleTimeoutSeconds": float64(7200), "absoluteSeconds": float64(43200), "rememberIdleSeconds": float64(1209600), "rememberAbsoluteSeconds": float64(2592000)},
		"node":               map[string]any{"identityFile": "/var/lib/yaginx/node/identity.json", "snapshotFile": "/var/lib/yaginx/runtime/snapshot.bin", "heartbeatInterval": "15s", "heartbeatStaleAfter": "45s", "shutdownGracePeriod": "30s", "drainTimeout": "5m"},
		"ports":              map[string]any{"bindAddress": "0.0.0.0", "http": float64(8080), "proxyHttp": float64(9080), "https": float64(8443), "proxyHttps": float64(9443), "management": float64(9090)},
		"management":         map[string]any{"allowedCIDRs": defaultCIDRs()},
		"listeners":          map[string]any{"http3": map[string]any{"enabled": true, "maxIdleTimeout": "30s"}, "proxyProtocol": map[string]any{"headerTimeout": "30s", "trustedCIDRs": defaultCIDRs(), "requiredOnProxyListeners": true}},
		"admin":              map[string]any{"basePath": "/", "uiDirectory": "/usr/share/yaginx/adminui", "apiBodyMaxBytes": float64(1048576), "indexCacheControl": "no-cache, no-store, must-revalidate", "hashedAssetCacheControl": "public, max-age=31536000, immutable"},
		"proxy":              map[string]any{"activityTimeout": "5m", "responseHeaderTimeout": "30s", "maxConcurrentRequests": float64(256), "maxRequestBodyBytes": float64(0), "maxRequestHeaderBytes": float64(65536), "maxIdleConnections": float64(128), "maxIdleConnectionsPerHost": float64(16), "idleConnectionTimeout": "90s", "expectContinueTimeout": "1s", "ipBlacklistCIDRs": []any{}},
		"ipLookup":           map[string]any{"profileURL": "https://tools.abc.red", "timeout": "3s"},
		"configSync":         map[string]any{"channel": "yaginx_config_changed", "revisionPollInterval": "30s", "notifyDebounce": "100ms", "reconnectInitialBackoff": "250ms", "reconnectMaximumBackoff": "30s", "eventRetention": "24h", "inactiveNodeAfter": "168h", "incrementalBatchSize": float64(500)},
		"acme":               map[string]any{"directoryURL": "https://acme-v02.api.letsencrypt.org/directory", "email": "operator@example.com", "keyType": "EC256", "challengeType": "http-01", "orderMode": "single-domain", "scanIntervalMinimum": "30s", "scanIntervalMaximum": "120s", "renewBefore": "360h", "lockRetryBase": "60s", "lockRetryJitterMaximum": "15s", "temporaryRetryMaximum": "6h", "keyValueEndpointURL": "", "keyValueSetPath": "/api/key_value/set", "keyValueGetPath": "/api/key_value/get", "keyValueRequestTimeout": "10s", "followRedirects": false},
		"trafficAggregation": map[string]any{"enabled": true, "flushInterval": "15s", "queueCapacity": float64(10000), "maxQueueBytes": float64(8 << 20), "batchSize": float64(500), "enableUAParsing": false, "uaCacheSize": float64(2000), "retentionDays": float64(90), "updateCurrentPeriod": true, "excludedPathPrefixes": []any{}},
		"requestHistory":     map[string]any{"enabled": true, "recordMode": "ErrorsOnly", "successSamplingRate": float64(0), "retentionHours": float64(168), "bodyCaptureMaxBytes": float64(8192), "maxHeaderBytes": float64(8192), "queueCapacity": float64(5000), "maxQueueBytes": float64(16 << 20), "fullQueueBehavior": "DropWrite", "flushInterval": "5s", "batchSize": float64(250), "maxFlushRetryCount": float64(1), "retentionDeleteBatchSize": float64(5000), "excludedPathPrefixes": []any{}, "bodyCaptureContentTypes": []any{"text/", "application/json", "application/xml", "+json", "+xml"}},
		"resourceReports":    map[string]any{"enabled": true, "minutelyDelay": "10s", "hourlyDelay": "1m", "dailyDelay": "5m", "minutelyReconciliationLookback": "10m", "reconciliationLookback": "48h", "materializationStatementTimeout": "2m", "contributionRetention": "744h", "monitorInfoRetention": "48h", "reportRetentionDays": float64(365), "minutelyRetentionDays": float64(30), "hourlyRetentionDays": float64(365), "dailyRetentionDays": float64(730), "leaderLockNamespace": "yaginx:resource-report"}, "observability": map[string]any{"logLevel": "info", "logFormat": "json", "metricsPath": "/metrics", "livePath": "/health/live", "readyPath": "/health/ready", "otlp": map[string]any{"enabled": false, "endpoint": "", "protocol": "grpc", "insecure": false, "sampleRatio": float64(0.01), "exportTimeout": "5s"}}, "upgrade": map[string]any{"manifestURL": "https://github.com/abcred-labs/yaginx/releases/latest/download/latest-release.json", "githubMirror": "", "releaseAssetsMirror": "", "component": "yaginx", "packageName": "yaginx", "stateDirectory": "/var/lib/yaginx/upgrade", "lockFile": "/run/yaginx/upgrade.lock", "downloadTimeout": "30m", "readinessTimeout": "90s", "supportedOS": "linux", "supportedArchitectures": []any{"amd64"}},
	})
	if admin := section(v, "admin"); admin["basePath"] == "/yaginx_console/" {
		admin["basePath"] = "/"
	}
	return nil
}

// migrateLegacyLocalNetworkSettings keeps pre-split config files usable. The
// management allowlist used to live under the shared observability section;
// it is now node-local because it protects this process's management socket.
func migrateLegacyLocalNetworkSettings(v map[string]any) []string {
	warnings := removeLegacyAutomaticUpgrade(v)
	if v == nil {
		return warnings
	}
	management := section(v, "management")
	observability := section(v, "observability")
	if _, exists := management["allowedCIDRs"]; !exists {
		if legacy, ok := observability["allowedCIDRs"]; ok {
			management["allowedCIDRs"] = legacy
			warnings = append(warnings, "observability.allowedCIDRs migrated to management.allowedCIDRs")
		}
	} else if _, ok := observability["allowedCIDRs"]; ok {
		warnings = append(warnings, "management.allowedCIDRs kept; observability.allowedCIDRs ignored")
	}
	delete(observability, "allowedCIDRs")
	acme := section(v, "acme")
	if _, exists := acme["keyValueEndpointURL"]; !exists {
		if legacy, ok := acme["keyValueBaseURL"]; ok {
			acme["keyValueEndpointURL"] = legacy
			warnings = append(warnings, "acme.keyValueBaseURL migrated to acme.keyValueEndpointURL")
		}
	} else if _, ok := acme["keyValueBaseURL"]; ok {
		warnings = append(warnings, "acme.keyValueEndpointURL kept; acme.keyValueBaseURL ignored")
	}
	delete(acme, "keyValueBaseURL")
	return warnings
}

// Only the retired automatic-upgrade keys are ignored. Keep unknown keys so
// schema validation still catches misspelled manual upgrade settings.
func removeLegacyAutomaticUpgrade(values map[string]any) []string {
	upgrade, ok := values["upgrade"].(map[string]any)
	if !ok {
		return nil
	}
	var warnings []string
	for _, key := range []string{"enabled", "minPublishedAge", "checkInterval"} {
		if _, exists := upgrade[key]; exists {
			delete(upgrade, key)
			warnings = append(warnings, "upgrade."+key+" ignored: automatic upgrades have been removed; use yaginx upgrade manually")
		}
	}
	return warnings
}

func logLegacyWarnings(warnings []string) {
	for _, warning := range warnings {
		slog.Warn("configuration warning", "warning", warning)
	}
}

func defaultCIDRs() []any {
	values := DefaultManagementCIDRs()
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

func applyRuntimeEnvironment(v map[string]any) error {
	for _, e := range []struct{ key, section, field string }{{"YAGINX_NODE_IDENTITY_FILE", "node", "identityFile"}, {"YAGINX_SNAPSHOT_FILE", "node", "snapshotFile"}, {"YAGINX_ADMIN_UI_DIR", "admin", "uiDirectory"}} {
		if x := os.Getenv(e.key); x != "" {
			section(v, e.section)[e.field] = x
		}
	}
	for _, e := range []struct{ key, field string }{{"YAGINX_HTTP_PORT", "http"}, {"YAGINX_PROXY_HTTP_PORT", "proxyHttp"}, {"YAGINX_HTTPS_PORT", "https"}, {"YAGINX_PROXY_HTTPS_PORT", "proxyHttps"}, {"YAGINX_MANAGEMENT_PORT", "management"}} {
		if x := os.Getenv(e.key); x != "" {
			p, err := strconv.Atoi(x)
			if err != nil {
				return fmt.Errorf("%s: %w", e.key, err)
			}
			section(v, "ports")[e.field] = float64(p)
		}
	}
	for _, e := range []struct{ key, section, field string }{{"YAGINX_DATABASE_MAX_CONNECTIONS", "database", "maxConnections"}, {"YAGINX_DATABASE_MIN_CONNECTIONS", "database", "minConnections"}} {
		if x := os.Getenv(e.key); x != "" {
			value, err := strconv.Atoi(x)
			if err != nil {
				return fmt.Errorf("%s: %w", e.key, err)
			}
			section(v, e.section)[e.field] = float64(value)
		}
	}
	for _, e := range []struct{ key, section, field string }{{"YAGINX_HTTP3_ENABLED", "listeners.http3", "enabled"}, {"YAGINX_ACME_ENABLED", "acme", "enabled"}} {
		if x := os.Getenv(e.key); x != "" {
			value, err := strconv.ParseBool(x)
			if err != nil {
				return fmt.Errorf("%s: %w", e.key, err)
			}
			section(v, e.section)[e.field] = value
		}
	}
	if x := os.Getenv("YAGINX_PROXY_TRUSTED_CIDRS"); x != "" {
		values := strings.Split(x, ",")
		cidrs := make([]any, 0, len(values))
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				return fmt.Errorf("YAGINX_PROXY_TRUSTED_CIDRS contains an empty CIDR")
			}
			cidrs = append(cidrs, value)
		}
		section(v, "listeners.proxyProtocol")["trustedCIDRs"] = cidrs
	}
	for _, e := range []struct{ key, section, field string }{{"YAGINX_LOG_LEVEL", "observability", "logLevel"}, {"YAGINX_LOG_FORMAT", "observability", "logFormat"}, {"YAGINX_UPGRADE_MANIFEST_URL", "upgrade", "manifestURL"}, {"YAGINX_GITHUB_MIRROR", "upgrade", "githubMirror"}, {"YAGINX_RELEASE_ASSETS_MIRROR", "upgrade", "releaseAssetsMirror"}} {
		if x := os.Getenv(e.key); x != "" {
			section(v, e.section)[e.field] = x
		}
	}
	return nil
}
func validateRuntime(v map[string]any) error {
	schema, err := compiledRuntimeSchema()
	if err != nil {
		return err
	}
	if err := schema.Validate(schemaValidationValue(v)); err != nil {
		return fmt.Errorf("runtime configuration does not match contract: %w", err)
	}
	allowed := map[string]bool{"configVersion": true, "database": true, "security": true, "node": true, "ports": true, "management": true, "listeners": true, "admin": true, "proxy": true, "ipLookup": true, "configSync": true, "acme": true, "trafficAggregation": true, "requestHistory": true, "resourceReports": true, "observability": true, "upgrade": true}
	for key := range v {
		if !allowed[key] {
			return fmt.Errorf("unknown runtime configuration property %q", key)
		}
	}
	if n, ok := v["configVersion"].(float64); !ok || n != 1 {
		return fmt.Errorf("configVersion must be 1")
	}
	ports := map[float64]bool{}
	for _, name := range []string{"http", "proxyHttp", "https", "proxyHttps", "management"} {
		p, ok := section(v, "ports")[name].(float64)
		if !ok || p < 1 || p > 65535 || ports[p] {
			return fmt.Errorf("ports.%s must be a unique TCP port", name)
		}
		ports[p] = true
	}
	for _, p := range []string{getString(v, "node", "identityFile"), getString(v, "node", "snapshotFile")} {
		if !filepath.IsAbs(p) && !strings.HasPrefix(p, "/") {
			return fmt.Errorf("runtime state paths must be absolute")
		}
	}
	otlp := section(section(v, "observability"), "otlp")
	if enabled, _ := otlp["enabled"].(bool); enabled {
		endpoint, _ := otlp["endpoint"].(string)
		parsed, err := url.ParseRequestURI(endpoint)
		if endpoint == "" || err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("observability.otlp.endpoint must be an absolute http(s) URL when OTLP is enabled")
		}
	}
	keyValueEndpointURL := strings.TrimSpace(getString(v, "acme", "keyValueEndpointURL"))
	if keyValueEndpointURL != "" {
		parsed, err := url.ParseRequestURI(keyValueEndpointURL)
		if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("acme.keyValueEndpointURL must be an absolute http(s) URL")
		}
	}
	return nil
}

// The released binary targets Linux and the contract intentionally requires
// POSIX absolute paths. Windows development tests still need to load a real
// temporary configuration, so validate a path-normalized copy there while the
// original paths remain untouched for filesystem access.
func schemaValidationValue(v map[string]any) map[string]any {
	if runtime.GOOS != "windows" {
		return v
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var copied map[string]any
	if json.Unmarshal(encoded, &copied) != nil {
		return v
	}
	normalizeWindowsPaths(copied)
	return copied
}

func normalizeWindowsPaths(v any) {
	switch value := v.(type) {
	case map[string]any:
		for key, item := range value {
			if text, ok := item.(string); ok && filepath.IsAbs(text) && !strings.HasPrefix(text, "/") {
				value[key] = "/windows/" + strings.ReplaceAll(text, "\\", "/")
				continue
			}
			normalizeWindowsPaths(item)
		}
	case []any:
		for _, item := range value {
			normalizeWindowsPaths(item)
		}
	}
}

func compiledRuntimeSchema() (*jsonschema.Schema, error) {
	runtimeSchemaOnce.Do(func() {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(runtimeConfigSchema))
		if err != nil {
			runtimeSchemaErr = fmt.Errorf("decode embedded runtime schema: %w", err)
			return
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(runtimeSchemaID, document); err != nil {
			runtimeSchemaErr = fmt.Errorf("add embedded runtime schema: %w", err)
			return
		}
		runtimeSchema, runtimeSchemaErr = compiler.Compile(runtimeSchemaID)
		if runtimeSchemaErr != nil {
			runtimeSchemaErr = fmt.Errorf("compile embedded runtime schema: %w", runtimeSchemaErr)
		}
	})
	return runtimeSchema, runtimeSchemaErr
}
func merge(dst, defaults map[string]any) {
	for key, value := range defaults {
		if existing, ok := dst[key]; ok {
			a, aok := existing.(map[string]any)
			b, bok := value.(map[string]any)
			if aok && bok {
				merge(a, b)
			}
		} else {
			dst[key] = value
		}
	}
}
func section(v map[string]any, key string) map[string]any {
	current := v
	for _, part := range strings.Split(key, ".") {
		if x, ok := current[part].(map[string]any); ok {
			current = x
			continue
		}
		x := map[string]any{}
		current[part] = x
		current = x
	}
	return current
}
func getString(v map[string]any, sectionName, key string) string {
	x, _ := section(v, sectionName)[key].(string)
	return x
}
