// Package mcnet knows the Minecraft Java Edition wire details that a
// transparent TCP tunnel has to touch: the server address inside the first
// handshake packet.
package mcnet

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"time"
)

const (
	// legacyPing is the first byte of the pre-1.7 server list ping. It carries
	// no hostname, so there is nothing to rewrite and the stream is passed
	// through untouched.
	legacyPing byte = 0xFE

	// maxHandshake guards against bogus lengths on connections that are not
	// Minecraft at all. A real handshake is a few hundred bytes (the address is
	// capped at 255), so 4 KiB is generous and keeps the allocation small.
	maxHandshake = 4 << 10

	// maxHostLength is the limit the protocol itself uses for the address.
	maxHostLength = 255
)

// ForwardStream rewrites the server address inside the first Minecraft
// handshake read from src, then streams the rest of the connection to dst.
//
// If the first bytes are not a modern handshake the data is forwarded
// unchanged, so the tunnel never corrupts traffic it does not understand.
func ForwardStream(dst io.Writer, src io.Reader, host string, port int) error {
	br := bufio.NewReaderSize(src, 8<<10)
	first, err := br.Peek(1)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	if first[0] == legacyPing {
		clearReadDeadline(src)
		_, err := io.Copy(dst, br)
		return err
	}

	// Everything consumed while inspecting the packet is teed into raw so a
	// non-Minecraft connection can still be forwarded byte for byte.
	var raw bytes.Buffer
	tee := io.TeeReader(br, &raw)

	// pass gives up on rewriting and forwards the stream verbatim. The
	// first-packet deadline set by the caller is dropped first: from here on the
	// session may idle for as long as the game does.
	pass := func() error {
		clearReadDeadline(src)
		return passthrough(dst, &raw, br)
	}

	length, err := ReadVarInt(tee)
	if err != nil || length < 1 || length > maxHandshake {
		return pass()
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(tee, payload); err != nil {
		return pass()
	}
	rewritten, ok := rewriteHandshake(payload, host, port)
	if !ok {
		return pass()
	}
	clearReadDeadline(src)

	out := AppendVarInt(make([]byte, 0, len(rewritten)+5), len(rewritten))
	out = append(out, rewritten...)
	if _, err := dst.Write(out); err != nil {
		return err
	}
	_, err = io.Copy(dst, br)
	return err
}

// passthrough replays the bytes consumed during inspection and then copies the
// rest of the stream verbatim.
func passthrough(dst io.Writer, raw *bytes.Buffer, rest io.Reader) error {
	if raw.Len() > 0 {
		if _, err := dst.Write(raw.Bytes()); err != nil {
			return err
		}
	}
	_, err := io.Copy(dst, rest)
	return err
}

// clearReadDeadline drops a read deadline that the caller set to bound the first
// packet, so the rest of the session is not limited by it.
func clearReadDeadline(r io.Reader) {
	if d, ok := r.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = d.SetReadDeadline(time.Time{})
	}
}

// rewriteHandshake rebuilds a handshake packet with a different host and port.
// It reports false when payload does not look like a handshake, in which case
// the caller forwards the original bytes.
func rewriteHandshake(payload []byte, host string, port int) ([]byte, bool) {
	if host == "" || len(host) > maxHostLength || port < 1 || port > 65535 {
		return nil, false
	}
	pos := 0
	packetID, n, err := readVarIntAt(payload[pos:])
	if err != nil || packetID != 0 {
		return nil, false
	}
	pos += n
	// Protocol version: kept as the client sent it.
	if _, n, err = readVarIntAt(payload[pos:]); err != nil {
		return nil, false
	}
	pos += n
	addrLen, n, err := readVarIntAt(payload[pos:])
	if err != nil || addrLen < 0 || addrLen > maxHostLength {
		return nil, false
	}
	pos += n
	// Old address, old port, then the next-state VarInt must all be present.
	if pos+addrLen+3 > len(payload) {
		return nil, false
	}

	out := make([]byte, 0, len(payload)+len(host))
	out = append(out, payload[:pos-n]...) // packet id + protocol version
	out = AppendVarInt(out, len(host))
	out = append(out, host...)
	out = append(out, byte(port>>8), byte(port))
	out = append(out, payload[pos+addrLen+2:]...) // next state and anything else
	return out, true
}

// ReadVarInt reads one Minecraft protocol VarInt. The value is signed, so -1
// (which the protocol uses as "unknown protocol version") decodes to -1.
func ReadVarInt(r io.Reader) (int, error) {
	value := uint32(0)
	var b [1]byte
	for i := 0; i < 5; i++ {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		value |= uint32(b[0]&0x7f) << (7 * uint(i))
		if b[0]&0x80 == 0 {
			return int(int32(value)), nil
		}
	}
	return 0, errors.New("mcnet: invalid VarInt")
}

// AppendVarInt appends one Minecraft protocol VarInt to dst.
func AppendVarInt(dst []byte, value int) []byte {
	u := uint32(int32(value))
	for {
		if u&^0x7f == 0 {
			return append(dst, byte(u))
		}
		dst = append(dst, byte(u&0x7f)|0x80)
		u >>= 7
	}
}

func readVarIntAt(b []byte) (int, int, error) {
	value := uint32(0)
	for i := 0; i < 5; i++ {
		if i >= len(b) {
			return 0, 0, io.ErrUnexpectedEOF
		}
		value |= uint32(b[i]&0x7f) << (7 * uint(i))
		if b[i]&0x80 == 0 {
			return int(int32(value)), i + 1, nil
		}
	}
	return 0, 0, errors.New("mcnet: invalid VarInt")
}
