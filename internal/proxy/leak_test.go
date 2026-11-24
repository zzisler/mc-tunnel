package proxy

import (
	"strings"
	"testing"
)

// A rejected proxy line must not echo the password back. The message ends up in
// the GUI log and in the file written by the "save log" button, so a leak here
// survives every other attempt to keep the password off disk.
func TestParseErrorsDoNotLeakThePassword(t *testing.T) {
	// A password with '#' or '@' inside makes url.Parse read the rest of the
	// line as a fragment, so the URL cases use a password that keeps the line a
	// URL; the separator based cases cover the other characters.
	const urlPass = "Pa:ss+wo1"
	const rawPass = "Pa:ss@w#rd+1"
	lines := []string{
		"http://user:" + urlPass + "@host.tld:99999",        // invalid port
		"socks5://user:" + urlPass + "@host.tld:1080/extra", // unexpected path
		"user:" + urlPass + "@host.tld:99999",               // no scheme
		"http://user:" + urlPass + "@host.tld:80%zz",        // url.Parse rejects it
		"user:" + rawPass + "@host.tld:99999",               // separators in the password
		"host.tld:1080:user:" + rawPass,                     // four parts, bad address
	}
	for _, line := range lines {
		_, err := Parse(line)
		if err == nil {
			t.Errorf("Parse(%q) accepted the line, expected an error", line)
			continue
		}
		for _, secret := range []string{urlPass, rawPass} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("password leaked in %q", err.Error())
			}
		}
	}
}

// maskLine is the one place that decides what an error may show.
func TestMaskLine(t *testing.T) {
	cases := map[string]string{
		"socks5://user:pass@host.tld:1080":     "socks5://user:***@host.tld:1080",
		"http://user:pass@host.tld:8080":       "http://user:***@host.tld:8080",
		"user:pass@host.tld:1080":              "user:***@host.tld:1080",
		"host.tld:1080:user:pass":              "host.tld:1080:user:***",
		"user@host.tld:1080":                   "user:***@host.tld:1080",
		"socks5://host.tld:1080":               "socks5://host.tld:1080",
		"host.tld:1080":                        "host.tld:1080",
	}
	for in, want := range cases {
		if got := maskLine(in); got != want {
			t.Errorf("maskLine(%q) = %q, want %q", in, got, want)
		}
	}
}
