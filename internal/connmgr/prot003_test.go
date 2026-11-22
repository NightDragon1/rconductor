// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package connmgr

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon"
)

// parallelAdapter dials connections that take several commands at once:
// "break" ends with EOF as an old keep-alive connection does, "hold" waits
// for release, anything else is echoed.
type parallelAdapter struct {
	dials   atomic.Int64
	release chan struct{}
	mu      sync.Mutex
	conns   []*parallelConn
}

type parallelConn struct {
	closed atomic.Bool
	a      *parallelAdapter
}

func (a *parallelAdapter) Dial(context.Context, string, logging.Secret) (rcon.Conn, error) {
	a.dials.Add(1)
	c := &parallelConn{a: a}
	a.mu.Lock()
	a.conns = append(a.conns, c)
	a.mu.Unlock()
	return c, nil
}

func (a *parallelAdapter) conn(i int) *parallelConn {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conns[i]
}

func (*parallelConn) Concurrent() {}

func (c *parallelConn) Execute(ctx context.Context, command string) (string, error) {
	switch command {
	case "break":
		return "", io.EOF
	case "hold":
		select {
		case <-c.a.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if c.closed.Load() {
		return "", io.ErrClosedPipe
	}
	return command, nil
}

func (c *parallelConn) Close() error {
	c.closed.Store(true)
	return nil
}

func startParallel(t *testing.T) (*Manager, *parallelAdapter, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	src := &fakeSource{targets: map[string]Target{}}
	src.set(Target{ID: "s", Name: "s", Protocol: "x", Addr: "x:1", Usable: true})
	a := &parallelAdapter{release: make(chan struct{})}
	m := New(rcon.Registry{"x": a}, src, slog.New(slog.DiscardHandler), Options{IdleProbe: time.Hour})
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	synctest.Wait()
	return m, a, cancel
}

// TestBrokenConcurrentConnectionRedials covers PROT-003: a concurrent
// connection that breaks under a command ends at once, without waiting for
// the next command or the idle probe, and the worker dials again.
func TestBrokenConcurrentConnectionRedials(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, a, cancel := startParallel(t)
		defer func() { cancel(); m.Wait() }()
		if s, _ := m.Status("s"); s != Connected || a.dials.Load() != 1 {
			t.Fatalf("start: %s, %d dials", s, a.dials.Load())
		}
		if _, err := m.Execute(t.Context(), "s", "break"); err == nil {
			t.Fatal("break: no error")
		}
		time.Sleep(10 * time.Second) // the backoff, far below the idle probe
		synctest.Wait()
		if n := a.dials.Load(); n != 2 {
			t.Errorf("%d dials after the break, want 2", n)
		}
		if !a.conn(0).closed.Load() {
			t.Error("the broken connection is still open")
		}
		if got, err := m.Execute(t.Context(), "s", "after"); err != nil || got != "after" {
			t.Errorf("after: %q %v", got, err)
		}
	})
}

// TestReloadDoesNotWaitForRunningCommands covers PROT-003: a reload (a new
// pin) dials the next connection at once; a command still running ends on
// the old connection, which closes after it.
func TestReloadDoesNotWaitForRunningCommands(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, a, cancel := startParallel(t)
		defer func() { cancel(); m.Wait() }()
		held := make(chan error, 1)
		go func() {
			got, err := m.Execute(t.Context(), "s", "hold")
			if err == nil && got != "hold" {
				err = io.ErrUnexpectedEOF
			}
			held <- err
		}()
		synctest.Wait()
		if err := m.Reload(t.Context(), "s"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if n := a.dials.Load(); n != 2 {
			t.Fatalf("%d dials while a command runs, want 2", n)
		}
		if a.conn(0).closed.Load() {
			t.Fatal("the old connection closed under its running command")
		}
		if got, err := m.Execute(t.Context(), "s", "new"); err != nil || got != "new" {
			t.Errorf("new connection: %q %v", got, err)
		}
		close(a.release)
		if err := <-held; err != nil {
			t.Errorf("held command: %v", err)
		}
		synctest.Wait()
		if !a.conn(0).closed.Load() {
			t.Error("the old connection stays open after its command")
		}
	})
}
