package proxy

import (
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	cases := []struct {
		line string
		want Config
	}{
		{"socks5://1.2.3.4:1080", Config{Kind: SOCKS5, Addr: "1.2.3.4:1080"}},
		{"socks5h://proxy.example.net:1080", Config{Kind: SOCKS5, Addr: "proxy.example.net:1080"}},
		{"SOCKS5://user:pass@1.2.3.4:1080", Config{Kind: SOCKS5, Addr: "1.2.3.4:1080", User: "user", Pass: "pass"}},
		{"http://1.2.3.4:8080", Config{Kind: HTTP, Addr: "1.2.3.4:8080"}},
		{"https://user:p%40ss@host.tld:8443", Config{Kind: HTTP, Addr: "host.tld:8443", User: "user", Pass: "p@ss"}},
		{"socks5://host.tld", Config{Kind: SOCKS5, Addr: "host.tld:1080"}},
		{"http://host.tld", Config{Kind: HTTP, Addr: "host.tld:8080"}},
		{"http://host.tld:8080/", Config{Kind: HTTP, Addr: "host.tld:8080"}},
		{"1.2.3.4:1080", Config{Addr: "1.2.3.4:1080"}},
		{"user:pass@1.2.3.4:1080", Config{Addr: "1.2.3.4:1080", User: "user", Pass: "pass"}},
		{"1.2.3.4:1080:user:pass", Config{Addr: "1.2.3.4:1080", User: "user", Pass: "pass"}},
		{"[2001:db8::1]:1080", Config{Addr: "[2001:db8::1]:1080"}},
		{"socks5://[2001:db8::1]:1080", Config{Kind: SOCKS5, Addr: "[2001:db8::1]:1080"}},
		{`  "http://u:p@1.1.1.1:3128"  `, Config{Kind: HTTP, Addr: "1.1.1.1:3128", User: "u", Pass: "p"}},
		{"user@host.tld:1080", Config{Addr: "host.tld:1080", User: "user"}},
	}
	for _, tc := range cases {
		got, err := Parse(tc.line)
		if err != nil {
			t.Errorf("%q: unexpected error %v", tc.line, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q:\n got %+v\nwant %+v", tc.line, got, tc.want)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"ftp://host:21",
		"socks4://host:1080",
		"host.tld:notaport",
		"host.tld:70000",
		"http://host.tld:8080/path",
		":1080",
		"socks5://",
	}
	for _, line := range cases {
		if cfg, err := Parse(line); err == nil {
			t.Errorf("%q: expected an error, got %+v", line, cfg)
		}
	}
}

// A line without a scheme must not decide the protocol: the caller keeps the
// choice from the UI.
func TestParseWithoutSchemeLeavesKindEmpty(t *testing.T) {
	cfg, err := Parse("1.2.3.4:1080")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != "" {
		t.Fatalf("kind = %q, want empty", cfg.Kind)
	}
}

func TestParseKind(t *testing.T) {
	for _, name := range []string{"socks5", "SOCKS5", "socks5h", "socks", "5"} {
		if kind, err := ParseKind(name); err != nil || kind != SOCKS5 {
			t.Errorf("%q -> %q, %v", name, kind, err)
		}
	}
	for _, name := range []string{"http", "HTTPS", "connect", "http-connect"} {
		if kind, err := ParseKind(name); err != nil || kind != HTTP {
			t.Errorf("%q -> %q, %v", name, kind, err)
		}
	}
	for _, name := range []string{"", "socks4", "vless", "shadowsocks"} {
		if _, err := ParseKind(name); err == nil {
			t.Errorf("%q: expected an error", name)
		}
	}
}

func TestNewValidatesAddress(t *testing.T) {
	if _, err := New(Config{Kind: SOCKS5, Addr: "host.tld"}); err != nil {
		t.Fatalf("default port should be applied: %v", err)
	}
	if _, err := New(Config{Kind: "quic", Addr: "host.tld:1"}); err == nil {
		t.Fatal("expected an error for an unknown kind")
	}
	if _, err := New(Config{Kind: SOCKS5, Addr: ""}); err == nil {
		t.Fatal("expected an error for an empty address")
	}
}

func TestDescribeMasksPassword(t *testing.T) {
	d, err := New(Config{Kind: SOCKS5, Addr: "1.2.3.4:1080", User: "bob", Pass: "hunter2"})
	if err != nil {
		t.Fatal(err)
	}
	got := d.Describe()
	want := "socks5://bob:***@1.2.3.4:1080"
	if got != want {
		t.Fatalf("Describe() = %q, want %q", got, want)
	}
	if strings.Contains(got, "hunter2") {
		t.Fatalf("password leaked in %q", got)
	}
}
