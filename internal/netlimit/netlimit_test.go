// SPDX-License-Identifier: AGPL-3.0-or-later

package netlimit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"testing"
	"testing/synctest"
	"time"
)

func listen(t *testing.T, o Options) *Listener {
	t.Helper()
	var lc net.ListenConfig
	tcp, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	o.Log = slog.New(slog.DiscardHandler)
	l := New(tcp, o)
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func dial(t *testing.T, l *Listener) net.Conn {
	t.Helper()
	var d net.Dialer
	c, err := d.DialContext(context.Background(), "tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func accept(t *testing.T, l *Listener) net.Conn {
	t.Helper()
	dial(t, l)
	c, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestSlotIsGivenBackOnce covers T-025: a second Close frees no second place.
func TestSlotIsGivenBackOnce(t *testing.T) {
	l := listen(t, Options{Max: 2, PerClient: 2})
	first, second := accept(t, l), accept(t, l)
	_ = first.Close()
	_ = first.Close()
	if n := len(l.slots); n != 1 {
		t.Errorf("slots in use = %d, want 1", n)
	}
	_ = second.Close()
	if n := len(l.clients); n != 0 {
		t.Errorf("%d clients left", n)
	}
}

// TestAcceptErrorGivesTheSlotBack covers T-025: a failed accept leaks nothing.
func TestAcceptErrorGivesTheSlotBack(t *testing.T) {
	l := listen(t, Options{Max: 1, PerClient: 1})
	_ = l.Listener.Close()
	if _, err := l.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Accept = %v", err)
	}
	if n := len(l.slots); n != 0 {
		t.Errorf("slots in use = %d", n)
	}
}

// TestClientOverItsLimitIsClosed covers T-025: one address takes no more than
// its share, gets hung up on at once, and its place stays free for others.
func TestClientOverItsLimitIsClosed(t *testing.T) {
	l := listen(t, Options{Max: 10, PerClient: 2})
	first, second := accept(t, l), accept(t, l)
	over := dial(t, l)
	accepted := make(chan net.Conn, 1)
	go func() {
		if c, err := l.Accept(); err == nil {
			accepted <- c
		}
	}()
	if err := over.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := over.Read(make([]byte, 1)); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("read over the limit = %v, want the end of the connection", err)
	}
	if n := len(l.slots); n > 3 {
		t.Errorf("slots in use = %d", n)
	}
	_ = first.Close()
	dial(t, l)
	select {
	case c := <-accepted:
		_ = c.Close()
	case <-time.After(5 * time.Second):
		t.Error("no place after one was freed")
	}
	_ = second.Close()
}

// TestExemptAddressHasNoClientLimit covers Design 5.2: behind a trusted proxy
// all visitors share one address.
func TestExemptAddressHasNoClientLimit(t *testing.T) {
	l := listen(t, Options{Max: 10, PerClient: 1, Exempt: func(netip.Addr) bool { return true }})
	for range 5 {
		defer func(c net.Conn) { _ = c.Close() }(accept(t, l))
	}
	if n := len(l.clients); n != 0 {
		t.Errorf("%d clients counted", n)
	}
}

// TestClientKey covers T-017 and T-002: an IPv6 /64 is one client.
func TestClientKey(t *testing.T) {
	for in, want := range map[string]string{
		"192.0.2.1":          "192.0.2.1",
		"::ffff:192.0.2.1":   "192.0.2.1",
		"2001:db8:1:2::1":    "2001:db8:1:2::",
		"2001:db8:1:2:ff::9": "2001:db8:1:2::",
	} {
		if got := ClientKey(netip.MustParseAddr(in)).String(); got != want {
			t.Errorf("ClientKey(%s) = %s, want %s", in, got, want)
		}
	}
}

// pipeConn is one end of a pipe with an address, for a test in virtual time.
type pipeConn struct{ net.Conn }

func (pipeConn) RemoteAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 1} }

// oneListener hands out one connection, then waits for its end.
type oneListener struct {
	net.Listener
	conn net.Conn
	done chan struct{}
}

func (l *oneListener) Accept() (net.Conn, error) {
	if c := l.conn; c != nil {
		l.conn = nil
		return c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

func (l *oneListener) Close() error { close(l.done); return nil }

// TestMaxAgeInVirtualTime covers RCD_MAX_ANONYMOUS_CONNECTION_SECONDS with its
// real value: the connection ends at the age, not before.
func TestMaxAgeInVirtualTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, client := net.Pipe()
		defer func() { _ = client.Close() }()
		l := New(&oneListener{conn: pipeConn{server}, done: make(chan struct{})},
			Options{Max: 4, PerClient: 4, MaxAge: 30 * time.Second, Log: slog.New(slog.DiscardHandler)})
		defer func() { _ = l.Close() }()
		if _, err := l.Accept(); err != nil {
			t.Fatal(err)
		}
		ended := make(chan error, 1)
		go func() {
			_, err := client.Read(make([]byte, 1))
			ended <- err
		}()
		time.Sleep(29 * time.Second)
		synctest.Wait()
		select {
		case err := <-ended:
			t.Fatalf("ended early: %v", err)
		default:
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case err := <-ended:
			if !errors.Is(err, io.EOF) {
				t.Errorf("end = %v", err)
			}
		default:
			t.Error("still open past its age")
		}
	})
}
