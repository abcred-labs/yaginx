// Package listeners runs the fixed Yaginx business listener topology.
package listeners

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	proxyproto "github.com/pires/go-proxyproto"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/http2"
)

type Config struct {
	HTTP, ProxyHTTP, HTTPS, ProxyHTTPS, HTTP3 string
	TrustedCIDRs                              []string
	RequireProxyProtocol                      bool
	HeaderTimeout                             time.Duration
	ActivityTimeout                           time.Duration
	HTTP3MaxIdleTimeout                       time.Duration
	MaxConcurrentRequests                     int
	MaxTLSHandshakes                          int
	TLSHandshakeTimeout                       time.Duration
	MaxRequestHeaderBytes                     int
	MaxRequestBodyBytes                       int64
	TLS                                       *tls.Config
	Handler                                   http.Handler
	Logger                                    *slog.Logger
	// TrackRequest is invoked at the listener boundary and returns a closure
	// that is called when the HTTP request completes. It keeps listeners free
	// of observability package dependencies.
	TrackRequest func(listener, protocol string) func()
	// ObserveConcurrentRejection records requests rejected before tracing and
	// business handlers because the global request budget was exhausted.
	ObserveConcurrentRejection func(listener, protocol string)
	// ObserveTLSHandshakeRejection records connections dropped because the
	// bounded TLS handshake budget was full.
	ObserveTLSHandshakeRejection func(listener, reason string)
	// Instrument wraps a business listener without importing a specific tracing
	// SDK. The callback owns any context propagation and redaction policy.
	Instrument func(listener string, next http.Handler) http.Handler
	// UnknownSNIBlockThreshold is the number of rejected TLS handshakes (for
	// example unknown SNI from internet scanners) within a minute after which
	// the source is dropped at first read for UnknownSNIBlockDuration. Zero
	// uses the default of 20; a negative value disables blocking.
	UnknownSNIBlockThreshold int
	UnknownSNIBlockDuration  time.Duration
}
type Running struct {
	servers   []*http.Server
	h3        *http3.Server
	h3Packet  net.PacketConn
	listeners []net.Listener
	once      sync.Once
	done      chan struct{}
}

// requiredProxyListener validates the header before net/http receives a
// connection. go-proxyproto deliberately reports a missing header from the
// first Read only; net/http can otherwise turn that into an HTTP 400 response.
// The proxy listener contract requires a transport-level rejection instead.
type requiredProxyListener struct{ net.Listener }

func (l requiredProxyListener) Accept() (net.Conn, error) {
	for {
		connection, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if _, err := connection.Read(make([]byte, 0)); err == nil {
			return connection, nil
		}
		_ = connection.Close()
	}
}

type proxyRejectionLoggingListener struct {
	net.Listener
	logger   *slog.Logger
	listener string
}

func (l proxyRejectionLoggingListener) Accept() (net.Conn, error) {
	connection, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &proxyRejectionLoggingConn{
		Conn:     connection,
		logger:   l.logger,
		listener: l.listener,
		source:   sourceIP(connection.RemoteAddr()),
	}, nil
}

type proxyRejectionLoggingConn struct {
	net.Conn
	logger   *slog.Logger
	listener string
	source   string
	logged   sync.Once
}

type tlsHandshakeListener struct {
	net.Listener
	config       *tls.Config
	slots        chan struct{}
	timeout      time.Duration
	closed       chan struct{}
	once         sync.Once
	observer     func(reason string)
	pendingMu    sync.Mutex
	pendingSlots map[*tlsHandshakeSlot]struct{}
	guard        *sourceGuard
}

type tlsHandshakeSlot struct {
	owner *tlsHandshakeListener
	conn  net.Conn
	once  sync.Once
}

func (s *tlsHandshakeSlot) release() {
	s.once.Do(func() {
		s.owner.pendingMu.Lock()
		delete(s.owner.pendingSlots, s)
		s.owner.pendingMu.Unlock()
		<-s.owner.slots
	})
}

type tlsSlotConn struct {
	net.Conn
	slot    *tlsHandshakeSlot
	checked sync.Once
}

