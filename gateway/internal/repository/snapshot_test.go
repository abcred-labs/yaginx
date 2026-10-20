package repository

import (
	"encoding/json"
	"testing"
	"time"

	"yaginx-gateway/internal/proxy"
)

func TestValidateSnapshotTransformsRejectsUnresolvedRouteValues(t *testing.T) {
	for _, raw := range []string{
		`{"PathPattern":"/mapped/{**remainder}"}`,
		`{"QueryRouteParameter":"route","Set":"remainder"}`,
	} {
		if err := validateSnapshotTransforms([]byte(raw)); err == nil {
			t.Fatalf("unsafe transform accepted: %s", raw)
		}
	}
	if err := validateSnapshotTransforms([]byte(`{"PathPrefix":"/safe","RequestHeadersCopy":"false"}`)); err != nil {
		t.Fatalf("safe transform rejected: %v", err)
	}
}

func TestValidateSnapshotTransformsAcceptsEditorKeyValueArray(t *testing.T) {
	if err := validateSnapshotTransforms([]byte(`[{"key":"PathPrefix","value":"/safe"},{"key":"RequestHeadersCopy","value":"false"}]`)); err != nil {
		t.Fatalf("editor transform array rejected: %v", err)
	}
	if err := validateSnapshotTransforms([]byte(`[{"key":"PathPattern","value":"/mapped/{**remainder}"}]`)); err == nil {
		t.Fatal("unsafe editor transform array accepted")
	}
}

func TestSimpleResponseHeadersPreserveLegacyExtraHeaders(t *testing.T) {
	var value map[string]any
	if err := json.Unmarshal([]byte(`{"extraHeaders":[{"key":"X-Fixture-Header","value":"fixture-value"}]}`), &value); err != nil {
		t.Fatal(err)
	}
	headers, err := simpleResponseHeaders(value)
	if err != nil || headers["X-Fixture-Header"] != "fixture-value" {
		t.Fatalf("headers=%#v err=%v", headers, err)
	}
	value["extraHeaders"] = []any{map[string]any{"key": "X-Bad\nHeader", "value": "value"}}
	if _, err := simpleResponseHeaders(value); err == nil {
		t.Fatal("header injection accepted")
	}
}

func TestLegacySimpleResponsesAreRejected(t *testing.T) {
	_, err := decodeProxySite(42, "legacy-site", []byte(`{}`), []byte(`[]`), []byte(`[]`), []byte(`{}`), []byte(`[
        {
            "Url": "MP_verify.txt",
            "StatusCode": 218,
            "ContentType": "text/plain",
            "ResponseContent": "verified",
            "ExtraHeaders": [{"Key": "X-Verification", "Value": "legacy"}]
        },
        {
            "path": "/canonical",
            "Url": "/legacy",
            "statusCode": 219,
            "StatusCode": 500,
            "contentType": "application/json",
            "ContentType": "text/plain",
            "responseContent": "canonical",
            "ResponseContent": "legacy",
            "extraHeaders": [{"key": "X-Source", "value": "canonical"}],
            "ExtraHeaders": [{"Key": "X-Source", "Value": "legacy"}]
        }
    ]`))
	if err == nil {
		t.Fatal("legacy simple response was accepted")
	}
}

