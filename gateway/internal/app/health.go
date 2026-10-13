package app

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// healthState is deliberately small and process-local. It exposes only
// readiness facts already known by serve; it never returns secrets, database
// errors, route data, or certificate identifiers.
type healthState struct {
	version, gitSHA, nodeID string
	mu                      sync.RWMutex
	nodeStatus              string
	databaseRevision        int64
	appliedRevision         int64
	snapshotLoadedAt        time.Time
	listeners               []string
	notify                  string
	ready                   bool
}

func newHealthState(version, gitSHA, nodeID, nodeStatus string) *healthState {
	return &healthState{version: version, gitSHA: gitSHA, nodeID: nodeID, nodeStatus: nodeStatus, notify: "unavailable"}
}

func (h *healthState) setSnapshot(revision int64, at time.Time) {
	h.mu.Lock()
	h.databaseRevision, h.appliedRevision, h.snapshotLoadedAt = revision, revision, at.UTC()
	h.mu.Unlock()
}

func (h *healthState) setNodeStatus(status string) {
	h.mu.Lock()
	h.nodeStatus = status
	h.mu.Unlock()
}

func (h *healthState) setReady(listeners []string) {
	h.mu.Lock()
	h.listeners, h.notify, h.ready = append([]string(nil), listeners...), "listening", true
	h.mu.Unlock()
}

func (h *healthState) setAppliedRevision(revision int64) {
	h.mu.Lock()
	h.appliedRevision = revision
	h.mu.Unlock()
}

func (h *healthState) live(w http.ResponseWriter, _ *http.Request) {
	writeHealth(w, http.StatusOK, map[string]any{"status": "live", "version": h.version, "gitSha": h.gitSHA, "nodeId": h.nodeID, "time": time.Now().UTC().Format(time.RFC3339Nano)})
}

func (h *healthState) readyHandler(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	ready, nodeStatus, notify := h.ready, h.nodeStatus, h.notify
	databaseRevision, appliedRevision, snapshotLoadedAt := h.databaseRevision, h.appliedRevision, h.snapshotLoadedAt
	listeners := append([]string(nil), h.listeners...)
	h.mu.RUnlock()
	reasons := []string{}
	if snapshotLoadedAt.IsZero() {
		reasons = append(reasons, "snapshot_unavailable")
	}
	if !ready {
		reasons = append(reasons, "listeners_unavailable")
	}
	status := http.StatusOK
	if len(reasons) > 0 {
		status = http.StatusServiceUnavailable
	}
	writeHealth(w, status, map[string]any{"status": map[bool]string{true: "ready", false: "not_ready"}[status == http.StatusOK], "ready": status == http.StatusOK, "version": h.version, "nodeId": h.nodeID, "degraded": false, "nodeStatus": nodeStatus, "database": "connected", "notify": notify, "databaseRevision": int64String(databaseRevision), "appliedRevision": int64String(appliedRevision), "snapshotLoadedAt": timeString(snapshotLoadedAt), "listeners": listeners, "reasons": reasons})
}

func writeHealth(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	// The temporary setup page may be served from port 9090 while the selected
	// management listener uses another port. Health is deliberately secret-free,
	// so allow the setup wizard to poll readiness during the restart handoff.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func int64String(value int64) string { return strconv.FormatInt(value, 10) }
func timeString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func managementCIDRGuard(cidrs []string, next http.Handler) (http.Handler, error) {
	return managementCIDRGuardWithLogger(cidrs, next, slog.Default())
}

func managementCIDRGuardWithLogger(cidrs []string, next http.Handler, logger *slog.Logger) (http.Handler, error) {
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, value := range cidrs {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, err
		}
		networks = append(networks, network)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		matchedCIDR := ""
		for index, network := range networks {
			candidate := ip
			// Go represents IPv4 peers accepted by a dual-stack IPv6 socket as
			// IPv4-mapped IPv6 addresses. Normalize them before matching so the
			// decision is independent of the listener's socket family.
			if candidate != nil {
				if ipv4 := candidate.To4(); ipv4 != nil {
					candidate = ipv4
				}
			}
			if candidate != nil && network.Contains(candidate) {
				matchedCIDR = cidrs[index]
				next.ServeHTTP(w, r)
				return
			}
		}
		if logger != nil {
			logger.Warn("management request denied by CIDR policy",
				"remoteAddr", r.RemoteAddr,
				"parsedHost", host,
				"parsedIP", ipString(ip),
				"configuredCIDRs", cidrs,
				"matchedCIDR", matchedCIDR,
				"reason", "client IP is outside configured management networks",
			)
		}
		http.Error(w, "management network access denied", http.StatusForbidden)
	}), nil
}

func ipString(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}