// Read drops sources the guard has blocked before any TLS work happens. The
// check runs on the first read, when a PROXY protocol header (if any) has
// been consumed and RemoteAddr is the real client rather than the load balancer.
func (c *tlsSlotConn) Read(buffer []byte) (int, error) {
	var blocked bool
	c.checked.Do(func() {
		guard := c.slot.owner.guard
		if guard == nil {
			return
		}
		if ip, ok := addrOf(c.Conn.RemoteAddr()); ok && guard.isBlocked(ip) {
			blocked = true
		}
	})
	if blocked {
		c.slot.owner.guard.blocked.Add(1)
		if observer := c.slot.owner.observer; observer != nil {
			observer("blocked_source")
		}
		_ = c.Close()
		return 0, errBlockedSource
	}
	return c.Conn.Read(buffer)
}

func (c *tlsSlotConn) Close() error {
	err := c.Conn.Close()
	c.slot.release()
	return err
}

func (l *tlsHandshakeListener) Accept() (net.Conn, error) {
	for {
		connection, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.slots <- struct{}{}:
		case <-l.closed:
			_ = connection.Close()
			return nil, net.ErrClosed
		default:
			_ = connection.Close()
			if l.observer != nil {
				l.observer("limit")
			}
			continue
		}

		slot := &tlsHandshakeSlot{owner: l, conn: connection}
		l.pendingMu.Lock()
		l.pendingSlots[slot] = struct{}{}
		l.pendingMu.Unlock()
		if err := connection.SetDeadline(time.Now().Add(l.timeout)); err != nil {
			slot.release()
			_ = connection.Close()
			continue
		}
		tlsConfig := l.configFor(slot)
		return tls.Server(&tlsSlotConn{Conn: connection, slot: slot}, tlsConfig), nil
	}
}

func (l *tlsHandshakeListener) Close() error {
	var err error
	l.once.Do(func() {
		close(l.closed)
		err = l.Listener.Close()
		l.pendingMu.Lock()
		pending := make([]*tlsHandshakeSlot, 0, len(l.pendingSlots))
		for slot := range l.pendingSlots {
			pending = append(pending, slot)
		}
		l.pendingMu.Unlock()
		for _, slot := range pending {
			_ = slot.conn.Close()
			slot.release()
		}
	})
	return err
}

// rejectHandshake accounts for a handshake that failed certificate selection:
// one metric increment and one strike against the source, no log line.
func (l *tlsHandshakeListener) rejectHandshake(slot *tlsHandshakeSlot) {
	if l.observer != nil {
		l.observer("unknown_sni")
	}
	if ip, ok := addrOf(slot.conn.RemoteAddr()); ok {
		l.guard.fail(ip)
	}
}

func (l *tlsHandshakeListener) configFor(slot *tlsHandshakeSlot) *tls.Config {
	var wrap func(*tls.Config) *tls.Config
	wrap = func(source *tls.Config) *tls.Config {
		if source == nil {
			source = l.config
		}
		config := source.Clone()
		verify := config.VerifyConnection
		config.VerifyConnection = func(state tls.ConnectionState) error {
			if verify != nil {
				if err := verify(state); err != nil {
					return err
				}
			}
			_ = slot.conn.SetDeadline(time.Time{})
			slot.release()
			return nil
		}
		if getCertificate := config.GetCertificate; getCertificate != nil {
			config.GetCertificate = func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
				certificate, err := getCertificate(info)
				if err != nil {
					l.rejectHandshake(slot)
				}
				return certificate, err
			}
		}
		if getConfig := config.GetConfigForClient; getConfig != nil {
			config.GetConfigForClient = func(info *tls.ClientHelloInfo) (*tls.Config, error) {
				selected, err := getConfig(info)
				if err != nil || selected == nil {
					return selected, err
				}
				return wrap(selected), nil
			}
		}
		return config
	}
	return wrap(l.config)
}

