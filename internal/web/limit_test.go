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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// noAnswer is how long a connection stays silent to count as waiting.
const noAnswer = 300 * time.Millisecond

// servedAt runs Serve with the limit and returns its address and its end.
func servedAt(t *testing.T, limit int, h http.Handler, logs io.Writer) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ready := make(chan net.Addr, 1)
	ended := make(chan error, 1)
	go func() {
		ended <- Serve(ctx, "127.0.0.1:0", limit, h, slog.New(slog.NewTextHandler(logs, nil)), func(a net.Addr) { ready <- a })
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

// TestSlotIsGivenBackOnce covers T-025: a second Close frees no second slot.
func TestSlotIsGivenBackOnce(t *testing.T) {
	var lc net.ListenConfig
	tcp, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := newLimitListener(tcp, 2, slog.New(slog.DiscardHandler))
	defer func() { _ = l.Close() }()
	take := func() net.Conn {
		t.Helper()
		dial(t, tcp.Addr().String())
		c, err := l.Accept()
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	first, second := take(), take()
	_ = first.Close()
	_ = first.Close()
	if n := len(l.slots); n != 1 {
		t.Errorf("slots in use = %d, want 1", n)
	}
	_ = second.Close()
}

// TestAcceptErrorGivesTheSlotBack covers T-025: a failed accept leaks nothing.
func TestAcceptErrorGivesTheSlotBack(t *testing.T) {
	var lc net.ListenConfig
	tcp, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := newLimitListener(tcp, 1, slog.New(slog.DiscardHandler))
	_ = tcp.Close()
	if _, err := l.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Accept = %v", err)
	}
	if n := len(l.slots); n != 0 {
		t.Errorf("slots in use = %d", n)
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
