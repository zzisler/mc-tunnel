package tunnel

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"mctunnel/internal/applog"
	"mctunnel/internal/proxy"
	"mctunnel/internal/relay"
)

// blockingTransport keeps a session open until the tunnel is stopped, so a test
// can fill every session slot without needing a real peer.
type blockingTransport struct{}

func (blockingTransport) Name() string { return "blocking" }

func (blockingTransport) Forward(ctx context.Context, _ net.Conn, _ string, _ proxy.Dialer, _ *applog.Logger) error {
	<-ctx.Done()
	return ctx.Err()
}

var _ relay.Transport = blockingTransport{}

// The listener is reachable by any local process, so the number of sessions it
// serves at once has to be bounded.
func TestSessionLimit(t *testing.T) {
	log := applog.New(50)
	dialer, err := proxy.New(proxy.Config{Kind: proxy.SOCKS5, Addr: fakeProxy(t)})
	if err != nil {
		t.Fatal(err)
	}
	m := New(log)
	addrs, err := m.Start(Config{
		Server:      "mc.example.net:25565",
		Listen:      "127.0.0.1:0",
		Proxy:       dialer,
		Transport:   blockingTransport{},
		MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	first, err := net.Dial("tcp", addrs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	time.Sleep(150 * time.Millisecond) // let acceptLoop take the only slot

	second, err := net.Dial("tcp", addrs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	_ = second.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := second.Read(make([]byte, 1)); err == nil {
		t.Fatal("a second session was accepted over the limit")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatalf("the second session was not refused, it just hung: %v", err)
	}

	refused := false
	for _, e := range log.Entries() {
		if strings.Contains(e.Msg, "session limit") {
			refused = true
		}
	}
	if !refused {
		t.Fatal("the refusal was not logged")
	}
}
