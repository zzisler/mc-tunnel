package proxy

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
)

// fakeSOCKS5 is a minimal SOCKS5 server. dial decides where the tunnel goes, so
// a test can redirect any target to a local listener.
type fakeSOCKS5 struct {
	requireAuth bool
	report      func(target string) // optional recorder
	dial        func(target string) (net.Conn, error)
}

func startFakeSOCKS5(t *testing.T, srv fakeSOCKS5) string {
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
			go srv.handle(conn)
		}
	}()
	return ln.Addr().String()
}

func (s fakeSOCKS5) handle(conn net.Conn) {
	defer conn.Close()

	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}

	if s.requireAuth {
		conn.Write([]byte{socksVersion, socksMethodUser})
		ab := make([]byte, 2)
		if _, err := io.ReadFull(conn, ab); err != nil {
			return
		}
		user := make([]byte, int(ab[1]))
		if _, err := io.ReadFull(conn, user); err != nil {
			return
		}
		plen := make([]byte, 1)
		if _, err := io.ReadFull(conn, plen); err != nil {
			return
		}
		pass := make([]byte, int(plen[0]))
		if _, err := io.ReadFull(conn, pass); err != nil {
			return
		}
		if string(user) != "bob" || string(pass) != "secret" {
			conn.Write([]byte{1, 1})
			return
		}
		conn.Write([]byte{1, 0})
	} else {
		conn.Write([]byte{socksVersion, socksMethodNone})
	}

	req := make([]byte, 4)
	if _, err := io.ReadFull(conn, req); err != nil {
		return
	}
	var host string
	switch req[3] {
	case socksAtypIPv4:
		b := make([]byte, 4)
		io.ReadFull(conn, b)
		host = net.IP(b).String()
	case socksAtypIPv6:
		b := make([]byte, 16)
		io.ReadFull(conn, b)
		host = net.IP(b).String()
	case socksAtypDomain:
		l := make([]byte, 1)
		io.ReadFull(conn, l)
		b := make([]byte, int(l[0]))
		io.ReadFull(conn, b)
		host = string(b)
	default:
		conn.Write([]byte{5, 8, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	portBuf := make([]byte, 2)
	io.ReadFull(conn, portBuf)
	target := net.JoinHostPort(host, itoa(int(binary.BigEndian.Uint16(portBuf))))
	if s.report != nil {
		s.report(target)
	}

	remote, err := s.dial(target)
	if err != nil || remote == nil {
		conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()
	conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})

	go func() { io.Copy(remote, conn); remote.Close() }()
	io.Copy(conn, remote)
}

// startFakeHTTPProxy answers CONNECT requests with a fixed status line.
func startFakeHTTPProxy(t *testing.T, status string, requireAuth string) (addr string, lastRequest *string, tunnel chan net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	lastRequest = new(string)
	tunnel = make(chan net.Conn, 1)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				br := bufio.NewReader(conn)
				var sb strings.Builder
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					sb.WriteString(line)
					if line == "\r\n" {
						break
					}
				}
				*lastRequest = sb.String()
				if requireAuth != "" && !strings.Contains(sb.String(), requireAuth) {
					conn.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\n\r\n"))
					return
				}
				conn.Write([]byte("HTTP/1.1 " + status + "\r\n\r\n"))
				if fields := strings.Fields(status); len(fields) > 0 && fields[0] == "200" {
					tunnel <- conn
					io.Copy(conn, conn) // echo until the client goes away
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), lastRequest, tunnel
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// echoListener accepts one connection and echoes everything back.
func echoListener(t *testing.T) string {
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
				io.Copy(conn, conn)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func basicAuth(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}
