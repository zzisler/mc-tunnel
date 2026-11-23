package mcnet

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// StatusRequest builds the two packets of a server list ping: a handshake with
// next-state "status" and the status request itself.
//
// The protocol version is sent as -1, the "unknown" value pingers use, because
// every server answers a status request regardless of the client version.
func StatusRequest(host string, port int) []byte {
	var payload []byte
	payload = AppendVarInt(payload, 0)  // packet id: handshake
	payload = AppendVarInt(payload, -1) // protocol version: unknown
	payload = AppendVarInt(payload, len(host))
	payload = append(payload, host...)
	payload = append(payload, byte(port>>8), byte(port))
	payload = AppendVarInt(payload, 1) // next state: status

	out := AppendVarInt(make([]byte, 0, len(payload)+2), len(payload))
	out = append(out, payload...)
	// Status request: length 1, packet id 0, no fields.
	return append(out, 0x01, 0x00)
}

// Status is the part of the server list answer this tool cares about.
type Status struct {
	Version struct {
		Name     string `json:"name"`
		Protocol int    `json:"protocol"`
	} `json:"version"`
	Players struct {
		Online int `json:"online"`
		Max    int `json:"max"`
	} `json:"players"`
}

// maxStatusPayload guards against bogus lengths from something that is not a
// Minecraft server.
const maxStatusPayload = 1 << 20

// ReadStatusResponse decodes the status response packet.
func ReadStatusResponse(r io.Reader) (Status, error) {
	var status Status

	length, err := ReadVarInt(r)
	if err != nil {
		return status, fmt.Errorf("no status response: %w", err)
	}
	if length < 1 || length > maxStatusPayload {
		return status, fmt.Errorf("invalid status packet length %d", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return status, fmt.Errorf("short status packet: %w", err)
	}

	pos := 0
	packetID, n, err := readVarIntAt(payload[pos:])
	if err != nil {
		return status, err
	}
	pos += n
	if packetID != 0 {
		return status, fmt.Errorf("unexpected packet id %d, want 0", packetID)
	}
	jsonLen, n, err := readVarIntAt(payload[pos:])
	if err != nil {
		return status, err
	}
	pos += n
	if jsonLen < 0 || pos+jsonLen > len(payload) {
		return status, errors.New("status packet carries a truncated JSON string")
	}
	if err := json.Unmarshal(payload[pos:pos+jsonLen], &status); err != nil {
		return status, fmt.Errorf("status JSON: %w", err)
	}
	return status, nil
}
