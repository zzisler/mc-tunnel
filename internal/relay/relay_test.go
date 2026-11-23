package relay

import (
	"errors"
	"net"
	"testing"
)

// The game disconnecting produces platform specific messages; none of them is
// an error worth showing to the user.
func TestIsClosedRecognisesDisconnects(t *testing.T) {
	closed := []string{
		"",
		"use of closed network connection",
		"read tcp 127.0.0.1:25565->127.0.0.1:53660: wsarecv: An existing connection was forcibly closed by the remote host.",
		"write tcp 127.0.0.1:25565->127.0.0.1:53660: wsasend: An established connection was aborted by the software in your host machine.",
		"read tcp: connection reset by peer",
		"write: broken pipe",
	}
	for _, message := range closed {
		if message == "" {
			if !isClosed(nil) {
				t.Error("nil must count as closed")
			}
			continue
		}
		if !isClosed(errors.New(message)) {
			t.Errorf("%q should count as a normal close", message)
		}
	}

	real := []error{
		errors.New("dial through proxy: http proxy: authentication required"),
		errors.New("i/o timeout"),
		errors.New("socks5: host unreachable"),
	}
	for _, err := range real {
		if isClosed(err) {
			t.Errorf("%q must stay an error", err)
		}
	}

	if !isClosed(net.ErrClosed) {
		t.Error("net.ErrClosed must count as closed")
	}
}
