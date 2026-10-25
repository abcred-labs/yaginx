package repository

import (
	"sort"
	"testing"
	"time"

	"yaginx-gateway/internal/management"
)

func TestNormalizeResourceHost(t *testing.T) {
	for _, item := range []struct{ raw, want string }{
		{" Example.TEST:443 ", "example.test"},
		{"example.test.", "example.test"},
		{"[2001:db8::1]:8443", "2001:db8::1"},
		{"__unmatched__", ""},
		{"", ""},
	} {
		if got := normalizeResourceHost(item.raw); got != item.want {
			t.Fatalf("normalizeResourceHost(%q) = %q, want %q", item.raw, got, item.want)
		}
	}
}

func TestNormalizedWebsiteHostsDeduplicatesConfiguration(t *testing.T) {
	defaultHost := "API.EXAMPLE.TEST:443"
	website := management.Website{
		WebsiteUpsertRequest: management.WebsiteUpsertRequest{
			DefaultHost: &defaultHost,
			Hosts:       []string{"api.example.test", "www.example.test.", "__unmatched__"},
		},
	}
	got := normalizedWebsiteHosts(website)
	want := []string{"api.example.test", "www.example.test"}
	if len(got) != len(want) {
		t.Fatalf("hosts = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("hosts = %#v, want %#v", got, want)
		}
	}
}

func TestResourceCatalogFilters(t *testing.T) {
	items := map[string]*resourceHost{
		"api.example.test":      {name: "api.example.test", managed: true, websites: []resourceWebsiteRef{{ID: 1}}},
		"certificate.example":   {name: "certificate.example", managed: true, websites: []resourceWebsiteRef{}},
		"observed.example.test": {name: "observed.example.test", websites: []resourceWebsiteRef{}},
	}
	for _, test := range []struct {
		name  string
		input management.ResourceCatalogQuery
		want  []string
	}{
		{name: "host fuzzy matching is case insensitive", input: management.ResourceCatalogQuery{Host: " API.ExAmPlE "}, want: []string{"api.example.test"}},
		{name: "empty query returns every host", input: management.ResourceCatalogQuery{}, want: []string{"api.example.test", "certificate.example", "observed.example.test"}},
		{name: "managed status", input: management.ResourceCatalogQuery{Status: "managed"}, want: []string{"api.example.test", "certificate.example"}},
		{name: "unmanaged status", input: management.ResourceCatalogQuery{Status: "unmanaged"}, want: []string{"observed.example.test"}},
		{name: "associated websites", input: management.ResourceCatalogQuery{WebsiteAssociation: "associated"}, want: []string{"api.example.test"}},
		{name: "unassociated websites", input: management.ResourceCatalogQuery{WebsiteAssociation: "unassociated"}, want: []string{"certificate.example", "observed.example.test"}},
		{name: "combined filters", input: management.ResourceCatalogQuery{Host: "example", Status: "managed", WebsiteAssociation: "unassociated"}, want: []string{"certificate.example"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			filter := resourceCatalogFilterFromQuery(test.input)
			got := []string{}
			for _, item := range items {
				if resourceHostMatchesCatalogFilter(item, filter) {
					got = append(got, item.name)
				}
			}
			sort.Strings(got)
			if len(got) != len(test.want) {
				t.Fatalf("matches = %#v, want %#v", got, test.want)
			}
			for index := range test.want {
				if got[index] != test.want[index] {
					t.Fatalf("matches = %#v, want %#v", got, test.want)
				}
			}
		})
	}
}

func TestResourceTimelineHasZeroValuedPointsForAnEmptyWindow(t *testing.T) {
	window := analyticsWindowValue{begin: time.Unix(0, 0).UTC(), end: time.Unix(120, 0).UTC(), step: time.Minute}
	points := denseAnalyticsPoints(nil, window)
	if len(points) != 3 {
		t.Fatalf("points = %#v", points)
	}
	for _, point := range points {
		if point["requests"] != int64(0) || point["errorRate"] != float64(0) {
			t.Fatalf("unexpected empty point %#v", point)
		}
	}
}

func TestResourceAutoRenewStatus(t *testing.T) {
	if resourceAutoRenew(&resourceHost{}) != nil {
		t.Fatal("host without a web_domain row must have no auto-renew state")
	}
	host := &resourceHost{domainConfig: &management.DomainListItem{}}
	if got := resourceAutoRenew(host); got.Status != "off" || got.MaxPerDay != ACMEAutoMaxAttemptsPerDay {
		t.Fatalf("disabled host = %+v", got)
	}
	host.domainConfig.AutoRequestCertificate = true
	if got := resourceAutoRenew(host); got.Status != "active" {
		t.Fatalf("enabled host = %+v", got)
	}
	stopped := time.Now()
	host.autoSummary = &ACMEAutoSummary{FailDays: 3, StoppedAt: &stopped, StopReason: "x", AttemptsToday: 2, LastOrder: &ACMELatestOrder{ID: 7, Status: "failed", UpdatedAt: stopped}}
	got := resourceAutoRenew(host)
	if got.Status != "stopped" || got.FailDays != 3 || got.AttemptsToday != 2 || got.LatestOrder == nil || got.LatestOrder.ID != 7 {
		t.Fatalf("stopped host = %+v", got)
	}
	// Stop state is irrelevant once the operator turned auto-request off.
	host.domainConfig.AutoRequestCertificate = false
	if got := resourceAutoRenew(host); got.Status != "off" {
		t.Fatalf("disabled-after-stop host = %+v", got)
	}
}
