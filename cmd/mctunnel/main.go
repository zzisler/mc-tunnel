//go:build windows

// Command mctunnel is a local Minecraft Java Edition tunnel: the game connects
// to 127.0.0.1:<port> and every session is forwarded through a SOCKS5 or HTTP
// CONNECT proxy to the real server.
package main

import "mctunnel/internal/ui"

func main() {
	ui.Run()
}
