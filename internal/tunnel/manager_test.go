package tunnel

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"mctunnel/internal/applog"
	"mctunnel/internal/mcnet"
	"mctunnel/internal/proxy"
)

// fakeProxy is a SOCKS5 server that always tunnels to target.
func fakeProxy(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeSOCKS5(conn)
		}
	}()
	return ln.Addr().String()
}

func handleFakeSOCKS5(conn net.Conn) {
	defer conn.Close()
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return
	}
	if _, err := io.ReadFull(conn, make([]byte, int(head[1]))); err != nil {
		return
	}
	conn.Write([]byte{5, 0})

	req := make([]byte, 4)
	if _, err := io.ReadFull(conn, req); err != nil {
		return
	}
	switch req[3] {
	case 1:
		io.ReadFull(conn, make([]byte, 4))
	case 4:
		io.ReadFull(conn, make([]byte, 16))
	case 3:
		l := make([]byte, 1)
		io.ReadFull(conn, l)
		io.ReadFull(conn, make([]byte, int(l[0])))
	}
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return
	}
	target := net.JoinHostPort("127.0.0.1", itoa(int(binary.BigEndian.Uint16(portBuf))))
	remote, err := net.Dial("tcp", target)
	if err != nil {
		conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()
	conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
	go func() { io.Copy(remote, conn); remote.Close() }()
	io.Copy(conn, remote)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}

// fakeMinecraft is a server that records the host it received in the handshake
// and then answers with a fixed payload.
type fakeMinecraft struct {
	addr string
	host chan string
}

func startFakeMinecraft(t *testing.T, reply string) *fakeMinecraft {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &fakeMinecraft{addr: ln.Addr().String(), host: make(chan string, 4)}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				// Read the length prefix and the packet without decoding it fully.
				length, err := mcnet.ReadVarInt(conn)
				if err != nil {
					return
				}
				payload := make([]byte, length)
				if _, err := io.ReadFull(conn, payload); err != nil {
					return
				}
				host, _, _ := parseHandshakeHost(payload)
				server.host <- host
				conn.Write([]byte(reply))
				io.Copy(io.Discard, conn)
			}(conn)
		}
	}()
	return server
}

// parseHandshakeHost pulls the server address out of a handshake payload.
func parseHandshakeHost(payload []byte) (string, int, bool) {
	pos := 0
	read := func() (int, bool) {
		v, err := mcnet.ReadVarInt(bytes.NewReader(payload[pos:]))
		if err != nil {
			return 0, false
		}
		pos += len(mcnet.AppendVarInt(nil, v))
		return v, true
	}
	if _, ok := read(); !ok { // packet id
		return "", 0, false
	}
	if _, ok := read(); !ok { // protocol version
		return "", 0, false
	}
	addrLen, ok := read()
	if !ok || pos+addrLen+2 > len(payload) {
		return "", 0, false
	}
	host := string(payload[pos : pos+addrLen])
	pos += addrLen
	port := int(binary.BigEndian.Uint16(payload[pos : pos+2]))
	return host, port, true
}

func buildHandshake(host string, port int) []byte {
	var payload []byte
	payload = mcnet.AppendVarInt(payload, 0)
	payload = mcnet.AppendVarInt(payload, 767)
	payload = mcnet.AppendVarInt(payload, len(host))
	payload = append(payload, host...)
	payload = append(payload, byte(port>>8), byte(port))
	payload = mcnet.AppendVarInt(payload, 2)
	return append(mcnet.AppendVarInt(nil, len(payload)), payload...)
}

func newManager(t *testing.T) (*Manager, *applog.Logger) {
	t.Helper()
	logger := applog.New(500)
	return New(logger), logger
}

