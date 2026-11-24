<p align="center">
  <img src="docs/design/appicon/icon-256.png" width="96" alt="MC Tunnel">
</p>

<h1 align="center">MC Tunnel</h1>

<p align="center">
  A local tunnel for Minecraft Java: the game connects to <code>127.0.0.1:25565</code>,
  while the traffic goes to the real server through a SOCKS5 or HTTP CONNECT proxy.
</p>

<p align="center">
  <img alt="Windows" src="https://img.shields.io/badge/platform-Windows%20x64%20%7C%20ARM64-0078D6?logo=windows&logoColor=white">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white">
  <img alt="UI" src="https://img.shields.io/badge/UI-Wails%20%2B%20WebView2-DF0000">
  <img alt="License" src="https://img.shields.io/badge/license-MIT-3fb950">
</p>

<p align="center">
  <b>Русская версия: <a href="README.ru.md">README.ru.md</a></b>
</p>

---

## Why

A proxy sees Minecraft traffic as ordinary TCP. MC Tunnel puts a small local
middleman between the game and the proxy: the game believes it is playing on
`127.0.0.1:25565`, while the connection actually leaves through the proxy to the
server you want. Useful when the server is unreachable directly and a proxy is
all you have.

```
Minecraft ──TCP──> 127.0.0.1:25565 ──MC Tunnel──> SOCKS5 / HTTP proxy ──> server
```

It is not a plain byte pump: the tunnel **rewrites the address inside the
Minecraft handshake** to the real server, so the client never has to know about
the proxy. If the first packet is not a handshake (a legacy `0xFE` ping, for
example), the stream passes through byte for byte.

## Features

- **SOCKS5** (RFC 1928, with authentication) and **HTTP CONNECT** (Basic auth).
- **One-button proxy check**: first the exit IP through the proxy, then a real
  server list ping to your server. You see that the server actually answers, not
  merely that the proxy accepts connections.
- **Live log**: connections, traffic counters, proxy errors. Long lines wrap
  instead of being cut off.
- **Settings are remembered** between runs; the proxy password is encrypted with
  Windows DPAPI and never stored as plain text.
- **A single window, no console**: GUI subsystem, all diagnostics go to the log.
- **Its own interface**: dark theme, Inter font bundled into the exe, a glow that
  follows the cursor, rounded corners, an application icon.

## Usage

1. Download `MC-Tunnel.exe` from [Releases](../../releases) and run it.
2. **Minecraft server** — `host` or `host:port` (port 25565 by default).
3. **Proxy** — one line, the protocol comes from the scheme:
   - `socks5://user:pass@host:1080`
   - `http://user:pass@host:8080`
   - `user:pass@host:1080` — no scheme, the protocol comes from the switch
   - `host:1080` — no authentication
   - `host:1080:user:pass` — the older colon-separated form
4. **Check proxy** (Проверить прокси) — make sure the proxy is alive and
   the server answers through it. The server field may be left empty for this
   check.
5. **Start** (Запустить) and join `127.0.0.1:25565` in Minecraft.

The local port is configurable. Both loopback addresses are bound, so `localhost`
in the game works over IPv4 and IPv6 — whichever the client picks.

## Where settings live

- `%APPDATA%\mctunnel\config.json` by default.
- If the profile is not writable: the folder next to the exe, then
  `%TEMP%\mctunnel`. The chosen path is always written to the log.
- The `MCTUNNEL_CONFIG` environment variable sets the path outright and is never
  silently overridden.
- The proxy password is encrypted with DPAPI and bound to the Windows account, so
  the config cannot be carried to another machine as is.
- Writes are atomic (temp file → `fsync` → rename), so an interrupted run never
  leaves half a config. An unreadable file is kept as `config.json.bak` and the
  app starts with an empty form.

## Under the hood

**Layers.** Each one knows only its neighbour:

```
main.go / app.go     Wails window and the bindings the page calls
frontend/dist        HTML/CSS/JS of the interface, no bundler
internal/tunnel      listener, sessions, counters, hard Stop
internal/relay       transport: tcp.go (Java), udp.go (a Bedrock placeholder)
internal/proxy       protocols and checks: socks5.go, http.go, check.go
internal/mcnet       handshake rewriting, server list ping
internal/serverprobe server check through the proxy
internal/settings    config: versioned schema, atomic writes
internal/secret      Windows DPAPI
internal/applog      in-memory log plus a subscription for the window
```

## Building

You need Go 1.25 and the Wails CLI:

```cmd
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails build -s -m
```

- `-s` — the frontend is already built, npm is not involved;
- `-m` — skip `go mod tidy`, the dependencies are pinned.

The executable lands in `build\bin`. The build needs **no cgo**: neither a C
compiler nor extra DLLs — the tunnel, the proxy and the settings are pure Go.

Checks before a commit:

```cmd
go vet ./...
go test ./...
```

The old Win32 interface (`cmd/mctunnel`, `internal/ui`) is still in the tree and
builds through `build.cmd`, in case the behaviour needs comparing.

Releases are built by GitHub Actions: pushing a `vX.Y.Z` tag runs the tests,
builds **x64 and ARM64** executables on a clean Windows runner, stamps that
version into the resource and attaches both files plus their SHA-256 to the
GitHub release. Nothing has to be built by hand for a release.

## Limitations

- Windows (x64 and ARM64) and Minecraft **Java** only. Bedrock/UDP is
  deliberately out of scope; Linux and macOS are not supported.
- No SRV: if the server lives behind a `_minecraft._tcp` record, the port has to
  be found by hand. The Minecraft client does that for you, the tunnel does not.
- The window has a fixed size (960×440) and does not maximise: the layout is
  built for exactly that.
- On Windows 10 the corners are rounded by clipping the window to a region, so
  the edge is stepped; on Windows 11 the compositor does it smoothly.
- WebView2 is required: present on Windows 11, usually installed with Edge on
  Windows 10.

## License

[MIT](LICENSE) © 2026 zzisler
