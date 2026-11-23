package proxy

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func echoRoundTrip(t *testing.T, conn net.Conn) {
	t.Helper()
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("echo = %q", buf)
	}
}

func TestSOCKS5NoAuthDomainTarget(t *testing.T) {
	echo := echoListener(t)
	var target string
	addr := startFakeSOCKS5(t, fakeSOCKS5{
		report: func(tgt string) { target = tgt },
		dial:   func(string) (net.Conn, error) { return net.Dial("tcp", echo) },
	})
	d, err := New(Config{Kind: SOCKS5, Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := d.Dial(testContext(t), "mc.example.net:25565")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	// A hostname must reach the proxy as a domain so DNS stays on its side.
	if target != "mc.example.net:25565" {
		t.Fatalf("proxy saw target %q", target)
	}
	echoRoundTrip(t, conn)
}

func TestSOCKS5Auth(t *testing.T) {
	echo := echoListener(t)
	addr := startFakeSOCKS5(t, fakeSOCKS5{
		requireAuth: true,
		dial:        func(string) (net.Conn, error) { return net.Dial("tcp", echo) },
	})
	d, err := New(Config{Kind: SOCKS5, Addr: addr, User: "bob", Pass: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := d.Dial(testContext(t), "1.2.3.4:25565")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	echoRoundTrip(t, conn)
}

func TestSOCKS5WrongPassword(t *testing.T) {
	addr := startFakeSOCKS5(t, fakeSOCKS5{
		requireAuth: true,
		dial:        func(string) (net.Conn, error) { return nil, nil },
	})
	d, err := New(Config{Kind: SOCKS5, Addr: addr, User: "bob", Pass: "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Dial(testContext(t), "1.2.3.4:25565")
	if err == nil {
		t.Fatal("expected an authentication error")
	}
	if !strings.Contains(err.Error(), "authentication") {
		t.Fatalf("unhelpful error: %v", err)
	}
}

func TestSOCKS5TargetRefused(t *testing.T) {
	addr := startFakeSOCKS5(t, fakeSOCKS5{
		dial: func(string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Err: errString("connection refused")}
		},
	})
	d, err := New(Config{Kind: SOCKS5, Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Dial(testContext(t), "1.2.3.4:25565")
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("expected a connection refused error, got %v", err)
	}
}

func TestSOCKS5ProxyUnreachable(t *testing.T) {
	d, err := New(Config{Kind: SOCKS5, Addr: "127.0.0.1:1", Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Dial(testContext(t), "1.2.3.4:25565")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "cannot reach proxy") {
		t.Fatalf("unhelpful error: %v", err)
	}
}

func TestHTTPConnect(t *testing.T) {
	addr, lastRequest, tunnel := startFakeHTTPProxy(t, "200 Connection established", "")
	d, err := New(Config{Kind: HTTP, Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := d.Dial(testContext(t), "mc.example.net:25565")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if !strings.HasPrefix(*lastRequest, "CONNECT mc.example.net:25565 HTTP/1.1\r\n") {
		t.Fatalf("request = %q", *lastRequest)
	}
	select {
	case proxySide := <-tunnel:
		echoRoundTrip(t, conn)
		proxySide.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("the fake proxy never saw the tunnel")
	}
}

func TestHTTPConnectSendsBasicAuth(t *testing.T) {
	addr, lastRequest, _ := startFakeHTTPProxy(t, "200 OK", basicAuth("bob", "secret"))
	d, err := New(Config{Kind: HTTP, Addr: addr, User: "bob", Pass: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := d.Dial(testContext(t), "1.2.3.4:25565")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()
	if !strings.Contains(*lastRequest, "Proxy-Authorization: "+basicAuth("bob", "secret")) {
		t.Fatalf("request = %q", *lastRequest)
	}
}

func TestHTTPConnectRejected(t *testing.T) {
	addr, _, _ := startFakeHTTPProxy(t, "403 Forbidden", "")
	d, err := New(Config{Kind: HTTP, Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Dial(testContext(t), "1.2.3.4:25565")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected a 403 error, got %v", err)
	}
}

func TestHTTPConnectAuthRequired(t *testing.T) {
	addr, _, _ := startFakeHTTPProxy(t, "200 OK", basicAuth("bob", "secret"))
	d, err := New(Config{Kind: HTTP, Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Dial(testContext(t), "1.2.3.4:25565")
	if err == nil || !strings.Contains(err.Error(), "authentication") {
		t.Fatalf("expected an authentication error, got %v", err)
	}
}

// errString is an error with a fixed message, used to fake a dial failure.
type errString string

func (e errString) Error() string { return string(e) }
