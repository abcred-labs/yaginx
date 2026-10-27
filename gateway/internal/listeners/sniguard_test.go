package listeners

import (
	"bytes"
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"yaginx-gateway/internal/certificates"
)

func TestUnknownSNIMarkerMatchesCertificateError(t *testing.T) {
	if certificates.ErrUnknownSNI.Error() != errUnknownSNIText {
		t.Fatalf("quiet-log marker %q out of sync with %q", errUnknownSNIText, certificates.ErrUnknownSNI)
	}
}

func TestSourceGuardBlocksAfterThresholdAndExpires(t *testing.T) {
	g := newSourceGuard(3, time.Minute)
	now := time.Unix(1000, 0)
	g.now = func() time.Time { return now }
	ip := netip.MustParseAddr("203.0.113.9")
	if g.fail(ip) || g.fail(ip) || g.isBlocked(ip) {
		t.Fatal("blocked too early")
	}
	if !g.fail(ip) || !g.isBlocked(ip) {
		t.Fatal("expected block at threshold")
	}
	now = now.Add(2 * time.Minute)
	if g.isBlocked(ip) {
		t.Fatal("block should expire")
	}
	if g.tracked() != 0 {
		t.Fatal("expired entry should be removed")
	}
}

func TestSourceGuardWindowResetsAndDisabled(t *testing.T) {
	g := newSourceGuard(2, time.Minute)
	now := time.Unix(1000, 0)
	g.now = func() time.Time { return now }
	ip := netip.MustParseAddr("203.0.113.10")
	g.fail(ip)
	now = now.Add(2 * time.Minute)
	if g.fail(ip) {
		t.Fatal("failure outside window must not count")
	}
	var disabled *sourceGuard = newSourceGuard(-1, 0)
	if disabled != nil || disabled.fail(ip) || disabled.isBlocked(ip) {
		t.Fatal("negative threshold must disable the guard")
	}
}

func freeTCP(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func TestUnknownSNIIsCountedThenSourceBlockedWithoutLogs(t *testing.T) {
	manager := certificates.NewManager()
	manager.SetHostFilter(func(string) bool { return false })
	var mu sync.Mutex
	reasons := map[string]int{}
	var logs bytes.Buffer
	httpsAddr := freeTCP(t)
	running, err := Start(Config{
		HTTP: freeTCP(t), ProxyHTTP: freeTCP(t), HTTPS: httpsAddr, ProxyHTTPS: freeTCP(t),
		TrustedCIDRs: []string{"127.0.0.0/8"}, HeaderTimeout: time.Second,
		TLS: manager.TLSConfig(), Handler: http.NotFoundHandler(),
		Logger:                   slog.New(slog.NewTextHandler(&logs, nil)),
		UnknownSNIBlockThreshold: 3, UnknownSNIBlockDuration: time.Minute,
		ObserveTLSHandshakeRejection: func(_, reason string) { mu.Lock(); reasons[reason]++; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close(context.Background())
	dial := func() error {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", httpsAddr, &tls.Config{ServerName: "portal.nextep-smart-order.feinian.net", InsecureSkipVerify: true})
		if conn != nil {
			conn.Close()
		}
		return err
	}
	for i := 0; i < 3; i++ {
		if err := dial(); err == nil {
			t.Fatal("handshake for unknown SNI must fail")
		}
	}
	time.Sleep(50 * time.Millisecond)
	if err := dial(); err == nil {
		t.Fatal("blocked source must fail")
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if reasons["unknown_sni"] != 3 || reasons["blocked_source"] != 1 {
		t.Fatalf("reasons = %v", reasons)
	}
	if strings.Contains(logs.String(), "handshake") {
		t.Fatalf("handshake noise was logged: %s", logs.String())
	}
}
