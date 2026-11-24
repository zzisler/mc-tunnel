package proxy

import (
	"bufio"
	"bytes"
	"net"
	"strings"
	"testing"
	"time"
)

// startNastyHTTPProxy accepts one connection, reads the CONNECT request and then
// runs reply against it. Tests use it to imitate a proxy that misbehaves after
// the request.
func startNastyHTTPProxy(t *testing.T, reply func(conn net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\r\n" {
				break
			}
		}
		reply(conn)
	}()
	return ln.Addr().String()
}

// A proxy that answers with a line that never ends must not make the client read
// forever: the read is capped by the buffer size.
func TestHTTPProxyRejectsEndlessResponseLine(t *testing.T) {
	addr := startNastyHTTPProxy(t, func(conn net.Conn) {
		conn.Write([]byte("HTTP/1.1 200 Connection established but"))
		chunk := bytes.Repeat([]byte("A"), 4<<10)
		for i := 0; i < 16; i++ {
			if _, err := conn.Write(chunk); err != nil {
				return
			}
		}
		time.Sleep(2 * time.Second)
	})

	d, err := New(Config{Kind: HTTP, Addr: addr, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := d.Dial(testContext(t), "mc.example.net:25565")
	if err == nil {
		conn.Close()
		t.Fatal("the endless status line was accepted")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Fatalf("error = %q, want a line length complaint", err)
	}
}

// A proxy that streams headers forever is cut off as well.
func TestHTTPProxyRejectsEndlessHeaders(t *testing.T) {
	addr := startNastyHTTPProxy(t, func(conn net.Conn) {
		conn.Write([]byte("HTTP/1.1 200 Connection established\r\n"))
		for i := 0; i < maxHTTPHeaders+50; i++ {
			if _, err := conn.Write([]byte("X-Filler: 0123456789\r\n")); err != nil {
				return
			}
		}
		time.Sleep(2 * time.Second)
	})

	d, err := New(Config{Kind: HTTP, Addr: addr, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := d.Dial(testContext(t), "mc.example.net:25565")
	if err == nil {
		conn.Close()
		t.Fatal("the endless header list was accepted")
	}
	if !strings.Contains(err.Error(), "headers") {
		t.Fatalf("error = %q, want a header count complaint", err)
	}
}
