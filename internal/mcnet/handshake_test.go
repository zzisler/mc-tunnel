package mcnet

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// buildHandshake assembles a modern handshake packet the way a client would.
func buildHandshake(protocol int, host string, port int, nextState int) []byte {
	var payload []byte
	payload = AppendVarInt(payload, 0) // packet id
	payload = AppendVarInt(payload, protocol)
	payload = AppendVarInt(payload, len(host))
	payload = append(payload, host...)
	payload = append(payload, byte(port>>8), byte(port))
	payload = AppendVarInt(payload, nextState)
	out := AppendVarInt(nil, len(payload))
	return append(out, payload...)
}

type handshake struct {
	Protocol int
	Host     string
	Port     int
	Next     int
	Rest     []byte
}

func parseHandshake(t *testing.T, data []byte) handshake {
	t.Helper()
	r := bytes.NewReader(data)
	length, err := ReadVarInt(r)
	if err != nil {
		t.Fatalf("packet length: %v", err)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatalf("packet payload: %v", err)
	}
	pos := 0
	next := func() int {
		v, err := ReadVarInt(bytes.NewReader(payload[pos:]))
		if err != nil {
			t.Fatalf("varint at %d: %v", pos, err)
		}
		// Recover the encoded size by re-encoding.
		pos += len(AppendVarInt(nil, v))
		return v
	}
	if id := next(); id != 0 {
		t.Fatalf("packet id = %d, want 0", id)
	}
	proto := next()
	addrLen := next()
	host := string(payload[pos : pos+addrLen])
	pos += addrLen
	port := int(binary.BigEndian.Uint16(payload[pos : pos+2]))
	pos += 2
	state := next()
	rest, _ := io.ReadAll(r)
	return handshake{Protocol: proto, Host: host, Port: port, Next: state, Rest: rest}
}

// forward runs ForwardStream with an in-memory game side and records
// everything the tunnel would send towards the server.
func forward(t *testing.T, input []byte, host string, port int) []byte {
	t.Helper()

	// The tunnel writes here; the test reads the result from the other end.
	toServer, serverSide := net.Pipe()
	// The test pretends to be the game and writes to the tunnel here.
	fromClient, clientSide := net.Pipe()

	collected := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(serverSide)
		collected <- data
	}()
	go func() {
		_ = ForwardStream(toServer, fromClient, host, port)
		toServer.Close()
	}()
	go func() {
		_, _ = clientSide.Write(input)
		clientSide.Close()
	}()

	select {
	case out := <-collected:
		return out
	case <-time.After(3 * time.Second):
		t.Fatal("ForwardStream blocked")
		return nil
	}
}

func TestForwardStreamRewritesAddress(t *testing.T) {
	login := []byte{0x10, 0x00, 0x00, 0x00} // pretend to be the next packet
	input := append(buildHandshake(767, "127.0.0.1", 25565, 2), login...)

	out := forward(t, input, "play.example.com", 25566)
	got := parseHandshake(t, out)
	if got.Host != "play.example.com" {
		t.Errorf("host = %q, want play.example.com", got.Host)
	}
	if got.Port != 25566 {
		t.Errorf("port = %d, want 25566", got.Port)
	}
	if got.Protocol != 767 {
		t.Errorf("protocol = %d, want 767", got.Protocol)
	}
	if got.Next != 2 {
		t.Errorf("next state = %d, want 2", got.Next)
	}
	if !bytes.Equal(got.Rest, login) {
		t.Errorf("trailing packet was not preserved: %x", got.Rest)
	}
}

func TestForwardStreamShorterHost(t *testing.T) {
	input := buildHandshake(47, "a-very-long-placeholder-name.example.lan", 25565, 1)
	out := forward(t, input, "a.io", 25565)
	got := parseHandshake(t, out)
	if got.Host != "a.io" {
		t.Fatalf("host = %q", got.Host)
	}
	if len(out) >= len(input) {
		t.Fatalf("expected a shorter packet, %d >= %d", len(out), len(input))
	}
}

func TestForwardStreamKeepsIPv6Host(t *testing.T) {
	input := buildHandshake(767, "localhost", 25565, 2)
	out := forward(t, input, "2001:db8::1", 25565)
	got := parseHandshake(t, out)
	if got.Host != "2001:db8::1" {
		t.Fatalf("host = %q", got.Host)
	}
}

// A non-Minecraft stream must reach the server byte for byte.
func TestForwardStreamPassesJunkThrough(t *testing.T) {
	junk := []byte("SSH-2.0-OpenSSH_9.0\r\n\x00\x01\x02binary-ish data")
	out := forward(t, junk, "play.example.com", 25565)
	if !bytes.Equal(out, junk) {
		t.Fatalf("junk was modified:\n got %x\nwant %x", out, junk)
	}
}

// The pre-1.7 server list ping starts with 0xFE and carries no hostname. The
// tunnel must forward it instead of swallowing it.
func TestForwardStreamLegacyPing(t *testing.T) {
	ping := []byte{0xFE, 0x01, 0xFA, 0x00, 0x0B, 'h', 'i'}
	out := forward(t, ping, "play.example.com", 25565)
	if !bytes.Equal(out, ping) {
		t.Fatalf("legacy ping was modified:\n got %x\nwant %x", out, ping)
	}
}

func TestForwardStreamEmptyConnection(t *testing.T) {
	fromClient, clientSide := net.Pipe()
	defer fromClient.Close()
	clientSide.Close()
	if err := ForwardStream(io.Discard, fromClient, "host", 25565); err != nil {
		t.Fatalf("empty stream: %v", err)
	}
}

func TestVarIntRoundTrip(t *testing.T) {
	for _, value := range []int{0, 1, 127, 128, 255, 2097151, 2097152, 1<<31 - 1, -1, -2147483648} {
		encoded := AppendVarInt(nil, value)
		decoded, err := ReadVarInt(bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("%d: %v", value, err)
		}
		if decoded != value {
			t.Fatalf("%d round-tripped to %d", value, decoded)
		}
	}
}

func TestVarIntWireFormat(t *testing.T) {
	// -1 has to be the five byte form the protocol expects.
	if got := AppendVarInt(nil, -1); !bytes.Equal(got, []byte{0xff, 0xff, 0xff, 0xff, 0x0f}) {
		t.Fatalf("AppendVarInt(-1) = % x", got)
	}
	if got := AppendVarInt(nil, 255); !bytes.Equal(got, []byte{0xff, 0x01}) {
		t.Fatalf("AppendVarInt(255) = % x", got)
	}
}
