// Package proxy opens TCP tunnels through a forward proxy.
//
// Every supported protocol lives in its own file (socks5.go, http.go) and
// registers a factory in this file, so a new protocol only needs a new file
// plus an init to become available everywhere else.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"mctunnel/internal/addr"
)

// Kind identifies a proxy protocol.
type Kind string

// Supported proxy protocols.
const (
	SOCKS5 Kind = "socks5"
	HTTP   Kind = "http"
)

// Valid reports whether the kind is a protocol this build knows about.
func (k Kind) Valid() bool {
	_, ok := factories[k]
	return ok
}

// String implements fmt.Stringer.
func (k Kind) String() string { return string(k) }

// ParseKind maps a user supplied protocol name to a Kind.
func ParseKind(s string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "socks5", "socks5h", "socks", "5":
		return SOCKS5, nil
	case "http", "https", "httpconnect", "http-connect", "connect":
		return HTTP, nil
	case "socks4", "socks4a", "4":
		return "", fmt.Errorf("proxy type %q is not supported, convert it to socks5 or http", s)
	}
	return "", fmt.Errorf("unknown proxy type %q, want socks5 or http", s)
}

// DefaultPort is the conventional port for a proxy kind.
func (k Kind) DefaultPort() int {
	switch k {
	case SOCKS5:
		return 1080
	case HTTP:
		return 8080
	}
	return 0
}

// Config describes a proxy endpoint.
type Config struct {
	Kind    Kind
	Addr    string // host:port
	User    string
	Pass    string
	Timeout time.Duration // how long establishing the tunnel may take
}

// Dialer opens TCP tunnels to a target through the proxy.
type Dialer interface {
	// Dial connects to target ("host:port") through the proxy.
	Dial(ctx context.Context, target string) (net.Conn, error)
	// Describe returns a human readable form with the password hidden.
	Describe() string
}

// Factory builds a Dialer from an already validated Config.
type Factory func(Config) (Dialer, error)

var factories = map[Kind]Factory{}

// Register makes a proxy kind available to New. Adding VLESS, Shadowsocks or
// anything else later means adding a file with an init that calls Register.
func Register(kind Kind, f Factory) {
	factories[kind] = f
}

// New validates cfg and returns a ready to use Dialer.
func New(cfg Config) (Dialer, error) {
	if !cfg.Kind.Valid() {
		return nil, fmt.Errorf("proxy: unsupported type %q", cfg.Kind)
	}
	normalised, err := addr.Normalize(cfg.Addr, cfg.Kind.DefaultPort())
	if err != nil {
		return nil, fmt.Errorf("proxy address: %w", err)
	}
	cfg.Addr = normalised
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	return factories[cfg.Kind](cfg)
}

const defaultTimeout = 12 * time.Second

// describe renders a proxy URI with the password masked.
func describe(kind Kind, cfg Config) string {
	if cfg.User != "" {
		return fmt.Sprintf("%s://%s:***@%s", kind, cfg.User, cfg.Addr)
	}
	return fmt.Sprintf("%s://%s", kind, cfg.Addr)
}

// dialProxy opens the TCP connection to the proxy itself. The returned
// connection carries the deadline taken from ctx (or Timeout when ctx has
// none) so a stalled handshake cannot hang a tunnel forever.
func dialProxy(ctx context.Context, cfg Config) (net.Conn, error) {
	d := net.Dialer{Timeout: cfg.Timeout}
	conn, err := d.DialContext(ctx, "tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("%s: cannot reach proxy %s: %w", cfg.Kind, cfg.Addr, err)
	}
	deadline := time.Now().Add(cfg.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	return conn, nil
}

// cancelOnDone closes conn as soon as ctx is cancelled, so closing the tunnel
// cannot leave an in-flight handshake hanging.
func cancelOnDone(ctx context.Context, conn net.Conn) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

// timeoutHint rewrites a handshake timeout into something actionable: a proxy
// that never answers the SOCKS request is usually filtering the target.
func timeoutHint(err error, target string, timeout time.Duration) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("socks5: no answer within %s while connecting to %s; "+
			"the proxy may block this host or port", timeout, target)
	}
	return err
}
