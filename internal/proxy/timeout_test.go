package proxy

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// startSilentProxy accepts connections and never answers, which is what a
// filtering proxy looks like from the client side.
func startSilentProxy(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	var (
		mu   sync.Mutex
		open []net.Conn
	)
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range open {
			conn.Close()
		}
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			open = append(open, conn)
			mu.Unlock()
			// Deliberately no response and no close.
		}
	}()
	return ln.Addr().String()
}

func TestHTTPConnectTimeoutIsExplained(t *testing.T) {
	addr := startSilentProxy(t)
	d, err := New(Config{Kind: HTTP, Addr: addr, Timeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Dial(testContext(t), "mc.example.net:25577")
	if err == nil {
		t.Fatal("expected a timeout")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no answer to CONNECT") {
		t.Fatalf("the error should explain the silent proxy, got: %v", err)
	}
	if !strings.Contains(msg, "mc.example.net:25577") {
		t.Fatalf("the error should name the target, got: %v", err)
	}
}

func TestSOCKS5TimeoutIsExplained(t *testing.T) {
	addr := startSilentProxy(t)
	d, err := New(Config{Kind: SOCKS5, Addr: addr, Timeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Dial(testContext(t), "mc.example.net:25577")
	if err == nil {
		t.Fatal("expected a timeout")
	}
	if !strings.Contains(err.Error(), "no answer") {
		t.Fatalf("the error should explain the silent proxy, got: %v", err)
	}
}
