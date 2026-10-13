package acme

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/challenge"
)

// ValidateRemoteEndpoint verifies that a configured endpoint is another node
// in this Yaginx cluster before challenge traffic is sent to it.
func ValidateRemoteEndpoint(ctx context.Context, baseURL, expectedClusterID, currentNodeID string, client *http.Client) error {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return fmt.Errorf("invalid key-value endpoint URL")
	}
	identityURL := *base
	identityURL.Path = strings.TrimRight(base.Path, "/") + "/api/identity"
	identityURL.RawQuery = ""
	identityURL.Fragment = ""
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, identityURL.String(), nil)
	if err != nil {
		return fmt.Errorf("build endpoint identity request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("probe key-value endpoint identity: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("key-value endpoint identity returned HTTP %d", response.StatusCode)
	}
	var value struct {
		Service   string `json:"service"`
		ClusterID string `json:"clusterId"`
		NodeID    string `json:"nodeId"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&value); err != nil {
		return fmt.Errorf("decode key-value endpoint identity: %w", err)
	}
	if value.Service != "yaginx" || value.ClusterID == "" || value.ClusterID != expectedClusterID {
		return fmt.Errorf("key-value endpoint is not in the current Yaginx cluster")
	}
	if value.NodeID == currentNodeID {
		return fmt.Errorf("key-value endpoint points to the current node")
	}
	return nil
}

// KeyValueProvider is lego's HTTP-01 provider backed by the existing key-value
// service. It intentionally avoids redirects and logs neither token values nor
// key authorizations.
type KeyValueProvider struct {
	BaseURL, SetPath, GetPath string
	Client                    *http.Client
}

var _ challenge.Provider = KeyValueProvider{}

func (p KeyValueProvider) Present(_ string, token, keyAuthorization string) error {
	if !validToken(token) || keyAuthorization == "" {
		return fmt.Errorf("invalid HTTP-01 material")
	}
	_, err := p.call(p.SetPath, token, keyAuthorization, false)
	return err
}
func (p KeyValueProvider) CleanUp(_ string, _, _ string) error { return nil }
func (p KeyValueProvider) Resolve(token string) (string, error) {
	if !validToken(token) {
		return "", fmt.Errorf("invalid HTTP-01 token")
	}
	return p.call(p.GetPath, token, "", true)
}
func (p KeyValueProvider) call(path, token, value string, requireData bool) (string, error) {
	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return "", err
	}
	endpoint, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	target := base.ResolveReference(endpoint)
	query := target.Query()
	query.Set("key", token)
	if value != "" {
		query.Set("value", value)
	}
	target.RawQuery = query.Encode()
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Get(target.String())
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("key-value status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return "", err
	}
	var envelope struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("decode key-value response: %w", err)
	}
	if envelope.Code != 200 || (requireData && strings.TrimSpace(envelope.Data) == "") {
		return "", fmt.Errorf("invalid key-value response")
	}
	return envelope.Data, nil
}
func validToken(token string) bool {
	if len(token) == 0 || len(token) > 256 {
		return false
	}
	for _, r := range token {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
