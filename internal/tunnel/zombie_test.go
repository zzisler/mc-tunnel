package tunnel

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"mctunnel/internal/proxy"
	"mctunnel/internal/relay"
)

// silentServer accepts a connection, reads whatever arrives and never answers
// or closes. This is exactly the state the user saw: the game already gave up
// while the proxy silently drops the stream.
func silentServer(t *testing.T) string {
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
			go io.Copy(io.Discard, conn)
		}
	}()
	return ln.Addr().String()
}

// A session whose client went away and whose server stays silent must not stay
// "active" forever: the half-close grace period ends it.
func TestSilentServerSessionEndsAfterGrace(t *testing.T) {
	server := silentServer(t)
	dialer, err := proxy.New(proxy.Config{Kind: proxy.SOCKS5, Addr: fakeProxy(t)})
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := newManager(t)
	addrs, err := manager.Start(Config{
		Server:    server,
		Listen:    "127.0.0.1:0",
		Proxy:     dialer,
		Transport: relay.TCPWithGrace(300 * time.Millisecond),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()

	conn, err := net.Dial("tcp", addrs[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(buildHandshake("127.0.0.1", 25565)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return manager.Stats().Active == 1 })

	// The game gives up and closes its side; the far end never answers.
	conn.Close()

	waitFor(t, func() bool { return manager.Stats().Active == 0 })
}
