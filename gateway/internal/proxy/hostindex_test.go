package proxy

import "testing"

func TestHostIndexMatchesFirstSiteBindingThenHosts(t *testing.T) {
	snapshot := &Snapshot{Sites: []Site{
		{ID: 1, Hosts: []string{"A.example.com."}, HostBindings: []HostBinding{{Hostname: "b.example.com:443", Enabled: true}, {Hostname: "off.example.com", Enabled: false}}},
		{ID: 2, Hosts: []string{"a.example.com", "off.example.com"}, HostBindings: []HostBinding{{Hostname: "b.example.com", Enabled: true}}},
	}}
	r := New(snapshot)
	for host, want := range map[string]int64{"a.example.com": 1, "A.EXAMPLE.com:8443": 1, "b.example.com": 1, "off.example.com": 2} {
		site, ok, _ := r.match(host)
		if !ok || site.ID != want {
			t.Fatalf("match(%q) = %d,%v want %d", host, site.ID, ok, want)
		}
	}
	if _, binding := (func() (bool, *HostBinding) { _, ok, b := r.match("b.example.com"); return ok, b })(); binding == nil {
		t.Fatal("expected binding for b.example.com")
	}
	if _, ok, _ := r.match("portal.nextep-smart-order.feinian.net"); ok || r.HasHost("portal.nextep-smart-order.feinian.net") {
		t.Fatal("unknown host matched")
	}
}

func TestHostIndexFollowsReplace(t *testing.T) {
	r := New(&Snapshot{Sites: []Site{{Hosts: []string{"one.example.com"}}}})
	if !r.HasHost("one.example.com") || r.HasHost("two.example.com") {
		t.Fatal("initial index wrong")
	}
	r.Replace(&Snapshot{Sites: []Site{{Hosts: []string{"two.example.com"}}}})
	if r.HasHost("one.example.com") || !r.HasHost("two.example.com") {
		t.Fatal("index not rebuilt after Replace")
	}
}

func BenchmarkHasHostUnknown(b *testing.B) {
	sites := make([]Site, 2000)
	for i := range sites {
		sites[i].Hosts = []string{"h" + string(rune('a'+i%26)) + ".example.com"}
	}
	r := New(&Snapshot{Sites: sites})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.HasHost("portal.nextep-smart-order.feinian.net")
	}
}
