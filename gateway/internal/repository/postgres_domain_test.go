package repository

import (
	"testing"
	"time"

	"yaginx-gateway/internal/management"
)

func TestValidateWebsiteIPAccess(t *testing.T) {
	for _, mode := range []string{"", "disabled", "blacklist", "whitelist"} {
		if err := validateWebsiteIPAccess(mode, []string{"203.0.113.9", "2001:db8::/32"}); err != nil {
			t.Fatalf("mode %q rejected: %v", mode, err)
		}
	}
	for _, value := range []struct {
		mode string
		cidr string
	}{
		{mode: "unknown", cidr: "203.0.113.9"},
		{mode: "blacklist", cidr: "not-an-ip"},
	} {
		if err := validateWebsiteIPAccess(value.mode, []string{value.cidr}); err == nil {
			t.Fatalf("invalid access policy accepted: %#v", value)
		}
	}
}

func TestDecodeProxySiteIPAccess(t *testing.T) {
	site, err := decodeProxySite(42, "site", []byte(`{"defaultHost":"site.test","ipAccessMode":"whitelist","ipAccessCIDRs":["198.51.100.9"]}`), []byte(`[]`), []byte(`[]`), []byte(`{}`), []byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if site.IPAccessMode != "whitelist" || len(site.IPAccessCIDRs) != 1 || site.IPAccessCIDRs[0] != "198.51.100.9" {
		t.Fatalf("site IP access=%#v", site)
	}
}

func TestNormalizeWebsiteDomainValues(t *testing.T) {
	defaultHost := "API.EXAMPLE.TEST:443"
	value := normalizeWebsiteDomainValues(management.WebsiteUpsertRequest{
		DefaultHost: &defaultHost,
		Hosts:       []string{"api.example.test", "WWW.EXAMPLE.TEST.", "www.example.test:443", "__unmatched__"},
	})
	if value.DefaultHost == nil || *value.DefaultHost != "api.example.test" {
		t.Fatalf("defaultHost = %#v", value.DefaultHost)
	}
	if got, want := value.Hosts, []string{"api.example.test", "www.example.test"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("hosts = %#v, want %#v", got, want)
	}
}

func TestDomainResponseMapsUseStringIDsAndCertificateSpecs(t *testing.T) {
	const id int64 = 9007199254740993
	fingerprint := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	notBefore := time.Unix(1_700_000_000, 0).UTC()
	notAfter := notBefore.Add(24 * time.Hour)

	response := domainResponseMap(id, "example.invalid", false, true, "")
	if response["id"] != "9007199254740993" {
		t.Fatalf("response id=%#v", response["id"])
	}
	if _, exists := response["certSpec"]; exists {
		t.Fatalf("upsert response must not include certSpec: %#v", response)
	}

	active := domainListItemMap(id, "example.invalid", false, true, "", &fingerprint, &notBefore, &notAfter)
	activeSpec := active["certSpec"].(map[string]any)
	if active["id"] != "9007199254740993" || activeSpec["hasCertificate"] != true || activeSpec["thumbprint"] != fingerprint || activeSpec["notBefore"] != notBefore.Unix() || activeSpec["notAfter"] != notAfter.Unix() {
		t.Fatalf("active domain item=%#v", active)
	}

	missing := domainListItemMap(id, "example.invalid", false, true, "", nil, nil, nil)
	missingSpec := missing["certSpec"].(map[string]any)
	if missingSpec["hasCertificate"] != false || missingSpec["thumbprint"] != nil || missingSpec["notBefore"] != int64(-62135596800) || missingSpec["notAfter"] != int64(-62135596800) {
		t.Fatalf("missing domain item=%#v", missing)
	}
}
