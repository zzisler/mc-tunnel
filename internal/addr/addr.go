// Package addr normalises the "host:port" strings that come from the GUI.
package addr

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Split separates host and port. hasPort reports whether s carried an explicit
// port; IPv6 literals may appear with or without brackets.
func Split(s string) (host, port string, hasPort bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false, fmt.Errorf("address is empty")
	}
	if strings.HasPrefix(s, "[") {
		if h, p, e := net.SplitHostPort(s); e == nil {
			return h, p, true, nil
		}
		if strings.HasSuffix(s, "]") {
			return s[1 : len(s)-1], "", false, nil
		}
		return "", "", false, fmt.Errorf("invalid address %q", s)
	}
	// A bare IPv6 literal has several colons and parses as an IP, not host:port.
	if strings.Count(s, ":") > 1 && net.ParseIP(s) != nil {
		return s, "", false, nil
	}
	h, p, e := net.SplitHostPort(s)
	switch {
	case e == nil:
		return h, p, true, nil
	case strings.Contains(e.Error(), "missing port"):
		return s, "", false, nil
	}
	return "", "", false, fmt.Errorf("invalid address %q: %w", s, e)
}

// Normalize returns a host:port pair, adding defPort when s has no port. A
// scheme prefix such as "minecraft://" and a trailing slash are tolerated
// because people paste addresses straight out of a browser.
func Normalize(s string, defPort int) (string, error) {
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "/")
	host, port, hasPort, err := Split(s)
	if err != nil {
		return "", err
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return "", fmt.Errorf("address %q has no host", s)
	}
	if !hasPort || port == "" {
		if defPort <= 0 {
			return "", fmt.Errorf("address %q has no port", s)
		}
		return net.JoinHostPort(host, strconv.Itoa(defPort)), nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("address %q has an invalid port %q", s, port)
	}
	return net.JoinHostPort(host, port), nil
}

// HostPort splits an already normalised address into a host and a port number.
func HostPort(s string) (string, int, error) {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, err
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", 0, fmt.Errorf("invalid port %q", port)
	}
	return host, n, nil
}
