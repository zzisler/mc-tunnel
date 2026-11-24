package relay

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"mctunnel/internal/applog"
)

// directDialer stands in for the proxy: it connects straight to a local
// listener, so a test controls the far end of the session.
type directDialer struct{ addr string }

func (d directDialer) Describe() string { return "direct" }

func (d directDialer) Dial(context.Context, string) (net.Conn, error) {
	return net.Dial("tcp", d.addr)
}

// A client that connects and then sends nothing must not hold a session open
// forever: the first packet is read under a deadline.
func TestSilentClientHitsHandshakeDeadline(t *testing.T) {
	remote, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	go func() {
		for {
			conn, err := remote.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			io.Copy(io.Discard, conn)
		}
	}()

	local, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()

	silentGame, err := net.Dial("tcp", local.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer silentGame.Close()
	client, err := local.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tr := tcpTransport{grace: 100 * time.Millisecond, handshakeTimeout: 200 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err = tr.Forward(ctx, client, "mc.example.net:25565", directDialer{addr: remote.Addr().String()}, applog.New(10))
	if err == nil {
		t.Fatal("a silent client produced no error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("the deadline did not fire, Forward took %s", elapsed)
	}
}
