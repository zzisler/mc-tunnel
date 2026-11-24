// Package tunnel owns the local listener and the sessions that run through it.
//
// Everything that fixes the old lifecycle bugs lives here:
//
//   - Start holds the manager lock for the whole start, so a double click can
//     no longer open two listeners and report a bogus "address in use" error;
//   - Stop closes the listeners *and* every tracked session, so "stopped"
//     really means no traffic is flowing;
//   - every session goroutine is tracked by a WaitGroup, so Stop can wait for
//     a clean shutdown and log it.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"mctunnel/internal/addr"
	"mctunnel/internal/applog"
	"mctunnel/internal/proxy"
	"mctunnel/internal/relay"
)

// ErrRunning is returned by Start when a tunnel is already running.
var ErrRunning = errors.New("tunnel is already running")

// defaultMaxSessions caps how many clients the listener serves at once. A local
// game only ever needs one, so the limit is really there to keep a misbehaving
// or hostile local process from opening connections without bound.
const defaultMaxSessions = 64

// Config describes one run of the tunnel.
type Config struct {
	// Server is the Minecraft server address, host or host:port. The default
	// port 25565 is filled in when it is missing.
	Server string
	// Listen is the local address to accept game connections on.
	Listen string
	// Proxy is the dialer used to reach Server.
	Proxy proxy.Dialer
	// Transport defaults to relay.TCP when nil.
	Transport relay.Transport
	// MaxSessions caps concurrent sessions; 0 means defaultMaxSessions.
	MaxSessions int
}

// Manager starts, stops and reports on the tunnel.
type Manager struct {
	log   *applog.Logger
	stats Stats

	mu  sync.Mutex
	run *run
}

// run is one started tunnel. It owns everything that Stop has to tear down.
type run struct {
	cfg       Config
	target    string
	transport relay.Transport
	addrs     []string

	ctx    context.Context
	cancel context.CancelFunc
	lns    []net.Listener
	stats  *Stats
	log    *applog.Logger

	cmu   sync.Mutex
	conns map[net.Conn]struct{}
	wg    sync.WaitGroup

	// sessions is a semaphore: a slot is taken before a session goroutine runs
	// and released when it ends.
	sessions chan struct{}
}

// New returns a Manager that logs to log.
func New(log *applog.Logger) *Manager {
	if log == nil {
		log = applog.New(100)
	}
	return &Manager{log: log}
}

// Start validates the configuration, opens the listeners and begins accepting.
// It returns the addresses the game should connect to.
func (m *Manager) Start(cfg Config) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.run != nil {
		return nil, ErrRunning
	}
	if cfg.Proxy == nil {
		return nil, errors.New("no proxy configured")
	}
	if cfg.Transport == nil {
		cfg.Transport = relay.TCP()
	}
	target, err := addr.Normalize(cfg.Server, 25565)
	if err != nil {
		return nil, fmt.Errorf("minecraft server: %w", err)
	}
	listen := strings.TrimSpace(cfg.Listen)
	if listen == "" {
		listen = "127.0.0.1:25565"
	}
	lns, addrs, err := listenAll(listen)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.stats.Reset()
	maxSessions := cfg.MaxSessions
	if maxSessions <= 0 {
		maxSessions = defaultMaxSessions
	}
	r := &run{
		cfg:       cfg,
		target:    target,
		transport: cfg.Transport,
		addrs:     addrs,
		ctx:       ctx,
		cancel:    cancel,
		lns:       lns,
		stats:     &m.stats,
		log:       m.log,
		conns:     make(map[net.Conn]struct{}),
		sessions:  make(chan struct{}, maxSessions),
	}
	m.run = r
	for _, ln := range lns {
		r.wg.Add(1)
		go r.acceptLoop(ln)
	}
	m.log.Infof("tunnel started: %s -> %s via %s (%s)",
		strings.Join(addrs, ", "), target, cfg.Proxy.Describe(), cfg.Transport.Name())
	return addrs, nil
}

