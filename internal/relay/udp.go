package relay

import (
	"context"
	"net"

	"mctunnel/internal/applog"
	"mctunnel/internal/proxy"
)

// udpTransport is the reserved slot for datagram traffic (Minecraft Bedrock,
// RakNet, QUIC).
//
// It is deliberately not implemented: Bedrock speaks UDP, and neither SOCKS5
// without UDP ASSOCIATE nor HTTP CONNECT can carry datagrams. A real
// implementation needs a SOCKS5 UDP ASSOCIATE exchange plus its own session
// table and NAT timeouts, which is a separate project, not a flag.
//
// It exists so the Manager can report "transport udp is not implemented"
// instead of quietly mis-forwarding packets.
type udpTransport struct{}

// Name implements Transport.
func (udpTransport) Name() string { return "udp" }

// Forward implements Transport.
func (udpTransport) Forward(context.Context, net.Conn, string, proxy.Dialer, *applog.Logger) error {
	return ErrUnsupported
}

var _ Transport = udpTransport{}

// UDP returns the datagram transport, which exists only as a placeholder.
func UDP() Transport { return udpTransport{} }
