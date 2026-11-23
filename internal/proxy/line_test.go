package proxy

import "testing"

// Line and Parse have to be inverses, otherwise the saved proxy line would
// drift away from what the user typed.
func TestLineRoundTrip(t *testing.T) {
	cases := []Config{
		{Kind: SOCKS5, Addr: "1.2.3.4:1080"},
		{Kind: HTTP, Addr: "proxy.example.net:8080"},
		{Kind: SOCKS5, Addr: "1.2.3.4:1080", User: "bob", Pass: "secret"},
		{Kind: HTTP, Addr: "1.2.3.4:8080", User: "user", Pass: "p@ss:word/with?chars#and%20more"},
		{Kind: SOCKS5, Addr: "[2001:db8::1]:1080", User: "u", Pass: "p"},
		{Kind: HTTP, Addr: "1.2.3.4:8080", User: "user only"},
	}
	for _, want := range cases {
		line := want.Line()
		got, err := Parse(line)
		if err != nil {
			t.Errorf("%+v -> %q: %v", want, line, err)
			continue
		}
		if got != want {
			t.Errorf("%+v -> %q -> %+v", want, line, got)
		}
	}
}

func TestLineWithoutScheme(t *testing.T) {
	if got := (Config{Addr: "1.2.3.4:1080"}).Line(); got != "1.2.3.4:1080" {
		t.Fatalf("Line() = %q", got)
	}
}