func TestLegacyWebsiteSpecificationsAreReadable(t *testing.T) {
	legacy := []byte(`{
        "DefaultHost":"legacy.example.invalid",
        "DefaultDestination":"http://127.0.0.1:10030",
        "DefaultDestinationHost":"upstream.example.invalid",
        "WebProxy":"http://proxy.example.invalid:8080",
        "AuthorizationPolicy":"Anonymous",
        "IsWithOriginalHostHeader":true,
        "IsAllowUnsafeSslCertificate":true,
        "IsAutoRedirectHttp2Https":true
    }`)

	if _, err := websiteMap(42, "legacy-site", legacy, true); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeProxySite(42, "legacy-site", legacy, []byte(`[]`), []byte(`[]`), []byte(`{}`), []byte(`[]`)); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyDefaultDestinationBecomesFallbackRuleDuringMigration(t *testing.T) {
	site, err := decodeProxySite(42, "legacy-site", []byte(`{
        "defaultDestination":"http://upstream.example.invalid",
        "defaultDestinationHost":"upstream.example.invalid"
    }`), []byte(`["legacy.example.invalid"]`), []byte(`[]`), []byte(`{}`), []byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if site.Destination != "" || site.DestinationHost != "" {
		t.Fatalf("legacy destination fields should be migrated out of the snapshot: %#v", site)
	}
	pool, ok := site.UpstreamPools["legacy-default"]
	if !ok || len(pool.Endpoints) != 1 || pool.Endpoints[0].Address != "http://upstream.example.invalid" || pool.Endpoints[0].Host != "upstream.example.invalid" {
		t.Fatalf("legacy default upstream pool = %#v", site.UpstreamPools)
	}
	if len(site.Rules) != 1 || !site.Rules[0].Enabled || !site.Rules[0].IsFallback || site.Rules[0].UpstreamPoolID != "legacy-default" {
		t.Fatalf("legacy default fallback rule = %#v", site.Rules)
	}
}

func TestLegacyDefaultHostBecomesPrimaryDuringMigration(t *testing.T) {
	site, err := decodeProxySite(42, "legacy-site", []byte(`{"defaultHost":"www.example.invalid"}`), []byte(`["alias.example.invalid","www.example.invalid"]`), []byte(`[]`), []byte(`{}`), []byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if site.RedirectHost != "www.example.invalid" {
		t.Fatalf("redirect host = %q", site.RedirectHost)
	}
	if len(site.HostBindings) != 2 || site.HostBindings[0].Hostname != "www.example.invalid" || site.HostBindings[0].Role != "primary" || site.HostBindings[1].Role != "alias" {
		t.Fatalf("host bindings = %#v", site.HostBindings)
	}
}

func TestLegacyHostsUseFirstHostAsPrimaryWithoutDefaultHost(t *testing.T) {
	site, err := decodeProxySite(42, "legacy-site", []byte(`{}`), []byte(`["primary.example.invalid","alias.example.invalid"]`), []byte(`[]`), []byte(`{}`), []byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if site.RedirectHost != "primary.example.invalid" || len(site.HostBindings) != 2 || site.HostBindings[0].Role != "primary" {
		t.Fatalf("site = %#v", site)
	}
}

func TestWebsiteMaintenanceModeFlowsToManagementAndProxySnapshots(t *testing.T) {
	specifications := []byte(`{"defaultHost":"maintenance.example.invalid","isMaintenanceMode":true}`)
	response, err := websiteMap(42, "maintenance-site", specifications, false)
	if err != nil {
		t.Fatal(err)
	}
	if response["isMaintenanceMode"] != true {
		t.Fatalf("management response=%#v", response)
	}
	site, err := decodeProxySite(42, "maintenance-site", specifications, []byte(`[]`), []byte(`[]`), []byte(`{}`), []byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if !site.MaintenanceMode {
		t.Fatalf("proxy site=%#v", site)
	}
}

func TestWebsiteRequestHistoryPolicyDefaultsToInheritAndFlowsToSnapshot(t *testing.T) {
	site, err := decodeProxySite(42, "history-site", []byte(`{
        "defaultHost":"history.example.invalid",
        "requestHistory":{"mode":"all","successSamplingRate":0.25}
    }`), []byte(`[]`), []byte(`[]`), []byte(`{}`), []byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if site.RequestHistoryMode != "all" || site.RequestHistorySuccessSamplingRate == nil || *site.RequestHistorySuccessSamplingRate != 0.25 {
		t.Fatalf("site history policy=%#v", site)
	}
	legacy, err := decodeProxySite(43, "legacy-history-site", []byte(`{"defaultHost":"legacy-history.example.invalid"}`), []byte(`[]`), []byte(`[]`), []byte(`{}`), []byte(`[]`))
	if err != nil || legacy.RequestHistoryMode != "inherit" || legacy.RequestHistorySuccessSamplingRate != nil {
		t.Fatalf("legacy site history policy=%#v err=%v", legacy, err)
	}
}

func TestWebsiteRequestHistoryPolicyRejectsInvalidValues(t *testing.T) {
	for _, raw := range []string{
		`{"defaultHost":"invalid.example.invalid","requestHistory":{"mode":"unknown"}}`,
		`{"defaultHost":"invalid.example.invalid","requestHistory":{"mode":"errorsOnly","successSamplingRate":0.5}}`,
		`{"defaultHost":"invalid.example.invalid","requestHistory":{"mode":"all","successSamplingRate":1.5}}`,
	} {
		if _, err := decodeProxySite(42, "invalid-history-site", []byte(raw), []byte(`[]`), []byte(`[]`), []byte(`{}`), []byte(`[]`)); err == nil {
			t.Fatalf("invalid request history policy accepted: %s", raw)
		}
	}
}

func TestWebsiteSpecificationNormalizesMixedLegacyAliases(t *testing.T) {
	value, err := decodeWebsiteSpecifications([]byte(`{
        "defaultHost":"current.example.invalid",
        "DefaultHost":"legacy.example.invalid",
        "redirectHttpToHttps":false,
        "IsAutoRedirectHttp2Https":true
    }`))
	if err != nil || value["defaultHost"] != "current.example.invalid" || value["redirectHttpToHttps"] != false {
		t.Fatalf("value=%#v err=%v", value, err)
	}
}

func TestEmptyWebsiteSpecificationsRemainReadable(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte{}, []byte("  \n\t"), []byte("null")} {
		specifications, err := decodeWebsiteSpecifications(raw)
		if err != nil {
			t.Fatalf("decode %q: %v", raw, err)
		}
		if len(specifications) != 0 {
			t.Fatalf("decode %q=%#v", raw, specifications)
		}
		site, err := decodeProxySite(42, "empty-site", raw, []byte(`["empty.example.invalid"]`), []byte(`[]`), []byte(`{}`), []byte(`[]`))
		if err != nil || len(site.Hosts) != 1 || site.Hosts[0] != "empty.example.invalid" {
			t.Fatalf("site for %q=%#v err=%v", raw, site, err)
		}
	}
}

func TestProxySnapshotReadsLegacyCollectionsInSpecifications(t *testing.T) {
	_, err := decodeProxySite(42, "legacy-site", []byte(`{
        "Hosts":["legacy.example.invalid"],
        "ProxyRules":[{
            "PathPattern":"/api",
            "Destination":"https://origin.example.invalid",
            "WebProxy":"http://proxy.example.invalid:8080",
            "IsEnabled":true,
            "IsWithOriginalHostHeader":true,
            "IsAllowUnsafeSslCertificate":true
        }],
        "ProxyTransforms":{"PathPrefix":"/safe"},
        "SimpleResponses":[{
            "Url":"/healthz",
            "StatusCode":218,
            "ContentType":"text/plain",
            "ResponseContent":"healthy",
            "ExtraHeaders":[{"Key":"X-Legacy","Value":"present"}]
        }]
    }`), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWebsiteEditorReadsDedicatedLegacyCollections(t *testing.T) {
	value, err := websiteMapWithCollections(
		42,
		"legacy-site",
		[]byte(`{"DefaultHost":"legacy.example.invalid","Hosts":["stale.example.invalid"]}`),
		[]byte(`["app.example.invalid"]`),
		[]byte(`[{"Name":"legacy-rule","PathPattern":"/api","Destination":"http://127.0.0.1:5000","IsEnabled":true,"ExtraHeaders":[{"Key":"X-Route","Value":"legacy"}]}]`),
		[]byte(`{"PathPrefix":"/safe"}`),
		[]byte(`[{"Url":"healthz","StatusCode":218,"ContentType":"text/plain","ResponseContent":"ok","ExtraHeaders":[{"Key":"X-Response","Value":"legacy"}]}]`),
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := websiteFromMap(value); err != nil {
		t.Fatal(err)
	}
}

func TestWebsiteEditorNormalizesEmptyTransformObject(t *testing.T) {
	value, err := websiteMapWithCollections(
		42,
		"v2-site",
		[]byte(`{"defaultHost":"v2.example.invalid"}`),
		[]byte(`[]`),
		[]byte(`[]`),
		[]byte(`{}`),
		[]byte(`[]`),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	website, err := websiteFromMap(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(website.ProxyTransforms) != 0 {
		t.Fatalf("proxy transforms=%#v", website.ProxyTransforms)
	}
}

func TestWebsiteEditorReadsCollectionsEmbeddedInLegacySpecifications(t *testing.T) {
	_, err := websiteMap(42, "legacy-site", []byte(`{
        "Hosts":["legacy.example.invalid"],
        "ProxyRules":[{"PathPattern":"/legacy","IsEnabled":true}],
        "SimpleResponses":[{"Url":"/healthz","StatusCode":200}]
    }`), false)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDecodeProxySiteResponseHeaderTimeout(t *testing.T) {
	decode := func(raw string) proxy.Site {
		t.Helper()
		site, err := decodeProxySite(7, "site", []byte(`{"defaultHost":"site.test","responseHeaderTimeout":`+raw+`}`), []byte(`[]`), []byte(`[]`), []byte(`{}`), []byte(`[]`))
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		return site
	}
	if got := decode(`"600s"`).ResponseHeaderTimeout; got != 600*time.Second {
		t.Fatalf("valid override=%v", got)
	}
	// An invalid stored value must degrade to the global timeout instead of
	// failing the whole snapshot.
	for _, raw := range []string{`"nonsense"`, `"0s"`, `"-5s"`, `"2h"`} {
		if got := decode(raw).ResponseHeaderTimeout; got != 0 {
			t.Fatalf("%s should inherit the global timeout, got %v", raw, got)
		}
	}
}
