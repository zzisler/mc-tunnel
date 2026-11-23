package mcnet

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

// parseStatusRequest splits what StatusRequest produced so the test can check
// the handshake fields and the trailing status request.
func parseStatusRequest(t *testing.T, data []byte) (host string, port int, nextState int, rest []byte) {
	t.Helper()
	r := bytes.NewReader(data)
	length, err := ReadVarInt(r)
	if err != nil {
		t.Fatalf("packet length: %v", err)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatal(err)
	}
	pos := 0
	read := func() int {
		v, err := ReadVarInt(bytes.NewReader(payload[pos:]))
		if err != nil {
			t.Fatalf("varint at %d: %v", pos, err)
		}
		pos += len(AppendVarInt(nil, v))
		return v
	}
	if id := read(); id != 0 {
		t.Fatalf("packet id = %d", id)
	}
	if proto := read(); proto != -1 {
		t.Fatalf("protocol version = %d, want -1", proto)
	}
	addrLen := read()
	host = string(payload[pos : pos+addrLen])
	pos += addrLen
	port = int(payload[pos])<<8 | int(payload[pos+1])
	pos += 2
	nextState = read()
	rest, _ = io.ReadAll(r)
	return host, port, nextState, rest
}

func TestStatusRequest(t *testing.T) {
	data := StatusRequest("mc.example.net", 25577)
	host, port, next, rest := parseStatusRequest(t, data)
	if host != "mc.example.net" || port != 25577 {
		t.Fatalf("handshake = %q:%d", host, port)
	}
	if next != 1 {
		t.Fatalf("next state = %d, want 1 (status)", next)
	}
	if !bytes.Equal(rest, []byte{0x01, 0x00}) {
		t.Fatalf("status request = % x, want 01 00", rest)
	}
}

func TestReadStatusResponse(t *testing.T) {
	payload := []byte(`{"version":{"name":"Paper 1.20.4","protocol":765},"players":{"online":3,"max":20}}`)
	var packet []byte
	packet = AppendVarInt(packet, 0)
	packet = AppendVarInt(packet, len(payload))
	packet = append(packet, payload...)
	framed := append(AppendVarInt(nil, len(packet)), packet...)

	status, err := ReadStatusResponse(bytes.NewReader(framed))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if status.Version.Name != "Paper 1.20.4" {
		t.Errorf("version = %q", status.Version.Name)
	}
	if status.Players.Online != 3 || status.Players.Max != 20 {
		t.Errorf("players = %d/%d", status.Players.Online, status.Players.Max)
	}
}

func TestReadStatusResponseRejectsGarbage(t *testing.T) {
	cases := map[string][]byte{
		"empty":       {},
		"zero length": {0x00},
		"wrong id":    {0x03, 0x01, 0x7b}, // length 3, packet id 1
	}
	for name, data := range cases {
		if _, err := ReadStatusResponse(bytes.NewReader(data)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestReadStatusResponseHandlesNestedDescription(t *testing.T) {
	// description may be an object, which must not break the version/players.
	payload, err := json.Marshal(map[string]any{
		"version":     map[string]any{"name": "Vanilla 1.21"},
		"players":     map[string]any{"online": 1, "max": 2},
		"description": map[string]any{"text": "hi", "extra": []any{map[string]any{"text": "!"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var packet []byte
	packet = AppendVarInt(packet, 0)
	packet = AppendVarInt(packet, len(payload))
	packet = append(packet, payload...)
	framed := append(AppendVarInt(nil, len(packet)), packet...)

	status, err := ReadStatusResponse(bytes.NewReader(framed))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if status.Version.Name != "Vanilla 1.21" {
		t.Fatalf("version = %q", status.Version.Name)
	}
}
