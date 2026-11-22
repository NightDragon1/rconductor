// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package connmgr

import (
	"context"
	"errors"
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
	holdErr error // the answer of "hold" after the release
	players error // the answer of Players
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

func (c *parallelConn) Players(context.Context) (rcon.Players, error) {
	return rcon.Players{Count: 1}, c.a.players
}

func (c *parallelConn) Execute(ctx context.Context, command string) (string, error) {
	switch command {
	case "break":
		return "", io.EOF
	case "fail":
		return "", errors.New("HTTP 500") // an error answer of the game
	case "hold":
		select {
		case <-c.a.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if c.a.holdErr != nil {
			return "", c.a.holdErr
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

func startParallel(t *testing.T, holdErr error) (*Manager, *parallelAdapter, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	src := &fakeSource{targets: map[string]Target{}}
	src.set(Target{ID: "s", Name: "s", Protocol: "x", Addr: "x:1", Usable: true})
	a := &parallelAdapter{release: make(chan struct{}), holdErr: holdErr}
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
		m, a, cancel := startParallel(t, nil)
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
		m, a, cancel := startParallel(t, nil)
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

// TestErrorAnswerKeepsConcurrentConnection: an error answer of the game is
// no lost connection; it stays, without a new dial (PROT-003).
func TestErrorAnswerKeepsConcurrentConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, a, cancel := startParallel(t, nil)
		defer func() { cancel(); m.Wait() }()
		if _, err := m.Execute(t.Context(), "s", "fail"); err == nil {
			t.Fatal("fail: no error")
		}
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if n := a.dials.Load(); n != 1 || a.conn(0).closed.Load() {
			t.Errorf("%d dials, closed %v after an error answer", n, a.conn(0).closed.Load())
		}
	})
}

// TestBreakWhileCommandsRun: a break ends the connection at once also when
// other commands still run on it; they end on the old one (PROT-003).
func TestBreakWhileCommandsRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, a, cancel := startParallel(t, nil)
		defer func() { cancel(); m.Wait() }()
		held := make(chan error, 1)
		go func() {
			_, err := m.Execute(t.Context(), "s", "hold")
			held <- err
		}()
		synctest.Wait()
		if _, err := m.Execute(t.Context(), "s", "break"); err == nil {
			t.Fatal("break: no error")
		}
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if n := a.dials.Load(); n != 2 {
			t.Errorf("%d dials while a command runs, want 2", n)
		}
		close(a.release)
		if err := <-held; err != nil {
			t.Errorf("held command: %v", err)
		}
	})
}

// TestPlayersBreakRedials: the player poll finds a lost transport as a
// command does (PROT-003).
func TestPlayersBreakRedials(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, a, cancel := startParallel(t, nil)
		defer func() { cancel(); m.Wait() }()
		a.players = io.EOF
		if _, err := m.Players(t.Context(), "s"); err == nil {
			t.Fatal("players: no error")
		}
		a.players = nil
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if n := a.dials.Load(); n != 2 {
			t.Errorf("%d dials after a lost poll, want 2", n)
		}
	})
}

// TestOldConnectionAfterReloadSetsNoStatus: a mismatch on the old
// connection after a reload reaches only its caller; the new connection
// stays connected (PROT-003, D-071).
func TestOldConnectionAfterReloadSetsNoStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, a, cancel := startParallel(t, rcon.ErrCertMismatch)
		defer func() { cancel(); m.Wait() }()
		held := make(chan error, 1)
		go func() {
			_, err := m.Execute(t.Context(), "s", "hold")
			held <- err
		}()
		synctest.Wait()
		if err := m.Reload(t.Context(), "s"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		close(a.release)
		if err := <-held; !errors.Is(err, rcon.ErrCertMismatch) {
			t.Errorf("held command: %v, want the mismatch", err)
		}
		synctest.Wait()
		if s, _ := m.Status("s"); s != Connected {
			t.Errorf("status %s after the old mismatch, want connected", s)
		}
		if got, err := m.Execute(t.Context(), "s", "new"); err != nil || got != "new" {
			t.Errorf("new connection: %q %v", got, err)
		}
	})
}
