//go:build windows

package ui

import (
	"strings"
	"testing"

	"mctunnel/internal/proxy"
	"mctunnel/internal/settings"
)

// The config that gets written must reproduce exactly the proxy the user had.
func TestSettingsFromFormRoundTrip(t *testing.T) {
	in := formValues{
		Server: "  mc.example.net  ",
		Proxy:  "http://bob:p%40ss@1.2.3.4:8080",
		Kind:   "socks5", // the line says http, and the line wins
		Port:   "25570",
	}
	stored, err := settingsFromForm(in, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.Server != "mc.example.net" {
		t.Errorf("server = %q, want the trimmed value", stored.Server)
	}
	if stored.ProxyKind != "http" {
		t.Errorf("kind = %q, want http from the line", stored.ProxyKind)
	}
	if stored.ProxyUser != "bob" || stored.ProxyPassword != "p@ss" {
		t.Errorf("credentials = %q / %q", stored.ProxyUser, stored.ProxyPassword)
	}
	if stored.LocalPort != 25570 || !stored.LogsVisible {
		t.Errorf("port/logs = %d / %v", stored.LocalPort, stored.LogsVisible)
	}

	// Rebuilding the line must give back the same proxy config.
	line := proxyLineFromSettings(stored)
	cfg, err := proxy.Parse(line)
	if err != nil {
		t.Fatalf("the rebuilt line %q does not parse: %v", line, err)
	}
	if cfg.Kind != proxy.HTTP || cfg.Addr != "1.2.3.4:8080" ||
		cfg.User != "bob" || cfg.Pass != "p@ss" {
		t.Fatalf("rebuilt line %q gave %+v", line, cfg)
	}
}

func TestSettingsFromFormDefaultsPort(t *testing.T) {
	stored, err := settingsFromForm(formValues{
		Server: "srv",
		Proxy:  "1.2.3.4:1080",
		Kind:   "socks5",
		Port:   "",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LocalPort != defaultPort {
		t.Fatalf("port = %d, want %d", stored.LocalPort, defaultPort)
	}
	if stored.ProxyKind != "socks5" {
		t.Fatalf("kind = %q, want the radio button value", stored.ProxyKind)
	}
}

func TestSettingsFromFormErrors(t *testing.T) {
	cases := map[string]formValues{
		"empty proxy":  {Server: "srv", Kind: "socks5"},
		"broken proxy": {Server: "srv", Proxy: "ftp://host:21", Kind: "socks5"},
		"broken port":  {Server: "srv", Proxy: "1.2.3.4:1080", Kind: "socks5", Port: "abc"},
	}
	for name, in := range cases {
		if _, err := settingsFromForm(in, false); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// An empty proxy field must not produce a config file at all.
func TestEmptyFormIsNotPersisted(t *testing.T) {
	if _, err := settingsFromForm(formValues{Kind: "socks5", Port: "25565"}, false); err == nil {
		t.Fatal("an empty proxy line must be rejected")
	}
}

func TestProxyLineFromEmptySettings(t *testing.T) {
	if got := proxyLineFromSettings(settings.Settings{}); got != "" {
		t.Fatalf("line = %q, want empty", got)
	}
}

func TestProxyLineKeepsSpecialCharacters(t *testing.T) {
	stored := settings.Settings{
		ProxyKind:     "http",
		ProxyAddr:     "198.51.100.7:39584",
		ProxyUser:     "user",
		ProxyPassword: "Pa:ss@w#rd/1+2",
	}
	cfg, err := proxy.Parse(proxyLineFromSettings(stored))
	if err != nil {
		t.Fatalf("the line does not parse: %v", err)
	}
	if cfg.Pass != stored.ProxyPassword {
		t.Fatalf("password round-tripped to %q", cfg.Pass)
	}
	if strings.Contains(proxyLineFromSettings(stored), "Pa:ss@w#rd/1+2") {
		t.Fatal("special characters must be escaped in the line")
	}
}
