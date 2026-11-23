// Package relay carries game traffic between the local client and the server.
//
// One file per transport: tcp.go serves Minecraft Java Edition, udp.go is the
// reserved (and deliberately unimplemented) place for a future datagram
// transport. The Manager in package tunnel only talks to the Transport
// interface, so adding a transport means adding a file here and nothing else.
package relay

import (
	"context"
	"errors"
	"net"
	"strings"

	"mctunnel/internal/applog"
	"mctunnel/internal/proxy"
)

// ErrUnsupported is returned by transports that are not implemented.
var ErrUnsupported = errors.New("transport is not implemented")

// Transport forwards one accepted client connection to the target server
// through the dialer, and blocks until both directions are finished, the
// connection fails, or ctx is cancelled.
type Transport interface {
	// Name identifies the transport in logs and diagnostics.
	Name() string
	// Forward tunnels client to target through d.
	Forward(ctx context.Context, client net.Conn, target string, d proxy.Dialer, log *applog.Logger) error
}

// closeWriter is implemented by TCP connections and by conns that forward
// half-close to one.
type closeWriter interface {
	CloseWrite() error
}

// closeWrite half-closes conn so the peer sees a clean end of stream. It is a
// no-op for connections that cannot signal it.
func closeWrite(conn net.Conn) {
	if cw, ok := conn.(closeWriter); ok {
		_ = cw.CloseWrite()
	}
}

// isClosed reports whether err is just the result of the peer (or Stop) ending
// the session. Windows phrases these as "An established connection was aborted
// by the software in your host machine" and "An existing connection was
// forcibly closed by the remote host", which are normal when a game disconnects
// and must not be logged as errors.
func isClosed(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return true
	}
	msg := err.Error()
	for _, fragment := range []string{
		"use of closed network connection",
		"connection reset by peer",
		"forcibly closed",
		"aborted by the software",
		"broken pipe",
		"connection was aborted",
	} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}
