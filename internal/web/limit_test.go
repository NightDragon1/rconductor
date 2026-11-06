// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/netlimit"
)

// noAnswer is how long a connection stays silent to count as waiting.
const noAnswer = 300 * time.Millisecond

// servedAt runs Serve with the limit and returns its address and its end.
func servedAt(t *testing.T, limit int, h http.Handler, logs io.Writer) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	return servedWith(t, netlimit.Options{Max: limit, PerClient: limit}, h, logs)
}

func servedWith(t *testing.T, o netlimit.Options, h http.Handler, logs io.Writer) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ready := make(chan net.Addr, 1)
	ended := make(chan error, 1)
	go func() {
		ended <- Serve(ctx, "127.0.0.1:0", o, h, slog.New(slog.NewTextHandler(logs, nil)), func(a net.Addr) { ready <- a })
	}()
	select {
	case a := <-ready:
		return a.String(), cancel, ended
	case err := <-ended:
		t.Fatal(err)
	}
	return "", cancel, ended
}

func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	var d net.Dialer
	c, err := d.DialContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// get asks over c and returns the status line, or an error after wait.
func get(c net.Conn, wait time.Duration) (string, error) {
	if _, err := fmt.Fprint(c, "GET / HTTP/1.1\r\nHost: test\r\n\r\n"); err != nil {
		return "", err
	}
	if err := c.SetReadDeadline(time.Now().Add(wait)); err != nil {
		return "", err
	}
	return bufio.NewReader(c).ReadString('\n')
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
}

// TestConnectionLimit covers T-025: connections without any request hold their
// slot, the next one waits and gets its answer when a slot is free.
func TestConnectionLimit(t *testing.T) {
	logs := &syncBuffer{}
	addr, _, _ := servedAt(t, 2, okHandler(), logs)
	first, second := dial(t, addr), dial(t, addr)
	if line, err := get(first, 5*time.Second); err != nil || !strings.Contains(line, "204") {
		t.Fatalf("first = %q, %v", line, err)
	}
	if line, err := get(second, 5*time.Second); err != nil || !strings.Contains(line, "204") {
		t.Fatalf("second = %q, %v", line, err)
	}
	third := dial(t, addr)
	if line, err := get(third, noAnswer); err == nil {
		t.Fatalf("over the limit answered: %q", line)
	}
	if !strings.Contains(logs.String(), "connection limit reached") {
		t.Error("no warning about the limit")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := third.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if line, err := bufio.NewReader(third).ReadString('\n'); err != nil || !strings.Contains(line, "204") {
		t.Errorf("after a free slot = %q, %v", line, err)
	}
}

// TestConnectionFloodStaysUnderTheLimit covers T-025 with many clients at once:
// never more requests run than the limit allows, and all get their answer.
func TestConnectionFloodStaysUnderTheLimit(t *testing.T) {
	const limit, clients = 4, 60
	var running, highest atomic.Int32
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := running.Add(1)
		defer running.Add(-1)
		for {
			old := highest.Load()
			if n <= old || highest.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusNoContent)
	})
	addr, _, _ := servedAt(t, limit, h, io.Discard)
	var wg sync.WaitGroup
	var answered atomic.Int32
	for range clients {
		wg.Go(func() {
			var d net.Dialer
			c, err := d.DialContext(context.Background(), "tcp", addr)
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = c.Close() }()
			if line, err := get(c, 30*time.Second); err == nil && strings.Contains(line, "204") {
				answered.Add(1)
			}
		})
	}
	wg.Wait()
	if n := answered.Load(); n != clients {
		t.Errorf("%d of %d answered", n, clients)
	}
	if n := highest.Load(); n > limit {
		t.Errorf("%d requests at once, the limit is %d", n, limit)
	}
}

