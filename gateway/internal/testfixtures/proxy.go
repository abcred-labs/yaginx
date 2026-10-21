package testfixtures

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// ProxyFixture is one data-plane case: listener, runtime config, client
// request/response, upstream observation and request-history classification.
type ProxyFixture struct {
	Version    int                      `json:"version"`
	MatrixLine int                      `json:"matrixLine"`
	Name       string                   `json:"name"`
	Source     string                   `json:"source"`
	ApprovalID string                   `json:"approvalId,omitempty"`
	Listener   ProxyListener            `json:"listener"`
	Config     json.RawMessage          `json:"config"`
	Client     ProxyClientObservation   `json:"client"`
	Upstream   ProxyUpstreamObservation `json:"upstream"`
	History    ProxyHistoryObservation  `json:"history"`
}

// ProxyListener identifies the wire listener exercised by a fixture case.
// It deliberately records the transport separately from the port because the
// HTTPS listener has both TCP and UDP/QUIC contract rows.
type ProxyListener struct {
	Port      int    `json:"port"`
	Transport string `json:"transport"`
}

// ProxyClientObservation is the request sent to the listener and the
// response returned to its caller. Bodies are stored as UTF-8 test payloads,
// never captured production traffic.
type ProxyClientObservation struct {
	Method     string            `json:"method"`
	Host       string            `json:"host"`
	Path       string            `json:"path"`
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers,omitempty"`
	Trailers   map[string]string `json:"trailers,omitempty"`
	Body       json.RawMessage   `json:"body,omitempty"`
	BodyBytes  int64             `json:"bodyBytes,omitempty"`
	BodySHA256 string            `json:"bodySha256,omitempty"`
}

// ProxyUpstreamObservation records whether an upstream was contacted and, if
// so, the request/result observed by the isolated test upstream. A direct
// simple-response fixture has Attempted=false and no upstream method/path.
type ProxyUpstreamObservation struct {
	Attempted  bool              `json:"attempted"`
	Method     string            `json:"method,omitempty"`
	Host       string            `json:"host,omitempty"`
	Path       string            `json:"path,omitempty"`
	Status     int               `json:"status,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	BodyBytes  int64             `json:"bodyBytes,omitempty"`
	BodySHA256 string            `json:"bodySha256,omitempty"`
}

// ProxyHistoryObservation is the expected request-history classification for
// the fixture input.
type ProxyHistoryObservation struct {
	Status        int    `json:"status"`
	StatusSource  string `json:"statusSource"`
	ErrorCategory string `json:"errorCategory"`
}

func loadProxy(fsys fs.FS, directory string) ([]ProxyFixture, error) {
	entries, err := fs.ReadDir(fsys, directory)
	if err != nil {
		return nil, err
	}
	fixtures := make([]ProxyFixture, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".json" || strings.HasSuffix(entry.Name(), ".template.json") {
			continue
		}
		contents, err := fs.ReadFile(fsys, path.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		var fixture ProxyFixture
		if err := json.Unmarshal(contents, &fixture); err != nil {
			return nil, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		if err := fixture.Validate(); err != nil {
			return nil, fmt.Errorf("validate %s: %w", entry.Name(), err)
		}
		fixtures = append(fixtures, fixture)
	}
	return fixtures, nil
}

// Validate rejects metadata-only proxy entries. Every row must retain the
// input listener/config plus client, isolated-upstream, and the expected
// history classification the Go runtime is checked against.
func (fixture ProxyFixture) Validate() error {
	if fixture.Version != 1 || fixture.MatrixLine < 1 || fixture.Name == "" || (fixture.Source != "captured" && fixture.Source != "contract") {
		return fmt.Errorf("invalid proxy fixture metadata")
	}
	if fixture.Source == "captured" && fixture.ApprovalID != "" {
		return fmt.Errorf("captured proxy fixture must not carry approvalId")
	}
	if fixture.Source == "contract" && fixture.ApprovalID == "" {
		return fmt.Errorf("contract proxy fixture requires approvalId")
	}
	if fixture.Listener.Port < 1 || fixture.Listener.Port > 65535 {
		return fmt.Errorf("listener port is invalid")
	}
	if fixture.Listener.Transport != "tcp" && fixture.Listener.Transport != "udp" {
		return fmt.Errorf("listener transport must be tcp or udp")
	}
	if len(fixture.Config) == 0 || !json.Valid(fixture.Config) {
		return fmt.Errorf("config must be JSON")
	}
	var config any
	if err := json.Unmarshal(fixture.Config, &config); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if err := rejectSensitiveValue(config); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if fixture.Client.Method == "" || fixture.Client.Host == "" || !strings.HasPrefix(fixture.Client.Path, "/") || fixture.Client.Status < 100 || fixture.Client.Status > 610 {
		return fmt.Errorf("client observation is incomplete")
	}
	if err := validateBodySummary(fixture.Client.BodyBytes, fixture.Client.BodySHA256); err != nil {
		return fmt.Errorf("client body summary: %w", err)
	}
	if fixture.Upstream.Attempted {
		if fixture.Upstream.Method == "" || fixture.Upstream.Host == "" || !strings.HasPrefix(fixture.Upstream.Path, "/") || fixture.Upstream.Status < 100 || fixture.Upstream.Status > 599 {
			return fmt.Errorf("attempted upstream observation is incomplete")
		}
		if err := validateBodySummary(fixture.Upstream.BodyBytes, fixture.Upstream.BodySHA256); err != nil {
			return fmt.Errorf("upstream body summary: %w", err)
		}
	} else if fixture.Upstream.Method != "" || fixture.Upstream.Host != "" || fixture.Upstream.Path != "" || fixture.Upstream.Status != 0 {
		return fmt.Errorf("unattempted upstream must not contain request data")
	}
	if fixture.History.Status < 100 || fixture.History.Status > 610 || fixture.History.StatusSource == "" {
		return fmt.Errorf("history observation is incomplete")
	}
	for location, headers := range map[string]map[string]string{"client headers": fixture.Client.Headers, "client trailers": fixture.Client.Trailers, "upstream headers": fixture.Upstream.Headers} {
		for name, value := range headers {
			if isSensitiveKey(name) && !strings.HasPrefix(value, "${") {
				return fmt.Errorf("%s contain unredacted %q", location, name)
			}
		}
	}
	for location, body := range map[string]json.RawMessage{"client body": fixture.Client.Body} {
		if len(body) == 0 || !json.Valid(body) {
			continue
		}
		var value any
		if err := json.Unmarshal(body, &value); err != nil {
			return fmt.Errorf("parse %s: %w", location, err)
		}
		if err := rejectSensitiveValue(value); err != nil {
			return fmt.Errorf("%s: %w", location, err)
		}
	}
	return nil
}

func validateBodySummary(bytes int64, hash string) error {
	if bytes < 0 {
		return fmt.Errorf("byte count is negative")
	}
	if hash == "" {
		return nil
	}
	if bytes == 0 || len(hash) != 64 {
		return fmt.Errorf("hash requires a non-zero byte count and 64 hex characters")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return fmt.Errorf("hash is not hexadecimal: %w", err)
	}
	return nil
}
