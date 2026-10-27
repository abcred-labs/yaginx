package proxy

import "strings"

// hostIndex maps every routable hostname of one immutable Snapshot to the site
// (and optional binding) that match() would have picked by scanning the sites
// in order. It is rebuilt only when the snapshot pointer changes, so request
// and TLS handshake lookups are a single map access with no allocation.
type hostIndex struct {
	snapshot *Snapshot
	hosts    map[string]hostRef
}

type hostRef struct {
	site    int
	binding int // -1 when the host comes from Site.Hosts
}

func buildHostIndex(snapshot *Snapshot) *hostIndex {
	index := &hostIndex{snapshot: snapshot, hosts: make(map[string]hostRef)}
	for i := range snapshot.Sites {
		site := &snapshot.Sites[i]
		for j := range site.HostBindings {
			binding := &site.HostBindings[j]
			if !binding.Enabled {
				continue
			}
			key := normalizedHost(binding.Hostname)
			if _, exists := index.hosts[key]; !exists {
				index.hosts[key] = hostRef{site: i, binding: j}
			}
		}
		for _, candidate := range site.Hosts {
			key := strings.ToLower(strings.TrimSuffix(candidate, "."))
			if _, exists := index.hosts[key]; !exists {
				index.hosts[key] = hostRef{site: i, binding: -1}
			}
		}
	}
	return index
}

// hostIndexFor returns the index of the current snapshot, rebuilding it when a
// new snapshot has been stored. Concurrent rebuilds are idempotent.
func (r *Runtime) hostIndexFor() *hostIndex {
	snapshot := r.snapshot.Load()
	if index := r.hostIdx.Load(); index != nil && index.snapshot == snapshot {
		return index
	}
	index := buildHostIndex(snapshot)
	r.hostIdx.Store(index)
	return index
}

// HasHost reports whether any enabled website binds the already normalized
// hostname. TLS uses it to reject scanner SNI values before touching the
// certificate store.
func (r *Runtime) HasHost(normalized string) bool {
	_, ok := r.hostIndexFor().hosts[normalized]
	return ok
}