// The end-to-end path: client connects to the local port, the tunnel dials
// through the proxy, rewrites the handshake and relays the payload back.
func TestSessionRewritesHandshakeAndRelays(t *testing.T) {
	server := startFakeMinecraft(t, "welcome")
	dialer, err := proxy.New(proxy.Config{Kind: proxy.SOCKS5, Addr: fakeProxy(t)})
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := newManager(t)
	addrs, err := manager.Start(Config{Server: server.addr, Listen: "127.0.0.1:0", Proxy: dialer})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer manager.Stop()

	conn, err := net.Dial("tcp", addrs[0])
	if err != nil {
		t.Fatalf("connect to the local port: %v", err)
	}
	defer conn.Close()

	// The game thinks it talks to localhost; the tunnel must send the real host.
	if _, err := conn.Write(buildHandshake("127.0.0.1", 25565)); err != nil {
		t.Fatal(err)
	}

	select {
	case host := <-server.host:
		if host != "127.0.0.1" {
			t.Fatalf("server saw host %q", host)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the handshake never reached the server")
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, len("welcome"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("reading the reply: %v", err)
	}
	if string(buf) != "welcome" {
		t.Fatalf("reply = %q", buf)
	}

	waitFor(t, func() bool { return manager.Stats().Total == 1 })
	stats := manager.Stats()
	if stats.Active != 1 {
		t.Fatalf("active = %d, want 1", stats.Active)
	}
	if stats.Up == 0 {
		t.Fatal("up counter stayed at zero")
	}

	conn.Close()
	waitFor(t, func() bool { return manager.Stats().Active == 0 })
}

// Stop must kill live sessions, not just the listener. This is the bug the old
// build had.
func TestStopClosesLiveSessions(t *testing.T) {
	mc := startFakeMinecraft(t, "welcome")
	dialer, err := proxy.New(proxy.Config{Kind: proxy.SOCKS5, Addr: fakeProxy(t)})
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := newManager(t)
	addrs, err := manager.Start(Config{Server: mc.addr, Listen: "127.0.0.1:0", Proxy: dialer})
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", addrs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write(buildHandshake("127.0.0.1", 25565)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-mc.host:
	case <-time.After(5 * time.Second):
		t.Fatal("session never started")
	}
	waitFor(t, func() bool { return manager.Stats().Active == 1 })

	manager.Stop()
	if manager.Running() {
		t.Fatal("Running() is still true after Stop")
	}

	// Anything the server already sent may still be sitting in the socket
	// buffer, so drain until the connection actually ends.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 4096)
	for {
		if _, err := conn.Read(buf); err != nil {
			break // closed or reset: exactly what the test wants
		}
	}
	waitFor(t, func() bool { return manager.Stats().Active == 0 })
}

// A second Start must be refused instead of opening another listener: this is
// the double click bug that used to produce a bogus "address in use" error.
func TestStartTwiceIsRefused(t *testing.T) {
	mc := startFakeMinecraft(t, "")
	dialer, err := proxy.New(proxy.Config{Kind: proxy.SOCKS5, Addr: fakeProxy(t)})
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := newManager(t)
	cfg := Config{Server: mc.addr, Listen: "127.0.0.1:0", Proxy: dialer}
	if _, err := manager.Start(cfg); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	if _, err := manager.Start(cfg); err == nil {
		t.Fatal("the second Start should have been refused")
	}
}

// After Stop a fresh Start has to work again with a new listener.
func TestRestartAfterStop(t *testing.T) {
	mc := startFakeMinecraft(t, "")
	dialer, err := proxy.New(proxy.Config{Kind: proxy.SOCKS5, Addr: fakeProxy(t)})
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := newManager(t)
	cfg := Config{Server: mc.addr, Listen: "127.0.0.1:0", Proxy: dialer}
	for i := 0; i < 3; i++ {
		if _, err := manager.Start(cfg); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		manager.Stop()
		waitFor(t, func() bool { return !manager.Running() })
	}
}

func TestStartValidatesConfig(t *testing.T) {
	manager, _ := newManager(t)
	if _, err := manager.Start(Config{Server: "host", Listen: "127.0.0.1:0"}); err == nil {
		t.Fatal("a missing proxy must be refused")
	}
	if _, err := manager.Start(Config{Server: "host.tld:notaport", Proxy: proxyStub{}}); err == nil {
		t.Fatal("a bad server must be refused")
	}
	if _, err := manager.Start(Config{
		Server: "host.tld",
		Listen: "127.0.0.1:0",
		Proxy:  proxyStub{},
	}); err != nil {
		t.Fatalf("a missing port should default to 25565: %v", err)
	}
	manager.Stop()
}

func TestStopWithoutStartIsSafe(t *testing.T) {
	manager, _ := newManager(t)
	manager.Stop()
	manager.Stop()
	if manager.Running() {
		t.Fatal("Running() = true")
	}
}

// The default listen address must serve both loopback families so that typing
// "localhost" in the game works.
func TestListenAllCoversIPv6Loopback(t *testing.T) {
	lns, addrs, err := listenAll("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	for _, ln := range lns {
		ln.Close()
	}
	if len(addrs) != 1 {
		t.Fatalf("an ephemeral port must stay one listener, got %v", addrs)
	}

	lns, addrs, err = listenAll("127.0.0.1:25565")
	if err != nil {
		t.Skipf("cannot bind the Minecraft port here: %v", err)
	}
	defer func() {
		for _, ln := range lns {
			ln.Close()
		}
	}()
	if len(addrs) != 2 {
		t.Fatalf("expected IPv4 and IPv6 loopback, got %v", addrs)
	}
	if !strings.Contains(addrs[1], "::1") {
		t.Fatalf("second address = %q, want IPv6 loopback", addrs[1])
	}
}

// proxyStub is a Dialer that always fails; it only satisfies validation paths.
type proxyStub struct{}

func (proxyStub) Dial(context.Context, string) (net.Conn, error) {
	return nil, context.Canceled
}

func (proxyStub) Describe() string { return "stub://none" }

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true in time")
}