func (c *proxyRejectionLoggingConn) Read(buffer []byte) (int, error) {
	n, err := c.Conn.Read(buffer)
	if errors.Is(err, proxyproto.ErrSuperfluousProxyHeader) {
		c.logged.Do(func() {
			c.logger.Warn("reject PROXY connection from untrusted source", "listener", c.listener, "source_ip", c.source)
		})
	}
	return n, err
}

func (r *Running) Close(ctx context.Context) error {
	var first error
	r.once.Do(func() {
		close(r.done)
		if r.h3 != nil {
			if err := r.h3.Close(); err != nil && first == nil {
				first = err
			}
		}
		if r.h3Packet != nil {
			if err := r.h3Packet.Close(); err != nil && first == nil {
				first = err
			}
		}
		for _, listener := range r.listeners {
			if err := listener.Close(); err != nil && first == nil {
				first = err
			}
		}
		for _, server := range r.servers {
			if err := server.Shutdown(ctx); err != nil && first == nil {
				first = err
			}
		}
	})
	return first
}

func Start(config Config) (*Running, error) {
	if config.Handler == nil {
		return nil, fmt.Errorf("listener handler is required")
	}
	if config.TLS == nil {
		return nil, fmt.Errorf("TLS config is required")
	}
	policy, err := proxyproto.StrictWhiteListPolicy(config.TrustedCIDRs)
	if err != nil {
		return nil, fmt.Errorf("trusted PROXY CIDRs: %w", err)
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	running := &Running{done: make(chan struct{})}
	if config.ActivityTimeout <= 0 {
		config.ActivityTimeout = 2 * time.Hour
	}
	if config.MaxRequestHeaderBytes <= 0 {
		config.MaxRequestHeaderBytes = 1 << 20
	}
	if config.HTTP3MaxIdleTimeout <= 0 {
		config.HTTP3MaxIdleTimeout = 30 * time.Second
	}
	if config.MaxConcurrentRequests < 1 {
		config.MaxConcurrentRequests = 256
	}
	if config.MaxTLSHandshakes < 1 {
		config.MaxTLSHandshakes = min(config.MaxConcurrentRequests, 64)
	}
	if config.TLSHandshakeTimeout <= 0 {
		config.TLSHandshakeTimeout = 10 * time.Second
	}
	guard := newSourceGuard(config.UnknownSNIBlockThreshold, config.UnknownSNIBlockDuration)
	if guard != nil {
		go guard.summarize(logger, running.done)
	}
	errorLog := log.New(serverErrorWriter{logger: logger}, "", 0)
	requestSlots := make(chan struct{}, config.MaxConcurrentRequests)
	tlsSlots := make(chan struct{}, config.MaxTLSHandshakes)
	handlerFor := func(name string) http.Handler {
		handler := config.Handler
		if config.MaxRequestBodyBytes > 0 {
			next := handler
			handler = http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.ContentLength > config.MaxRequestBodyBytes {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					return
				}
				request.Body = http.MaxBytesReader(w, request.Body, config.MaxRequestBodyBytes)
				next.ServeHTTP(w, request)
			})
		}
		if config.TrackRequest != nil {
			next := handler
			handler = http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				done := config.TrackRequest(name, request.Proto)
				if done != nil {
					defer done()
				}
				next.ServeHTTP(w, request)
			})
		}
		if config.Instrument != nil {
			handler = config.Instrument(name, handler)
		}
		next := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			select {
			case requestSlots <- struct{}{}:
				defer func() { <-requestSlots }()
				next.ServeHTTP(w, request)
			default:
				if config.ObserveConcurrentRejection != nil {
					config.ObserveConcurrentRejection(name, request.Proto)
				}
				w.Header().Set("Retry-After", "1")
				http.Error(w, "server is busy", http.StatusServiceUnavailable)
			}
		})
		return handler
	}
	startHTTP := func(name, addr string, proxy bool, tlsEnabled bool) error {
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		if proxy {
			if config.RequireProxyProtocol {
				connPolicy := func(options proxyproto.ConnPolicyOptions) (proxyproto.Policy, error) {
					decision, err := policy(options.Upstream)
					if err != nil {
						return proxyproto.REJECT, err
					}
					if decision == proxyproto.REJECT {
						logger.Warn("reject PROXY connection from untrusted source", "listener", name, "source_ip", sourceIP(options.Upstream))
						return proxyproto.REJECT, proxyproto.ErrInvalidUpstream
					}
					return proxyproto.REQUIRE, nil
				}
				listener = &proxyproto.Listener{Listener: listener, ConnPolicy: connPolicy, ReadHeaderTimeout: config.HeaderTimeout}
				listener = requiredProxyListener{Listener: listener}
			} else {
				listener = proxyRejectionLoggingListener{
					Listener: &proxyproto.Listener{Listener: listener, Policy: policy, ReadHeaderTimeout: config.HeaderTimeout},
					logger:   logger,
					listener: name,
				}
			}
		}
		var serverTLSConfig *tls.Config
		if tlsEnabled {
			serverTLSConfig = config.TLS.Clone()
			serverTLSConfig.NextProtos = appendUnique(serverTLSConfig.NextProtos, "h2", "http/1.1")
			listener = &tlsHandshakeListener{
				Listener: listener, config: serverTLSConfig, slots: tlsSlots,
				timeout: config.TLSHandshakeTimeout, closed: make(chan struct{}),
				observer: func(reason string) {
					if config.ObserveTLSHandshakeRejection != nil {
						config.ObserveTLSHandshakeRejection(name, reason)
					}
				},
				pendingSlots: make(map[*tlsHandshakeSlot]struct{}), guard: guard,
			}
		}
		server := &http.Server{Handler: handlerFor(name), ReadHeaderTimeout: 30 * time.Second, IdleTimeout: config.ActivityTimeout, MaxHeaderBytes: config.MaxRequestHeaderBytes, TLSConfig: serverTLSConfig, ErrorLog: errorLog}
		if tlsEnabled {
			if err := http2.ConfigureServer(server, &http2.Server{MaxConcurrentStreams: uint32(config.MaxConcurrentRequests)}); err != nil {
				_ = listener.Close()
				return err
			}
		}
		running.listeners = append(running.listeners, listener)
		running.servers = append(running.servers, server)
		go func() { _ = server.Serve(listener) }()
		return nil
	}
	if err := startHTTP("http", config.HTTP, false, false); err != nil {
		running.Close(context.Background())
		return nil, err
	}
	if err := startHTTP("proxy_http", config.ProxyHTTP, true, false); err != nil {
		running.Close(context.Background())
		return nil, err
	}
	if err := startHTTP("https", config.HTTPS, false, true); err != nil {
		running.Close(context.Background())
		return nil, err
	}
	if err := startHTTP("proxy_https", config.ProxyHTTPS, true, true); err != nil {
		running.Close(context.Background())
		return nil, err
	}
	if config.HTTP3 != "" {
		packet, err := net.ListenPacket("udp", config.HTTP3)
		if err != nil {
			running.Close(context.Background())
			return nil, err
		}
		running.h3 = &http3.Server{
			Addr: config.HTTP3, Handler: handlerFor("http3"), TLSConfig: http3.ConfigureTLSConfig(config.TLS),
			QUICConfig: &quic.Config{
				HandshakeIdleTimeout:       config.TLSHandshakeTimeout,
				MaxIdleTimeout:             config.HTTP3MaxIdleTimeout,
				MaxIncomingStreams:         int64(config.MaxConcurrentRequests),
				MaxIncomingUniStreams:      16,
				MaxStreamReceiveWindow:     4 << 20,
				MaxConnectionReceiveWindow: 16 << 20,
			},
		}
		running.h3Packet = packet
		go func() { _ = running.h3.Serve(packet) }()
	}
	return running, nil
}

func sourceIP(address net.Addr) string {
	if address == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(address.String())
	if err == nil {
		return host
	}
	return address.String()
}

func appendUnique(values []string, additions ...string) []string {
	out := append([]string(nil), values...)
	for _, addition := range additions {
		found := false
		for _, value := range out {
			if value == addition {
				found = true
				break
			}
		}
		if !found {
			out = append(out, addition)
		}
	}
	return out
}
