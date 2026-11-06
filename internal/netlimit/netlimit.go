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
			return &conn{Conn: c, l: l, client: client, counted: true}, nil
		}
		_ = c.Close()
		<-l.slots
		l.warn("connection limit of a client reached, connection closed", l.opts.PerClient)
	}
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
}

func (c *conn) Close() error {
	err := c.Conn.Close()
	c.release.Do(func() {
		if c.counted {
			c.l.leave(c.client)
		}
		<-c.l.slots
	})
	return err
}
