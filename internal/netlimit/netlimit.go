// SPDX-License-Identifier: AGPL-3.0-or-later

// Package netlimit bounds the connections of a listener in total and per
// client address (T-017, T-025).
package netlimit

import (
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

// warnInterval spaces the warnings about a limit.
const warnInterval = time.Minute

// ClientKey is the address a limit counts: an IPv6 client usually owns a
// whole /64, so rotating inside it must not start a new count.
func ClientKey(ip netip.Addr) netip.Addr {
	ip = ip.Unmap()
	if ip.Is6() {
		if p, err := ip.Prefix(64); err == nil {
			return p.Addr()
		}
	}
	return ip
}

// Options configure a Listener.
type Options struct {
	// Max is the number of open connections in total.
	Max int
	// PerClient is the number of open connections of one client address.
	PerClient int
	// MaxAge, if set, ends a connection this long after it was opened unless
	// Keep was called for it: anyone can open one, and a request now and
	// then would hold its place for good. It waits for a running request.
	MaxAge time.Duration
	// Exempt, if set, names addresses without a limit of their own: behind
	// a proxy all visitors share its address.
	Exempt func(netip.Addr) bool
	Log    *slog.Logger
}

// Listener keeps at most Max connections open, WebSockets included: anyone
// can open them without a session, and each takes memory. Further connections
// wait in the backlog of the kernel until one ends. A client over its own
// limit is hung up on at once, so that one address cannot take all places.
type Listener struct {
	net.Listener
	opts     Options
	slots    chan struct{}
	done     chan struct{}
	close    sync.Once
	lastWarn atomic.Int64

	mu      sync.Mutex
	clients map[netip.Addr]int
}

// New wraps ln.
func New(ln net.Listener, opts Options) *Listener {
	return &Listener{Listener: ln, opts: opts, slots: make(chan struct{}, opts.Max), done: make(chan struct{}), clients: map[netip.Addr]int{}}
}

// acquire takes a slot and reports false once the listener is closed.
func (l *Listener) acquire() bool {
	select {
	case l.slots <- struct{}{}:
		return true
	default:
	}
	l.warn("connection limit reached, new connections wait", l.opts.Max)
	select {
	case l.slots <- struct{}{}:
		return true
	case <-l.done:
		return false
	}
}

func (l *Listener) warn(msg string, limit int) {
	now := time.Now().UnixNano()
	last := l.lastWarn.Load()
	if now-last >= int64(warnInterval) && l.lastWarn.CompareAndSwap(last, now) {
		l.opts.Log.Warn(msg, slog.Int("limit", limit))
	}
}

// admit counts a connection of client and reports false over its limit.
func (l *Listener) admit(client netip.Addr) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.clients[client] >= l.opts.PerClient {
		return false
	}
	l.clients[client]++
	return true
}

func (l *Listener) leave(client netip.Addr) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.clients[client]--; l.clients[client] <= 0 {
		delete(l.clients, client)
	}
}

func addrOf(a net.Addr) netip.Addr {
	if t, ok := a.(*net.TCPAddr); ok {
		return t.AddrPort().Addr()
	}
	p, err := netip.ParseAddrPort(a.String())
	if err != nil {
		return netip.Addr{}
	}
	return p.Addr()
}

// Accept waits for a free slot, then for a connection within its client limit.
func (l *Listener) Accept() (net.Conn, error) {
	for {
		if !l.acquire() {
			return nil, net.ErrClosed
		}
		c, err := l.Listener.Accept()
		if err != nil {
			<-l.slots
			return nil, err
		}
		addr := addrOf(c.RemoteAddr())
		if l.opts.Exempt != nil && l.opts.Exempt(addr) {
			return &conn{Conn: c, l: l}, nil
		}
		client := ClientKey(addr)
		if l.admit(client) {
			return l.limited(c, client), nil
		}
		_ = c.Close()
		<-l.slots
		l.warn("connection limit of a client reached, connection closed", l.opts.PerClient)
	}
}

func (l *Listener) limited(c net.Conn, client netip.Addr) *conn {
	lc := &conn{Conn: c, l: l, client: client, counted: true}
	if l.opts.MaxAge > 0 {
		lc.mu.Lock()
		defer lc.mu.Unlock()
		lc.age = time.AfterFunc(l.opts.MaxAge, lc.expire)
	}
	return lc
}

// Close ends the listener and a waiting Accept.
func (l *Listener) Close() error {
	l.close.Do(func() { close(l.done) })
	return l.Listener.Close()
}

// conn gives its places back when closed, once.
type conn struct {
	net.Conn
	l       *Listener
	client  netip.Addr
	counted bool
	release sync.Once

	mu      sync.Mutex
	age     *time.Timer
	kept    bool
	busy    bool
	expired bool
}

// expire ends the connection, or marks it to end after the running request.
func (c *conn) expire() {
	c.mu.Lock()
	c.expired = !c.kept
	end := c.expired && !c.busy
	c.mu.Unlock()
	if end {
		_ = c.Close()
	}
}

// Keep exempts a connection from MaxAge, because it proved who sends on it.
func Keep(nc net.Conn) {
	c, ok := nc.(*conn)
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.kept = true
	c.expired = false
	if c.age != nil {
		c.age.Stop()
	}
}

// Busy tells whether a request is running on the connection. An expired
// connection ends as soon as none is.
func Busy(nc net.Conn, busy bool) {
	c, ok := nc.(*conn)
	if !ok {
		return
	}
	c.mu.Lock()
	c.busy = busy
	end := c.expired && !busy
	c.mu.Unlock()
	if end {
		_ = c.Close()
	}
}

func (c *conn) Close() error {
	c.mu.Lock()
	if c.age != nil {
		c.age.Stop()
	}
	c.mu.Unlock()
	err := c.Conn.Close()
	c.release.Do(func() {
		if c.counted {
			c.l.leave(c.client)
		}
		<-c.l.slots
	})
	return err
}
