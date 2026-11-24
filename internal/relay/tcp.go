package relay

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"mctunnel/internal/addr"
	"mctunnel/internal/applog"
	"mctunnel/internal/mcnet"
	"mctunnel/internal/proxy"
)

// defaultHalfCloseGrace is how long the surviving direction of a session may
// stay open after the other one finished. Without it a proxy that silently
// drops a stream leaves the session (and its goroutine) alive forever: the
// game closed its side, but the far end never sends EOF.
const defaultHalfCloseGrace = 15 * time.Second

// defaultHandshakeTimeout bounds the wait for the first packet from the client.
// A connection that is opened and then goes quiet would otherwise hold a
// session slot for as long as it likes.
const defaultHandshakeTimeout = 10 * time.Second

// tcpTransport serves Minecraft Java Edition: one TCP stream per session.
type tcpTransport struct {
	grace            time.Duration
	handshakeTimeout time.Duration
}

// TCP returns the Minecraft Java Edition transport with the default grace
// period for half-closed sessions and the default first-packet timeout.
func TCP() Transport {
	return tcpTransport{grace: defaultHalfCloseGrace, handshakeTimeout: defaultHandshakeTimeout}
}

// TCPWithGrace is TCP with a custom grace period, used by tests.
func TCPWithGrace(grace time.Duration) Transport {
	return tcpTransport{grace: grace, handshakeTimeout: defaultHandshakeTimeout}
}

// TCPWithHandshakeTimeout is TCP with a custom first-packet timeout, used by
// tests.
func TCPWithHandshakeTimeout(d time.Duration) Transport {
	return tcpTransport{grace: defaultHalfCloseGrace, handshakeTimeout: d}
}

// Name implements Transport.
func (t tcpTransport) Name() string { return "tcp" }

// Forward implements Transport. The two directions are pumped independently so
// a half-closed socket does not tear down the other one immediately.
func (t tcpTransport) Forward(ctx context.Context, client net.Conn, target string, d proxy.Dialer, log *applog.Logger) error {
	remote, err := d.Dial(ctx, target)
	if err != nil {
		return fmt.Errorf("dial through proxy: %w", err)
	}
	defer remote.Close()

	// Stop() cancels ctx from another goroutine: closing both ends unblocks the
	// pumps below, so a running tunnel cannot outlive its Manager.
	watcher := make(chan struct{})
	defer close(watcher)
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
			_ = remote.Close()
		case <-watcher:
		}
	}()

	host, port, err := addr.HostPort(target)
	if err != nil {
		return fmt.Errorf("target: %w", err)
	}

	// Hold the client to a deadline until its first packet arrives. The
	// handshake reader clears it once it has the packet, so a long-lived session
	// is not affected.
	if t.handshakeTimeout > 0 {
		if d, ok := client.(interface{ SetReadDeadline(time.Time) error }); ok {
			_ = d.SetReadDeadline(time.Now().Add(t.handshakeTimeout))
		}
	}

	done := make(chan error, 2)
	go func() {
		// client -> server, with the handshake rewritten on the way.
		err := mcnet.ForwardStream(remote, client, host, port)
		closeWrite(remote)
		done <- err
	}()
	go func() {
		// server -> client, untouched.
		_, err := io.Copy(client, remote)
		closeWrite(client)
		done <- err
	}()

	var firstErr error
	record := func(err error) {
		if err != nil && !isClosed(err) && firstErr == nil {
			firstErr = err
		}
	}

	record(<-done)

	// Give the other direction a grace period to finish on its own, then stop
	// it. Cancelling the context also lands here because the watcher closes
	// both connections and both pumps return.
	grace := t.grace
	if grace <= 0 {
		grace = defaultHalfCloseGrace
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case err := <-done:
		record(err)
	case <-timer.C:
		_ = client.Close()
		_ = remote.Close()
		record(<-done)
	}
	return firstErr
}
