package proxy

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"mctunnel/internal/addr"
)

// Parse turns one user supplied proxy line into a Config, so the GUI needs a
// single text field instead of host, port, user and password separately.
//
// Accepted shapes:
//
//	socks5://user:pass@host:port
//	http://user:pass@host:8080
//	user:pass@host:port
//	host:port:user:pass
//	host:port
//
// A scheme always wins over the protocol selected in the UI. When the line has
// no scheme, Config.Kind is left empty and the caller decides.
func Parse(line string) (Config, error) {
	line = strings.TrimSpace(line)
	line = strings.Trim(line, `"'`)
	if line == "" {
		return Config{}, errors.New("proxy line is empty")
	}

	if i := strings.Index(line, "://"); i >= 0 {
		return parseURL(line)
	}

	// host:port:user:pass — a very common copy-paste format for proxy sellers.
	if strings.Count(line, ":") == 3 && !strings.Contains(line, "@") {
		parts := strings.Split(line, ":")
		if _, err := strconv.Atoi(parts[1]); err == nil {
			return finish(Config{User: parts[2], Pass: parts[3]}, parts[0]+":"+parts[1], line)
		}
	}

	// user:pass@host:port
	if i := strings.LastIndex(line, "@"); i >= 0 {
		user, pass, found := strings.Cut(line[:i], ":")
		if !found {
			user, pass = line[:i], ""
		}
		return finish(Config{User: user, Pass: pass}, line[i+1:], line)
	}

	return finish(Config{}, line, line)
}

func parseURL(line string) (Config, error) {
	u, err := url.Parse(line)
	if err != nil {
		return Config{}, fmt.Errorf("proxy line %q: %w", line, err)
	}
	kind, err := ParseKind(u.Scheme)
	if err != nil {
		return Config{}, err
	}
	if path := strings.Trim(u.Path, "/"); path != "" {
		return Config{}, fmt.Errorf("proxy line %q: unexpected path %q", line, u.Path)
	}
	cfg := Config{Kind: kind}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Pass, _ = u.User.Password()
	}
	return finish(cfg, u.Host, line)
}

func finish(cfg Config, hostport, original string) (Config, error) {
	hostport = strings.TrimSpace(strings.TrimSuffix(hostport, "/"))
	if hostport == "" {
		return Config{}, fmt.Errorf("proxy line %q has no address", original)
	}
	defPort := 0
	if cfg.Kind != "" {
		defPort = cfg.Kind.DefaultPort()
	}
	normalised, err := normalise(hostport, defPort)
	if err != nil {
		return Config{}, fmt.Errorf("proxy line %q: %w", original, err)
	}
	cfg.Addr = normalised
	return cfg, nil
}

// normalise is addr.Normalize with proxy specific error wording.
func normalise(s string, defPort int) (string, error) {
	host, port, hasPort, err := addr.Split(s)
	if err != nil {
		return "", err
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return "", errors.New("no host")
	}
	if !hasPort || port == "" {
		if defPort <= 0 {
			return "", errors.New("no port")
		}
		port = strconv.Itoa(defPort)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("invalid port %q", port)
	}
	return net.JoinHostPort(host, port), nil
}
