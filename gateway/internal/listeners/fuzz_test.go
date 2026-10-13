package listeners

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"

	proxyproto "github.com/pires/go-proxyproto"
)

func FuzzProxyProtocolHeader(f *testing.F) {
	policy, err := proxyproto.StrictWhiteListPolicy([]string{"127.0.0.0/8"})
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte("PROXY TCP4 192.0.2.1 198.51.100.1 1234 443\r\n"))
	f.Add([]byte("\r\n\r\n\x00\r\nQUIT\n"))
	f.Add([]byte("not-a-proxy-header"))

	f.Fuzz(func(t *testing.T, header []byte) {
		if len(header) > 1<<20 {
			t.Skip()
		}
		server, client := net.Pipe()
		defer client.Close()
		listener := &proxyproto.Listener{
			Listener:          &singleConnectionListener{connection: proxyRemoteConn{Conn: server}},
			Policy:            policy,
			ReadHeaderTimeout: 100 * time.Millisecond,
		}
		result := make(chan error, 1)
		go func() {
			connection, err := listener.Accept()
			if connection != nil {
				_ = connection.Close()
			}
			result <- err
		}()
		_, _ = client.Write(header)
		_ = client.Close()
		select {
		case <-result:
		case <-time.After(time.Second):
			t.Fatal("PROXY parser did not finish after peer close")
		}
	})
}

type singleConnectionListener struct {
	connection net.Conn
	mu         sync.Mutex
	accepted   bool
}

func (l *singleConnectionListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.accepted {
		return nil, io.EOF
	}
	l.accepted = true
	return l.connection, nil
}
func (l *singleConnectionListener) Close() error   { return l.connection.Close() }
func (l *singleConnectionListener) Addr() net.Addr { return fuzzAddr("127.0.0.1:9443") }

type proxyRemoteConn struct{ net.Conn }

func (proxyRemoteConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

type fuzzAddr string

func (a fuzzAddr) Network() string { return "tcp" }
func (a fuzzAddr) String() string  { return string(a) }

var _ net.Listener = (*singleConnectionListener)(nil)
var _ net.Conn = proxyRemoteConn{}
