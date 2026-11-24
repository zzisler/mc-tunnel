package mcnet

import (
	"bytes"
	"net"
	"testing"
	"time"
)

// recordingConn remembers every read deadline that was set on it.
type recordingConn struct {
	net.Conn
	deadlines []time.Time
}

func (c *recordingConn) SetReadDeadline(t time.Time) error {
	c.deadlines = append(c.deadlines, t)
	return c.Conn.SetReadDeadline(t)
}

// The caller bounds the first packet with a read deadline; the handshake reader
// has to drop it once the packet is in, or the session would die 10 seconds in.
func TestForwardStreamClearsFirstPacketDeadline(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	conn := &recordingConn{Conn: client}

	if err := conn.SetReadDeadline(time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	conn.deadlines = nil

	go func() {
		server.Write(buildHandshake(767, "play.example.com", 25565, 2))
		server.Close()
	}()

	var out bytes.Buffer
	if err := ForwardStream(&out, conn, "real.example.com", 25565); err != nil {
		t.Fatalf("forward: %v", err)
	}
	if len(conn.deadlines) == 0 {
		t.Fatal("the first-packet deadline was never cleared")
	}
	if last := conn.deadlines[len(conn.deadlines)-1]; !last.IsZero() {
		t.Fatalf("last deadline = %v, want the zero time", last)
	}
}
