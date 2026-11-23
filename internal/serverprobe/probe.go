// Package serverprobe asks a Minecraft server for its server list entry over an
// already established proxy tunnel.
//
// It answers the question a plain proxy check cannot: "does traffic to *my*
// server actually come back?" A proxy can accept a CONNECT and still drop the
// stream, which looks exactly like a client timeout.
package serverprobe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"mctunnel/internal/addr"
	"mctunnel/internal/mcnet"
	"mctunnel/internal/proxy"
)

// DefaultTimeout is how long the whole probe may take.
const DefaultTimeout = 15 * time.Second

// Result is the outcome of Status.
type Result struct {
	// Target is the probed "host:port".
	Target string
	// Latency is how long the tunnel plus the first answer took.
	Latency time.Duration
	// Version is the server version string, e.g. "Paper 1.20.4".
	Version string
	// Online and Slots come from the player list.
	Online int
	Slots  int
	// Err is set when no status could be read.
	Err error
}

// OK reports whether the server answered.
func (r Result) OK() bool { return r.Err == nil }

// String implements fmt.Stringer and is what the GUI prints.
func (r Result) String() string {
	if r.Err != nil {
		return "failed: " + r.Err.Error()
	}
	version := r.Version
	if version == "" {
		version = "unknown version"
	}
	return fmt.Sprintf("%s, %d/%d players, %d ms", version, r.Online, r.Slots, r.Latency.Milliseconds())
}

// Status opens a tunnel to target through d and performs a server list ping.
func Status(ctx context.Context, d proxy.Dialer, target string) Result {
	result := Result{Target: target}

	host, port, err := addr.HostPort(target)
	if err != nil {
		result.Err = err
		return result
	}
	if ctx.Err() != nil {
		result.Err = ctx.Err()
		return result
	}

	start := time.Now()
	conn, err := d.Dial(ctx, target)
	if err != nil {
		result.Err = fmt.Errorf("tunnel through the proxy: %w", err)
		return result
	}
	defer conn.Close()

	deadline := start.Add(DefaultTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)
	// Captured before the read: after a timeout, the remaining time is negative
	// and would be reported as "within 0s".
	allowed := deadline.Sub(time.Now())

	if _, err := conn.Write(mcnet.StatusRequest(host, port)); err != nil {
		result.Err = fmt.Errorf("sending the status request: %w", err)
		return result
	}
	status, err := mcnet.ReadStatusResponse(conn)
	if err != nil {
		result.Err = explain(err, allowed, target)
		return result
	}

	result.Latency = time.Since(start)
	result.Version = status.Version.Name
	result.Online = status.Players.Online
	result.Slots = status.Players.Max
	return result
}

// explain turns a bare read error into something a user can act on.
func explain(err error, allowed time.Duration, target string) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("no answer from %s within %s; the proxy let the connection through "+
			"but no data came back, or the server is down", target, allowed.Round(time.Second))
	}
	if isReset(err) {
		return fmt.Errorf("%s closed the connection instead of answering the status request "+
			"(wrong port, or the server rejects this proxy address)", target)
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s closed the connection without answering", target)
	}
	return err
}

// isReset recognises the Windows and POSIX wordings of a dropped connection.
func isReset(err error) bool {
	msg := err.Error()
	for _, fragment := range []string{
		"forcibly closed",
		"aborted by the software",
		"connection reset",
		"broken pipe",
	} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}
