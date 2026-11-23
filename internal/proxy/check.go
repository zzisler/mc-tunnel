package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Report is the outcome of Check.
type Report struct {
	// OK is true when a TCP tunnel through the proxy was established.
	OK bool
	// Latency is how long the proxy needed to open the tunnel.
	Latency time.Duration
	// ExitIP is the public address seen by the outside world, when known.
	ExitIP string
	// Target is the endpoint that was reached through the tunnel.
	Target string
	// Note carries a caveat such as "certificate not verified".
	Note string
	// Err is the failure that stopped the check.
	Err error
}

// String implements fmt.Stringer and is what the GUI prints.
func (r Report) String() string {
	switch {
	case r.OK && r.ExitIP != "" && r.Note != "":
		return fmt.Sprintf("ok, tunnel in %d ms, exit IP %s (%s)", r.Latency.Milliseconds(), r.ExitIP, r.Note)
	case r.OK && r.ExitIP != "":
		return fmt.Sprintf("ok, tunnel in %d ms, exit IP %s", r.Latency.Milliseconds(), r.ExitIP)
	case r.OK && r.Note != "":
		return fmt.Sprintf("ok, tunnel in %d ms (%s)", r.Latency.Milliseconds(), r.Note)
	case r.OK:
		return fmt.Sprintf("ok, tunnel in %d ms", r.Latency.Milliseconds())
	case r.Err != nil:
		return "failed: " + r.Err.Error()
	}
	return "failed"
}

// checkTargets are plain text endpoints that answer with the caller's address.
// Several are tried because proxies, captive portals and DNS filters like to
// break individual hosts.
var checkTargets = []struct {
	Host string
	Path string
}{
	{Host: "api.ipify.org", Path: "/"},
	{Host: "ifconfig.me", Path: "/ip"},
	{Host: "icanhazip.com", Path: "/"},
}

// Check verifies that the proxy really carries traffic by opening a tunnel to
// an outside service and asking it which address it sees.
func Check(ctx context.Context, d Dialer) Report {
	var (
		report   Report
		lastErr  error
		dialedOK bool
	)

	for _, target := range checkTargets {
		if ctx.Err() != nil {
			break
		}
		addr := net.JoinHostPort(target.Host, "443")
		for _, insecure := range []bool{false, true} {
			start := time.Now()
			conn, err := d.Dial(ctx, addr)
			if err != nil {
				lastErr = err
				break // no point retrying the same host in insecure mode
			}
			dialedOK = true
			report.Latency = time.Since(start)
			report.Target = addr

			ip, note, err := fetchExitIP(ctx, conn, target.Host, target.Path, insecure)
			conn.Close()
			if err != nil {
				lastErr = err
				if !insecure && isCertificateError(err) {
					continue // an intercepting proxy: try once without verification
				}
				break
			}
			report.OK = true
			report.ExitIP = ip
			report.Note = note
			return report
		}
	}

	// A tunnel that opened but failed the HTTP exchange still proves the proxy
	// works; report that as a success with a caveat instead of a blanket
	// failure.
	if dialedOK {
		report.OK = true
		report.Note = "could not read the exit IP"
		return report
	}
	if lastErr == nil {
		lastErr = errors.New("proxy check could not be attempted")
	}
	report.Err = lastErr
	return report
}

// fetchExitIP speaks minimal HTTPS to an echo service over an already
// established tunnel.
func fetchExitIP(ctx context.Context, conn net.Conn, host, path string, insecure bool) (string, string, error) {
	deadline := time.Now().Add(15 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecure, // a second pass, only after a certificate error
	})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return "", "", fmt.Errorf("tls handshake with %s: %w", host, err)
	}

	req := strings.Join([]string{
		"GET " + path + " HTTP/1.1",
		"Host: " + host,
		"User-Agent: mctunnel/1.0",
		"Accept: text/plain",
		"Connection: close",
		"", "",
	}, "\r\n")
	if _, err := io.WriteString(tlsConn, req); err != nil {
		return "", "", fmt.Errorf("request to %s: %w", host, err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(tlsConn), nil)
	if err != nil {
		return "", "", fmt.Errorf("response from %s: %w", host, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", "", fmt.Errorf("body from %s: %w", host, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", "", fmt.Errorf("%s answered %s", host, resp.Status)
	}
	ip := extractIP(string(body))
	if ip == "" {
		return "", "", fmt.Errorf("%s did not return an IP address", host)
	}
	note := ""
	if insecure {
		note = "TLS certificate not verified, the proxy may intercept traffic"
	}
	return ip, note, nil
}

// isCertificateError reports whether err came from certificate verification.
func isCertificateError(err error) bool {
	var verifyErr *tls.CertificateVerificationError
	if errors.As(err, &verifyErr) {
		return true
	}
	return strings.Contains(err.Error(), "x509:") ||
		strings.Contains(err.Error(), "certificate is not trusted")
}

// extractIP finds the first token in body that parses as an IP address.
func extractIP(body string) string {
	for _, field := range strings.Fields(body) {
		candidate := strings.Trim(field, ".,;[]")
		if net.ParseIP(candidate) != nil {
			return candidate
		}
	}
	return ""
}