// TestWebSocketHoldsItsSlot covers T-025: a hijacked connection counts until
// it ends, then its slot is free again.
func TestWebSocketHoldsItsSlot(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.CloseNow() }()
		_, _, _ = ws.Read(r.Context())
	})
	addr, _, _ := servedAt(t, 1, h, io.Discard)
	ws, resp, err := websocket.Dial(context.Background(), "ws://"+addr+"/ws", nil)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		t.Fatal(err)
	}
	waiting := dial(t, addr)
	if line, err := get(waiting, noAnswer); err == nil {
		t.Fatalf("answered beside the WebSocket: %q", line)
	}
	if err := ws.CloseNow(); err != nil {
		t.Fatal(err)
	}
	if err := waiting.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if line, err := bufio.NewReader(waiting).ReadString('\n'); err != nil || !strings.Contains(line, "204") {
		t.Errorf("after the WebSocket = %q, %v", line, err)
	}
}

// TestShutdownWithAFullListener covers T-025: the wait for a slot does not
// hold up the end of the panel.
func TestShutdownWithAFullListener(t *testing.T) {
	addr, cancel, ended := servedAt(t, 1, okHandler(), io.Discard)
	c := dial(t, addr)
	if line, err := get(c, 5*time.Second); err != nil || !strings.Contains(line, "204") {
		t.Fatalf("first = %q, %v", line, err)
	}
	dial(t, addr)
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-ended:
		if err != nil {
			t.Errorf("Serve = %v", err)
		}
	case <-time.After(shutdownTimeout + 5*time.Second):
		t.Fatal("no shutdown")
	}
}

// syncBuffer collects log lines of the server goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestServerDropsAClientThatDoesNotRead covers T-025: a client that asks for
// much, reads nothing and stays connected loses its place at the write deadline.
func TestServerDropsAClientThatDoesNotRead(t *testing.T) {
	defer func(d time.Duration) { writeTimeout = d }(writeTimeout)
	writeTimeout = 200 * time.Millisecond
	big := bytes.Repeat([]byte("x"), 1<<20)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/big" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		for {
			if _, err := w.Write(big); err != nil {
				return
			}
		}
	})
	addr, _, _ := servedAt(t, 1, h, io.Discard)
	c := dial(t, addr)
	if _, err := fmt.Fprint(c, "GET /big HTTP/1.1\r\nHost: test\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	if line, err := get(dial(t, addr), 5*time.Second); err != nil || !strings.Contains(line, "204") {
		t.Errorf("the place stayed taken: %q, %v", line, err)
	}
}

// closed reports whether the peer ends c within wait.
func closed(c net.Conn, wait time.Duration) bool {
	if err := c.SetReadDeadline(time.Now().Add(wait)); err != nil {
		return true
	}
	_, err := c.Read(make([]byte, 1))
	var timeout net.Error
	return !errors.As(err, &timeout) || !timeout.Timeout()
}

// TestV2ConnectionWithoutSessionAges covers V-2: a request now and then does not
// hold a place for good, and the place is free afterwards.
func TestV2ConnectionWithoutSessionAges(t *testing.T) {
	o := netlimit.Options{Max: 1, PerClient: 1, MaxAge: 300 * time.Millisecond}
	addr, _, _ := servedWith(t, o, okHandler(), io.Discard)
	c := dial(t, addr)
	if line, err := get(c, time.Second); err != nil || !strings.Contains(line, "204") {
		t.Fatalf("first request: %q, %v", line, err)
	}
	if !closed(c, 2*time.Second) {
		t.Fatal("connection outlived its age")
	}
	if line, err := get(dial(t, addr), 2*time.Second); err != nil || !strings.Contains(line, "204") {
		t.Errorf("the place stayed taken: %q, %v", line, err)
	}
}

// TestAgeWaitsForTheRunningRequest: a request that runs over the age gets
// its answer, then the connection ends.
func TestAgeWaitsForTheRunningRequest(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	})
	o := netlimit.Options{Max: 2, PerClient: 2, MaxAge: 100 * time.Millisecond}
	addr, _, _ := servedWith(t, o, h, io.Discard)
	c := dial(t, addr)
	if line, err := get(c, 2*time.Second); err != nil || !strings.Contains(line, "204") {
		t.Fatalf("the running request lost its answer: %q, %v", line, err)
	}
	if !closed(c, 2*time.Second) {
		t.Error("connection went on after the request")
	}
}

