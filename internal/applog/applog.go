// Package applog keeps the recent log lines in memory and notifies a single
// subscriber (the GUI) about new ones. It is the only place that knows how a
// log line is formatted, so a future file sink or a different front end can
// reuse it unchanged.
package applog

import (
	"fmt"
	"sync"
	"time"
)

// Level is the severity of a log entry.
type Level int

// Severities, ordered from noisiest to loudest.
const (
	Debug Level = iota
	Info
	Warn
	Error
)

// String returns the short tag used in log output.
func (l Level) String() string {
	switch l {
	case Debug:
		return "DBG"
	case Info:
		return "INF"
	case Warn:
		return "WRN"
	case Error:
		return "ERR"
	}
	return "???"
}

// Entry is one line of the in-memory log.
type Entry struct {
	Time  time.Time
	Level Level
	Msg   string
}

// Format renders the entry the way the GUI shows it.
func (e Entry) Format() string {
	return e.Time.Format("15:04:05.000") + " " + e.Level.String() + " " + e.Msg
}

// Logger keeps the last N entries and is safe for concurrent use.
type Logger struct {
	mu      sync.Mutex
	entries []Entry
	max     int
	hook    func()
}

// New returns a logger that keeps at most max entries.
func New(max int) *Logger {
	if max < 1 {
		max = 1
	}
	return &Logger{max: max}
}

// SetHook installs a callback fired after every new entry. The hook runs
// without the logger lock held, so it may take locks or post window messages.
func (l *Logger) SetHook(fn func()) {
	l.mu.Lock()
	l.hook = fn
	l.mu.Unlock()
}

func (l *Logger) logf(lv Level, format string, args ...any) {
	e := Entry{Time: time.Now(), Level: lv, Msg: fmt.Sprintf(format, args...)}
	l.mu.Lock()
	l.entries = append(l.entries, e)
	if len(l.entries) > l.max {
		// Drop the oldest half in one go so the cost is amortised and the GUI
		// notices a shorter log and redraws from scratch.
		keep := l.max / 2
		if keep < 1 {
			keep = 1
		}
		l.entries = append([]Entry(nil), l.entries[len(l.entries)-keep:]...)
	}
	h := l.hook
	l.mu.Unlock()
	if h != nil {
		h()
	}
}

// Debugf logs a diagnostic message.
func (l *Logger) Debugf(format string, args ...any) { l.logf(Debug, format, args...) }

// Infof logs a normal message.
func (l *Logger) Infof(format string, args ...any) { l.logf(Info, format, args...) }

// Warnf logs a recoverable problem.
func (l *Logger) Warnf(format string, args ...any) { l.logf(Warn, format, args...) }

// Errorf logs a failure.
func (l *Logger) Errorf(format string, args ...any) { l.logf(Error, format, args...) }

// Len returns the number of stored entries.
func (l *Logger) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// Entries returns a copy of the stored entries.
func (l *Logger) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Bytes formats a byte count for humans.
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}
