// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// limitWarnInterval spaces the warnings about a full listener.
const limitWarnInterval = time.Minute

// limitListener keeps at most cap(slots) connections open, WebSockets
// included: anyone can open them without a session, and each takes memory
// (T-025). Further connections wait in the backlog of the kernel until one
// ends.
type limitListener struct {
	net.Listener
	slots    chan struct{}
	done     chan struct{}
	close    sync.Once
	log      *slog.Logger
	now      func() time.Time
	lastWarn atomic.Int64
}

func newLimitListener(ln net.Listener, limit int, log *slog.Logger) *limitListener {
	return &limitListener{Listener: ln, slots: make(chan struct{}, limit), done: make(chan struct{}), log: log, now: time.Now}
}

// acquire takes a slot and reports false once the listener is closed.
func (l *limitListener) acquire() bool {
	select {
	case l.slots <- struct{}{}:
		return true
	default:
	}
	l.warn()
	select {
	case l.slots <- struct{}{}:
		return true
	case <-l.done:
		return false
	}
}

func (l *limitListener) warn() {
	now := l.now().UnixNano()
	last := l.lastWarn.Load()
	if now-last >= int64(limitWarnInterval) && l.lastWarn.CompareAndSwap(last, now) {
		l.log.Warn("connection limit reached, new connections wait", slog.Int("limit", cap(l.slots)))
	}
}

// Accept waits for a free slot, then for a connection.
func (l *limitListener) Accept() (net.Conn, error) {
	if !l.acquire() {
		return nil, net.ErrClosed
	}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &limitConn{Conn: c, slots: l.slots}, nil
}

// Close ends the listener and a waiting Accept.
func (l *limitListener) Close() error {
	l.close.Do(func() { close(l.done) })
	return l.Listener.Close()
}

// limitConn gives its slot back when closed, once.
type limitConn struct {
	net.Conn
	slots   chan struct{}
	release sync.Once
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.release.Do(func() { <-c.slots })
	return err
}
