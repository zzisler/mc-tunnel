package serverprobe

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"mctunnel/internal/mcnet"
	"mctunnel/internal/proxy"
)

// fakeSOCKS5 tunnels every request to target, whatever was asked for.
func fakeSOCKS5(t *testing.T, target string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				head := make([]byte, 2)
				if _, err := io.ReadFull(conn, head); err != nil {
					return
				}
				if _, err := io.ReadFull(conn, make([]byte, int(head[1]))); err != nil {
					return
				}
				conn.Write([]byte{5, 0})
				req := make([]byte, 4)
				if _, err := io.ReadFull(conn, req); err != nil {
					return
				}
				skipAddress(conn, req[3])
				io.ReadFull(conn, make([]byte, 2))
				remote, err := net.Dial("tcp", target)
				if err != nil {
					conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				defer remote.Close()
				conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				go func() { io.Copy(remote, conn); remote.Close() }()
				io.Copy(conn, remote)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func skipAddress(conn net.Conn, atyp byte) {
	switch atyp {
	case 1:
		io.ReadFull(conn, make([]byte, 4))
	case 4:
		io.ReadFull(conn, make([]byte, 16))
	case 3:
		l := make([]byte, 1)
		io.ReadFull(conn, l)
		io.ReadFull(conn, make([]byte, int(l[0])))
	}
}

// fakeMinecraft answers a server list ping with a fixed status.
func fakeMinecraft(t *testing.T, statusJSON string, reply bool) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				if _, err := mcnet.ReadVarInt(conn); err != nil {
					return
				}
				if !reply {
					// Accept the bytes and stay silent: the case this probe exists
					// to explain.
					io.Copy(io.Discard, conn)
					return
				}
				var packet []byte
				packet = mcnet.AppendVarInt(packet, 0)
				packet = mcnet.AppendVarInt(packet, len(statusJSON))
				packet = append(packet, statusJSON...)
				conn.Write(append(mcnet.AppendVarInt(nil, len(packet)), packet...))
				io.Copy(io.Discard, conn)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func dialer(t *testing.T, addr string) proxy.Dialer {
	t.Helper()
	d, err := proxy.New(proxy.Config{Kind: proxy.SOCKS5, Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestStatusReportsServer(t *testing.T) {
	server := fakeMinecraft(t, `{"version":{"name":"Paper 1.20.4"},"players":{"online":3,"max":20}}`, true)
	d := dialer(t, fakeSOCKS5(t, server))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := Status(ctx, d, "mc.example.net:25577")
	if !result.OK() {
		t.Fatalf("probe failed: %v", result.Err)
	}
	if result.Version != "Paper 1.20.4" || result.Online != 3 || result.Slots != 20 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.String(), "3/20") {
		t.Fatalf("String() = %q", result.String())
	}
	if result.Latency <= 0 {
		t.Error("latency was not measured")
	}
}

// The real-world failure: the tunnel opens, bytes go out, nothing comes back.
func TestStatusSilentServer(t *testing.T) {
	server := fakeMinecraft(t, "", false)
	d := dialer(t, fakeSOCKS5(t, server))

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	result := Status(ctx, d, "mc.example.net:25577")
	if result.OK() {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(result.Err.Error(), "no answer") {
		t.Fatalf("unhelpful error: %v", result.Err)
	}
	// The message must name the timeout that was configured, not whatever is
	// left of it after the deadline already passed.
	if strings.Contains(result.Err.Error(), "within 0s") {
		t.Fatalf("timeout is reported as 0s: %v", result.Err)
	}
}

func TestStatusUnreachableProxy(t *testing.T) {
	d, err := proxy.New(proxy.Config{Kind: proxy.HTTP, Addr: "127.0.0.1:1", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := Status(ctx, d, "mc.example.net:25577")
	if result.OK() {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(result.Err.Error(), "tunnel") {
		t.Fatalf("error should mention the tunnel: %v", result.Err)
	}
}

func TestStatusBadTarget(t *testing.T) {
	d, err := proxy.New(proxy.Config{Kind: proxy.SOCKS5, Addr: "127.0.0.1:1080"})
	if err != nil {
		t.Fatal(err)
	}
	if result := Status(context.Background(), d, "no-port"); result.OK() {
		t.Fatal("expected a failure for a target without a port")
	}
}
