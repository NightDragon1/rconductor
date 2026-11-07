// SPDX-License-Identifier: AGPL-3.0-or-later

package netlimit

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// TestR5B31SlotsUnderStress: real http.Server, real timers, mixed behavior, with
// ages much shorter than the requests. Every slot must come back, once.
func TestR5B31SlotsUnderStress(t *testing.T) {
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := New(tcp, Options{Max: 64, PerClient: 32, MaxAge: 30 * time.Millisecond, Log: slog.New(slog.DiscardHandler)})
	type key struct{}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/keep" {
				Keep(r.Context().Value(key{}).(net.Conn))
			}
			if r.URL.Path == "/hijack" {
				c, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					time.Sleep(time.Duration(rand.IntN(20)) * time.Millisecond)
					_ = c.Close()
				}
				return
			}
			time.Sleep(time.Duration(rand.IntN(50)) * time.Millisecond)
			_, _ = io.WriteString(w, "ok")
		}),
		ConnContext: func(ctx context.Context, c net.Conn) context.Context { return context.WithValue(ctx, key{}, c) },
		ConnState: func(c net.Conn, s http.ConnState) {
			switch s {
			case http.StateActive:
				Busy(c, true)
			case http.StateIdle:
				Busy(c, false)
			case http.StateHijacked:
				Keep(c)
			}
		},
		IdleTimeout: 100 * time.Millisecond,
	}
	go func() { _ = srv.Serve(ln) }()
	addr := tcp.Addr().String()
	var wg sync.WaitGroup
	for range 48 {
		wg.Go(func() {
			for range 60 {
				c, err := net.DialTimeout("tcp", addr, time.Second)
				if err != nil {
					continue
				}
				switch rand.IntN(5) {
				case 0: // silent
					time.Sleep(time.Duration(rand.IntN(60)) * time.Millisecond)
				case 1:
					_, _ = fmt.Fprint(c, "GET /a HTTP/1.1\r\nHost: x\r\n\r\nGET /b HTTP/1.1\r\nHost: x\r\n\r\n")
					_ = c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
					_, _ = io.Copy(io.Discard, c)
				case 2:
					_, _ = fmt.Fprint(c, "GET /keep HTTP/1.1\r\nHost: x\r\n\r\n")
					time.Sleep(time.Duration(rand.IntN(60)) * time.Millisecond)
				case 3:
					_, _ = fmt.Fprint(c, "GET /hijack HTTP/1.1\r\nHost: x\r\n\r\n")
				case 4:
					_, _ = fmt.Fprint(c, "GET /part")
				}
				_ = c.Close()
			}
		})
	}
	wg.Wait()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ln.mu.Lock()
		n := len(ln.clients)
		ln.mu.Unlock()
		if len(ln.slots) <= 1 && n == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	ln.mu.Lock()
	t.Logf("slots in use %d (1 = the waiting Accept), clients %v", len(ln.slots), ln.clients)
	ln.mu.Unlock()
	_ = srv.Close()
	if len(ln.slots) > 1 {
		t.Error("slots lost")
	}
}
