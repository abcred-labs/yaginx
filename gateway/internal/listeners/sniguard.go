package listeners

import (
	"bytes"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultUnknownSNIThreshold = 20
	defaultUnknownSNIWindow    = time.Minute
	defaultUnknownSNIBlock     = 5 * time.Minute
	// maxTrackedSources bounds memory: internet scanners can use unlimited
	// source addresses, so beyond this many live entries new sources are simply
	// not tracked (they are still rejected, just never blocked early).
	maxTrackedSources = 65536
	summaryInterval   = time.Minute
)

var errBlockedSource = errors.New("blocked source")

type sourceState struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
}

// sourceGuard remembers sources that repeatedly present TLS handshakes the
// gateway has to reject (unknown SNI). Once a source exceeds the threshold in
// the window, its next connections are closed on the first read, before any
// ClientHello parsing or key exchange work. Memory is bounded.
type sourceGuard struct {
	mu        sync.Mutex
	sources   map[netip.Addr]*sourceState
	threshold int
	window    time.Duration
	block     time.Duration
	now       func() time.Time
	// Counters are reset by each periodic summary.
	rejected atomic.Int64
	blocked  atomic.Int64
}

// newSourceGuard returns nil (disabled) when threshold is negative.
func newSourceGuard(threshold int, block time.Duration) *sourceGuard {
	if threshold < 0 {
		return nil
	}
	if threshold == 0 {
		threshold = defaultUnknownSNIThreshold
	}
	if block <= 0 {
		block = defaultUnknownSNIBlock
	}
	return &sourceGuard{sources: make(map[netip.Addr]*sourceState), threshold: threshold, window: defaultUnknownSNIWindow, block: block, now: time.Now}
}

func addrOf(address net.Addr) (netip.Addr, bool) {
	if address == nil {
		return netip.Addr{}, false
	}
	if tcp, ok := address.(*net.TCPAddr); ok {
		ip, ok := netip.AddrFromSlice(tcp.IP)
		return ip.Unmap(), ok
	}
	parsed, err := netip.ParseAddrPort(address.String())
	if err != nil {
		return netip.Addr{}, false
	}
	return parsed.Addr().Unmap(), true
}

func (g *sourceGuard) isBlocked(ip netip.Addr) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	state, ok := g.sources[ip]
	if !ok {
		return false
	}
	now := g.now()
	if now.Before(state.blockedUntil) {
		return true
	}
	if !state.blockedUntil.IsZero() {
		delete(g.sources, ip)
	}
	return false
}

// fail records one rejected handshake and reports whether the source is now blocked.
func (g *sourceGuard) fail(ip netip.Addr) bool {
	if g == nil {
		return false
	}
	g.rejected.Add(1)
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	state, ok := g.sources[ip]
	if !ok {
		if len(g.sources) >= maxTrackedSources {
			g.sweepLocked(now)
			if len(g.sources) >= maxTrackedSources {
				return false
			}
		}
		state = &sourceState{windowStart: now}
		g.sources[ip] = state
	}
	if now.Sub(state.windowStart) > g.window {
		state.failures, state.windowStart = 0, now
	}
	state.failures++
	if state.failures >= g.threshold {
		state.blockedUntil = now.Add(g.block)
		state.failures, state.windowStart = 0, now
		return true
	}
	return false
}

func (g *sourceGuard) sweepLocked(now time.Time) {
	for ip, state := range g.sources {
		if !now.Before(state.blockedUntil) && now.Sub(state.windowStart) > g.window {
			delete(g.sources, ip)
		}
	}
}

func (g *sourceGuard) tracked() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.sources)
}

// summarize logs one aggregate line per interval instead of one line per probe.
func (g *sourceGuard) summarize(logger *slog.Logger, done <-chan struct{}) {
	ticker := time.NewTicker(summaryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			rejected, blocked := g.rejected.Swap(0), g.blocked.Swap(0)
			if rejected > 0 || blocked > 0 {
				logger.Info("rejected TLS probes for unconfigured hosts", "unknown_sni", rejected, "blocked_connections", blocked, "tracked_sources", g.tracked(), "interval", summaryInterval.String())
			}
		}
	}
}

// serverErrorWriter feeds net/http's ErrorLog into slog and drops the
// handshake failures already accounted for by metrics and the periodic summary.
type serverErrorWriter struct{ logger *slog.Logger }

var quietHandshakeMarkers = [][]byte{[]byte("TLS handshake error"), []byte(errUnknownSNIText), []byte(errBlockedSource.Error())}

const errUnknownSNIText = "unknown SNI"

func (w serverErrorWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, quietHandshakeMarkers[0]) && (bytes.Contains(p, quietHandshakeMarkers[1]) || bytes.Contains(p, quietHandshakeMarkers[2])) {
		return len(p), nil
	}
	w.logger.Warn("http server", "message", string(bytes.TrimSpace(p)))
	return len(p), nil
}
