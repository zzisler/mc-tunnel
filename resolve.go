//go:build windows

package main

import (
	"fmt"

	"mctunnel/internal/addr"
	"mctunnel/internal/proxy"
	"mctunnel/internal/settings"
)

// resolveTarget turns the two server fields into the address the tunnel dials
// and the local port it listens on. It mirrors the validation in internal/ui
// so both front ends behave the same; the Win32 one is on its way out, and then
// this copy is the only one left.
func resolveTarget(server string, port int) (string, int, error) {
	target, err := addr.Normalize(server, settings.DefaultPort)
	if err != nil {
		return "", 0, fmt.Errorf("minecraft server: %w", err)
	}
	if port == 0 {
		port = settings.DefaultPort
	}
	if port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("local port %d is not a valid port", port)
	}
	return target, port, nil
}

// resolveDialer parses the proxy line and settles the protocol: a scheme in the
// line wins over the radio buttons, and the UI is told when that happened.
func resolveDialer(proxyText, kind string) (proxy.Dialer, proxy.Kind, []string, error) {
	cfg, err := proxy.Parse(proxyText)
	if err != nil {
		return nil, "", nil, err
	}
	selected, err := proxy.ParseKind(kind)
	if err != nil {
		return nil, "", nil, err
	}

	var forced proxy.Kind
	var notes []string
	switch {
	case cfg.Kind == "":
		// No scheme in the line: the radio buttons decide.
		cfg.Kind = selected
	case cfg.Kind != selected:
		forced = cfg.Kind
		notes = append(notes, fmt.Sprintf("proxy type taken from the line: %s", cfg.Kind))
	}

	dialer, err := proxy.New(cfg)
	if err != nil {
		return nil, "", nil, err
	}
	return dialer, forced, notes, nil
}
