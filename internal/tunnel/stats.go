package tunnel

import (
	"net"
	"sync/atomic"
)

// Snapshot is a consistent-enough copy of the counters for display.
type Snapshot struct {
	// Active is the number of sessions open right now.
	Active int64
	// Total is the number of sessions since the tunnel was started.
	Total int64
	// Up is how many bytes the clients sent.
	Up int64
	// Down is how many bytes the server sent back.
	Down int64
}

// Stats collects counters for one Manager. Every field is atomic because the
// pumps of several sessions update them concurrently.
type Stats struct {
	up     atomic.Int64
	down   atomic.Int64
	total  atomic.Int64
	active atomic.Int64
}

// Reset clears every counter, used when a new session of the tunnel starts.
func (s *Stats) Reset() {
	s.up.Store(0)
	s.down.Store(0)
	s.total.Store(0)
	s.active.Store(0)
}

// Snapshot reads the counters.
func (s *Stats) Snapshot() Snapshot {
	return Snapshot{
		Active: s.active.Load(),
		Total:  s.total.Load(),
		Up:     s.up.Load(),
		Down:   s.down.Load(),
	}
}

// countingConn wraps the accepted client connection so both directions of one
// session are measured in place: every Read is traffic towards the server and
// every Write is traffic towards the game.
type countingConn struct {
	net.Conn
	stats *Stats
	up    atomic.Int64
	down  atomic.Int64
}

func newCountingConn(conn net.Conn, stats *Stats) *countingConn {
	return &countingConn{Conn: conn, stats: stats}
}

// Read implements io.Reader and counts bytes going up (client to server).
func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.up.Add(int64(n))
		c.stats.up.Add(int64(n))
	}
	return n, err
}

// Write implements io.Writer and counts bytes going down (server to client).
func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.down.Add(int64(n))
		c.stats.down.Add(int64(n))
	}
	return n, err
}

// Up returns the bytes this session sent towards the server.
func (c *countingConn) Up() int64 { return c.up.Load() }

// Down returns the bytes this session received from the server.
func (c *countingConn) Down() int64 { return c.down.Load() }

// CloseWrite forwards half-close to the wrapped TCP connection.
func (c *countingConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}
