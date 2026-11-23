package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// SOCKS5 protocol constants (RFC 1928 / RFC 1929).
const (
	socksVersion      = 0x05
	socksMethodNone   = 0x00
	socksMethodUser   = 0x02
	socksMethodReject = 0xff
	socksCmdConnect   = 0x01
	socksAtypIPv4     = 0x01
	socksAtypDomain   = 0x03
	socksAtypIPv6     = 0x04
)

// socks5 tunnels TCP through a SOCKS5 server.
type socks5 struct {
	cfg Config
}

func init() {
	Register(SOCKS5, func(cfg Config) (Dialer, error) { return &socks5{cfg: cfg}, nil })
}

// Describe implements Dialer.
func (p *socks5) Describe() string { return describe(SOCKS5, p.cfg) }

// Dial implements Dialer.
func (p *socks5) Dial(ctx context.Context, target string) (net.Conn, error) {
	host, port, err := parseTarget(target)
	if err != nil {
		return nil, err
	}
	conn, err := dialProxy(ctx, p.cfg)
	if err != nil {
		return nil, err
	}
	stop := cancelOnDone(ctx, conn)
	if err := p.handshake(conn, host, port); err != nil {
		stop()
		conn.Close()
		return nil, timeoutHint(err, target, p.cfg.Timeout)
	}
	stop()
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func (p *socks5) handshake(conn net.Conn, host string, port int) error {
	// Greeting: offer user/password first when we have credentials so the
	// server picks it, otherwise no-auth only.
	var methods []byte
	if p.cfg.User != "" || p.cfg.Pass != "" {
		methods = []byte{socksMethodUser, socksMethodNone}
	} else {
		methods = []byte{socksMethodNone}
	}
	greeting := append([]byte{socksVersion, byte(len(methods))}, methods...)
	if _, err := conn.Write(greeting); err != nil {
		return fmt.Errorf("socks5: greeting: %w", err)
	}

	var reply [2]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return fmt.Errorf("socks5: greeting reply: %w", err)
	}
	if reply[0] != socksVersion {
		return fmt.Errorf("socks5: proxy answered version %d, not 5", reply[0])
	}
	switch reply[1] {
	case socksMethodNone:
		// no authentication needed
	case socksMethodUser:
		if err := p.auth(conn); err != nil {
			return err
		}
	case socksMethodReject:
		return errors.New("socks5: proxy accepted no authentication method (it may require credentials)")
	default:
		return fmt.Errorf("socks5: proxy asked for unsupported method %#x", reply[1])
	}

	req := []byte{socksVersion, socksCmdConnect, 0x00}
	req = append(req, encodeAddress(host)...)
	var portBuf [2]byte
	binary.BigEndian.PutUint16(portBuf[:], uint16(port))
	req = append(req, portBuf[:]...)
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("socks5: connect request: %w", err)
	}

	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return fmt.Errorf("socks5: connect reply: %w", err)
	}
	if head[0] != socksVersion {
		return fmt.Errorf("socks5: proxy answered version %d, not 5", head[0])
	}
	if head[1] != 0 {
		return fmt.Errorf("socks5: %s", socksReplyText(head[1]))
	}
	if err := discardBoundAddress(conn, head[3]); err != nil {
		return err
	}
	return nil
}

func (p *socks5) auth(conn net.Conn) error {
	if len(p.cfg.User) > 255 || len(p.cfg.Pass) > 255 {
		return errors.New("socks5: username or password is longer than 255 bytes")
	}
	msg := []byte{0x01, byte(len(p.cfg.User))}
	msg = append(msg, p.cfg.User...)
	msg = append(msg, byte(len(p.cfg.Pass)))
	msg = append(msg, p.cfg.Pass...)
	if _, err := conn.Write(msg); err != nil {
		return fmt.Errorf("socks5: authentication: %w", err)
	}
	var reply [2]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return fmt.Errorf("socks5: authentication reply: %w", err)
	}
	if reply[1] != 0 {
		return errors.New("socks5: authentication failed (check username and password)")
	}
	return nil
}

func encodeAddress(host string) []byte {
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return append([]byte{socksAtypIPv4}, ip4...)
		}
		if ip16 := ip.To16(); ip16 != nil {
			return append([]byte{socksAtypIPv6}, ip16...)
		}
	}
	// A hostname is resolved by the proxy, which is what keeps DNS off the
	// local side of the tunnel.
	return append([]byte{socksAtypDomain, byte(len(host))}, host...)
}

func discardBoundAddress(conn net.Conn, atyp byte) error {
	var n int
	switch atyp {
	case socksAtypIPv4:
		n = 4
	case socksAtypIPv6:
		n = 16
	case socksAtypDomain:
		var size [1]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return fmt.Errorf("socks5: connect reply: %w", err)
		}
		n = int(size[0])
	default:
		return fmt.Errorf("socks5: proxy returned unknown address type %#x", atyp)
	}
	if n > 0 {
		if _, err := io.ReadFull(conn, make([]byte, n+2)); err != nil {
			return fmt.Errorf("socks5: connect reply: %w", err)
		}
	}
	return nil
}

func socksReplyText(code byte) string {
	switch code {
	case 1:
		return "general server failure"
	case 2:
		return "connection not allowed by ruleset"
	case 3:
		return "network unreachable"
	case 4:
		return "host unreachable"
	case 5:
		return "connection refused by the target"
	case 6:
		return "TTL expired"
	case 7:
		return "command not supported"
	case 8:
		return "address type not supported"
	}
	return "connect rejected with code " + strconv.Itoa(int(code))
}

// parseTarget validates a "host:port" target and splits it.
func parseTarget(target string) (string, int, error) {
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return "", 0, fmt.Errorf("target %q: %w", target, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("target %q: invalid port", target)
	}
	if host == "" {
		return "", 0, fmt.Errorf("target %q: no host", target)
	}
	return host, port, nil
}
