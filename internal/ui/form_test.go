//go:build windows

package ui

import (
	"strings"
	"testing"
)

// The form is the only place where the user's text turns into a tunnel
// configuration, so it is tested without creating a window.
func TestResolveForm(t *testing.T) {
	s, err := resolveForm(formValues{
		Server: "play.example.com",
		Proxy:  "1.2.3.4:1080",
		Kind:   "socks5",
		Port:   "25565",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.target != "play.example.com:25565" {
		t.Errorf("target = %q, want the default port appended", s.target)
	}
	if s.port != 25565 {
		t.Errorf("local port = %d", s.port)
	}
	if s.dialer == nil {
		t.Fatal("no dialer")
	}
	if !strings.HasPrefix(s.dialer.Describe(), "socks5://") {
		t.Errorf("the radio button did not pick socks5: %s", s.dialer.Describe())
	}
	if s.kind != "" {
		t.Errorf("kind should stay empty when the line has no scheme, got %q", s.kind)
	}
}

func TestResolveFormSchemeOverridesRadio(t *testing.T) {
	s, err := resolveForm(formValues{
		Server: "play.example.com:25566",
		Proxy:  "http://bob:secret@1.2.3.4:3128",
		Kind:   "socks5", // user left the default radio on SOCKS5
		Port:   "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.kind != "http" {
		t.Errorf("kind = %q, want http so the UI can update", s.kind)
	}
	if len(s.notes) == 0 {
		t.Error("the user should be told that the line decided the protocol")
	}
	if s.port != defaultPort {
		t.Errorf("local port = %d, want %d", s.port, defaultPort)
	}
	if !strings.HasPrefix(s.dialer.Describe(), "http://") {
		t.Errorf("dialer = %s", s.dialer.Describe())
	}
	if strings.Contains(s.dialer.Describe(), "secret") {
		t.Error("the password must not appear in the log line")
	}
}

func TestResolveFormHTTPFromRadio(t *testing.T) {
	s, err := resolveForm(formValues{Server: "srv:25565", Proxy: "10.0.0.1:3128", Kind: "http"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.dialer.Describe(), "http://10.0.0.1:3128") {
		t.Errorf("dialer = %s", s.dialer.Describe())
	}
}

func TestResolveFormHostPortUserPass(t *testing.T) {
	s, err := resolveForm(formValues{Server: "srv", Proxy: "1.2.3.4:1080:bob:secret", Kind: "socks5"})
	if err != nil {
		t.Fatal(err)
	}
	if s.dialer.Describe() != "socks5://bob:***@1.2.3.4:1080" {
		t.Errorf("dialer = %s", s.dialer.Describe())
	}
}

func TestResolveFormErrors(t *testing.T) {
	cases := []struct {
		name string
		in   formValues
		want string
	}{
		{"empty proxy", formValues{Server: "srv", Kind: "socks5"}, "proxy line is empty"},
		{"bad proxy", formValues{Server: "srv", Proxy: "ftp://host:21", Kind: "socks5"}, "proxy type"},
		{"empty server", formValues{Server: "", Proxy: "1.2.3.4:1080", Kind: "socks5"}, "minecraft server"},
		{"bad server port", formValues{Server: "srv:99999", Proxy: "1.2.3.4:1080", Kind: "socks5"}, "minecraft server"},
		{"bad local port", formValues{Server: "srv", Proxy: "1.2.3.4:1080", Kind: "socks5", Port: "0"}, "local port"},
		{"text local port", formValues{Server: "srv", Proxy: "1.2.3.4:1080", Kind: "socks5", Port: "abc"}, "local port"},
	}
	for _, tc := range cases {
		_, err := resolveForm(tc.in)
		if err == nil {
			t.Errorf("%s: expected an error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not mention %q", tc.name, err, tc.want)
		}
	}
}

func TestResolveFormIPv6Server(t *testing.T) {
	s, err := resolveForm(formValues{Server: "[2001:db8::1]:25565", Proxy: "1.2.3.4:1080", Kind: "socks5"})
	if err != nil {
		t.Fatal(err)
	}
	if s.target != "[2001:db8::1]:25565" {
		t.Fatalf("target = %q", s.target)
	}
}

func TestParseLocalPort(t *testing.T) {
	if port, err := parseLocalPort("  "); err != nil || port != defaultPort {
		t.Fatalf("empty port = %d, %v", port, err)
	}
	if port, err := parseLocalPort(" 25570 "); err != nil || port != 25570 {
		t.Fatalf("padded port = %d, %v", port, err)
	}
	for _, bad := range []string{"-1", "0", "65536", "25 65", "port"} {
		if _, err := parseLocalPort(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}