// Stop closes the listeners and every live session. It returns as soon as the
// sockets are closed; the sessions finish in the background because they can
// be blocked inside third-party dialers for a moment.
func (m *Manager) Stop() {
	m.mu.Lock()
	r := m.run
	m.run = nil
	m.mu.Unlock()
	if r == nil {
		return
	}
	m.log.Infof("stopping tunnel…")
	r.shutdown()
	go func() {
		r.wg.Wait()
		m.log.Infof("tunnel stopped")
	}()
}

// Running reports whether a tunnel is currently accepting connections.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.run != nil
}

// Stats returns a snapshot of the counters of the current or last run.
func (m *Manager) Stats() Snapshot { return m.stats.Snapshot() }

// LocalAddrs returns the addresses the last Start opened, or nil.
func (m *Manager) LocalAddrs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.run == nil {
		return nil
	}
	return append([]string(nil), m.run.addrs...)
}

func (r *run) shutdown() {
	r.cancel()
	for _, ln := range r.lns {
		_ = ln.Close()
	}
	r.cmu.Lock()
	for conn := range r.conns {
		_ = conn.Close()
	}
	r.cmu.Unlock()
}

func (r *run) acceptLoop(ln net.Listener) {
	defer r.wg.Done()
	for {
		client, err := ln.Accept()
		if err != nil {
			// Listener closed by shutdown, or a permanent failure: either way
			// this loop is done.
			return
		}
		if r.ctx.Err() != nil {
			_ = client.Close()
			continue
		}
		select {
		case r.sessions <- struct{}{}:
		default:
			r.log.Warnf("%s: session limit reached (%d), connection refused",
				client.RemoteAddr(), cap(r.sessions))
			_ = client.Close()
			continue
		}
		r.track(client)
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			defer func() { <-r.sessions }()
			r.session(client)
		}()
	}
}

func (r *run) track(conn net.Conn) {
	r.cmu.Lock()
	r.conns[conn] = struct{}{}
	r.cmu.Unlock()
}

func (r *run) untrack(conn net.Conn) {
	r.cmu.Lock()
	delete(r.conns, conn)
	r.cmu.Unlock()
}

func (r *run) session(conn net.Conn) {
	peer := conn.RemoteAddr().String()
	counted := newCountingConn(conn, r.stats)

	defer func() {
		r.untrack(conn)
		_ = counted.Close()
		r.stats.active.Add(-1)
		r.log.Infof("%s closed: up %s, down %s",
			peer, applog.Bytes(counted.Up()), applog.Bytes(counted.Down()))
	}()

	r.stats.total.Add(1)
	r.stats.active.Add(1)
	r.log.Infof("%s connected -> %s", peer, r.target)

	err := r.transport.Forward(r.ctx, counted, r.target, r.cfg.Proxy, r.log)
	if err != nil && !errors.Is(err, context.Canceled) && r.ctx.Err() == nil {
		r.log.Errorf("%s: %v", peer, err)
	}
}

// listenAll opens the requested listener and, when it is bound to IPv4
// loopback with a fixed port, a second one on IPv6 loopback so that typing
// "localhost" in the game works no matter which family the client picks.
func listenAll(spec string) ([]net.Listener, []string, error) {
	ln, err := net.Listen("tcp", spec)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot listen on %s: %w", spec, err)
	}
	lns := []net.Listener{ln}
	addrs := []string{ln.Addr().String()}

	// The resolved port of an ephemeral listener is never shared with the IPv6
	// one, so the second socket is only useful for a fixed port.
	host, requestedPort, splitErr := net.SplitHostPort(spec)
	if splitErr != nil || requestedPort == "" || requestedPort == "0" || host != "127.0.0.1" {
		return lns, addrs, nil
	}
	if ln6, err := net.Listen("tcp", net.JoinHostPort("::1", requestedPort)); err == nil {
		lns = append(lns, ln6)
		addrs = append(addrs, ln6.Addr().String())
	}
	return lns, addrs, nil
}