// TestSessionKeepsItsConnection: who showed a session stays, as a browser with
// an open panel does, and so does a WebSocket.
func TestSessionKeepsItsConnection(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keepConn(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	o := netlimit.Options{Max: 2, PerClient: 2, MaxAge: 200 * time.Millisecond}
	addr, _, _ := servedWith(t, o, h, io.Discard)
	c := dial(t, addr)
	if line, err := get(c, time.Second); err != nil || !strings.Contains(line, "204") {
		t.Fatalf("request: %q, %v", line, err)
	}
	if closed(c, 600*time.Millisecond) {
		t.Fatal("connection with a session was ended")
	}
	if line, err := get(c, time.Second); err != nil || !strings.Contains(line, "204") {
		t.Errorf("second request: %q, %v", line, err)
	}
}

// TestProxyConnectionsDoNotAge: a trusted proxy carries many visitors over
// its connections; it has no session of its own to show.
func TestProxyConnectionsDoNotAge(t *testing.T) {
	o := netlimit.Options{Max: 2, PerClient: 1, MaxAge: 200 * time.Millisecond, Exempt: func(netip.Addr) bool { return true }}
	addr, _, _ := servedWith(t, o, okHandler(), io.Discard)
	c := dial(t, addr)
	if line, err := get(c, time.Second); err != nil || !strings.Contains(line, "204") {
		t.Fatalf("request: %q, %v", line, err)
	}
	if closed(c, 600*time.Millisecond) {
		t.Error("connection of the proxy was ended")
	}
}

// TestSessionRequestReachesKeep covers the path of a real request: the API
// keeps the connection once the session is valid, and only then.
func TestSessionRequestReachesKeep(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	session := f.login(t, "Mod")
	o := netlimit.Options{Max: 4, PerClient: 4, MaxAge: 300 * time.Millisecond}
	addr, _, _ := servedWith(t, o, f.h, io.Discard)
	ask := func(c net.Conn, cookie string) {
		t.Helper()
		if _, err := fmt.Fprintf(c, "GET /api/v1/auth/session HTTP/1.1\r\nHost: panel.lan\r\nCookie: %s\r\n\r\n", cookie); err != nil {
			t.Fatal(err)
		}
		if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := bufio.NewReader(c).ReadString('\n'); err != nil {
			t.Fatal(err)
		}
	}
	with, without := dial(t, addr), dial(t, addr)
	ask(with, session.Name+"="+session.Value)
	ask(without, session.Name+"=wrong")
	if !closed(without, 2*time.Second) {
		t.Error("a made-up session kept its connection")
	}
	if closed(with, 100*time.Millisecond) {
		t.Error("a valid session lost its connection")
	}
}

// TestBrokenRequestsAreRefused comes from an exploratory check without a
// finding, so it has no state before a fix to fail on: requests the HTTP server
// must refuse by itself stay refused behind the connection limit.
func TestBrokenRequestsAreRefused(t *testing.T) {
	addr, _, _ := servedAt(t, 8, okHandler(), io.Discard)
	for name, c := range map[string]struct{ request, want string }{
		"no host":          {"GET / HTTP/1.1\r\nConnection: close\r\n\r\n", "400"},
		"bad request line": {"NONSENSE\r\n\r\n", "400"},
		"header too big":   {"GET / HTTP/1.1\r\nHost: x\r\nX-A: " + strings.Repeat("a", 2<<20) + "\r\n\r\n", "431"},
	} {
		conn := dial(t, addr)
		if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		go func() { _, _ = fmt.Fprint(conn, c.request) }()
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil || !strings.Contains(line, c.want) {
			t.Errorf("%s: %q, %v", name, line, err)
		}
	}
}
