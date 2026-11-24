package proxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// Limits for the CONNECT response. A hostile or broken proxy could otherwise
// keep a goroutine busy and grow memory: the line read is capped by the buffer
// size, and the number of headers by maxHTTPHeaders.
const (
	maxHTTPLine    = 8 << 10
	maxHTTPHeaders = 100
)

// httpProxy tunnels TCP through an HTTP proxy using the CONNECT method.
type httpProxy struct {
	cfg Config
}

func init() {
	Register(HTTP, func(cfg Config) (Dialer, error) { return &httpProxy{cfg: cfg}, nil })
}

// Describe implements Dialer.
func (p *httpProxy) Describe() string { return describe(HTTP, p.cfg) }

// Dial implements Dialer.
func (p *httpProxy) Dial(ctx context.Context, target string) (net.Conn, error) {
	if _, _, err := parseTarget(target); err != nil {
		return nil, err
	}
	conn, err := dialProxy(ctx, p.cfg)
	if err != nil {
		return nil, err
	}
	stop := cancelOnDone(ctx, conn)

	br := bufio.NewReaderSize(conn, maxHTTPLine)
	if err := p.connect(br, conn, target); err != nil {
		stop()
		conn.Close()
		return nil, err
	}
	stop()
	_ = conn.SetDeadline(time.Time{})
	// The proxy may have pipelined data after the response header; hand back a
	// connection that reads through the buffered reader so nothing is lost.
	return &bufferedConn{Conn: conn, r: br}, nil
}

func (p *httpProxy) connect(br *bufio.Reader, conn net.Conn, target string) error {
	var sb strings.Builder
	sb.WriteString("CONNECT ")
	sb.WriteString(target)
	sb.WriteString(" HTTP/1.1\r\nHost: ")
	sb.WriteString(target)
	sb.WriteString("\r\n")
	if p.cfg.User != "" || p.cfg.Pass != "" {
		creds := base64.StdEncoding.EncodeToString([]byte(p.cfg.User + ":" + p.cfg.Pass))
		sb.WriteString("Proxy-Authorization: Basic ")
		sb.WriteString(creds)
		sb.WriteString("\r\n")
	}
	sb.WriteString("User-Agent: mctunnel/1.0\r\nProxy-Connection: Keep-Alive\r\n\r\n")
	if _, err := io.WriteString(conn, sb.String()); err != nil {
		return fmt.Errorf("http proxy: connect request: %w", err)
	}

	statusLine, err := readHTTPLine(br)
	if err != nil {
		return connectError(err, target, p.cfg.Timeout)
	}
	code, text, err := parseStatusLine(statusLine)
	if err != nil {
		return err
	}
	// Drain the rest of the response headers.
	for i := 0; ; i++ {
		if i > maxHTTPHeaders {
			return errors.New("http proxy: too many response headers")
		}
		line, err := readHTTPLine(br)
		if err != nil {
			return fmt.Errorf("http proxy: reading response headers: %w", err)
		}
		if line == "" {
			break
		}
	}

	switch {
	case code == 200:
		return nil
	case code == 407:
		return errors.New("http proxy: authentication required or rejected (check username and password)")
	case code == 403:
		return errors.New("http proxy: connection forbidden by the proxy (HTTP 403)")
	case code == 502 || code == 503 || code == 504:
		return fmt.Errorf("http proxy: proxy could not reach %s (HTTP %d %s)", target, code, text)
	}
	return fmt.Errorf("http proxy: connect rejected with HTTP %d %s", code, text)
}

// readHTTPLine reads one CRLF-terminated line without ever buffering more than
// the reader's buffer size. ReadString grows until it finds the newline, which
// lets a proxy that never sends one exhaust memory.
func readHTTPLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return "", errors.New("response line too long")
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(line), "\r\n"), nil
}

// connectError explains a failed CONNECT exchange. A silent proxy is the most
// common case: it accepts the request and never answers because the port or
// the host is not allowed.
func connectError(err error, target string, timeout time.Duration) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("http proxy: no answer to CONNECT %s within %s; "+
			"the proxy likely blocks this host or port", target, timeout)
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("http proxy: the proxy closed the connection during CONNECT %s", target)
	}
	return fmt.Errorf("http proxy: no CONNECT answer for %s: %w", target, err)
}

// parseStatusLine turns "HTTP/1.1 200 Connection established" into its code.
func parseStatusLine(line string) (int, string, error) {
	fields := strings.SplitN(line, " ", 3)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "HTTP/") {
		return 0, "", fmt.Errorf("http proxy: not an HTTP response: %q", line)
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, "", fmt.Errorf("http proxy: bad status %q", line)
	}
	text := line
	if len(fields) == 3 {
		text = fields[2]
	}
	return code, text, nil
}

// bufferedConn keeps bytes that were buffered while reading the CONNECT
// response available to the caller.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.r.Read(b) }

// CloseWrite forwards half-close to the underlying TCP connection.
func (c *bufferedConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}
