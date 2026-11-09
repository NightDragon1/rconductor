// SPDX-License-Identifier: AGPL-3.0-or-later

package connmgr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi/httpapitest"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
	"github.com/NightDragon1/rconductor/internal/rcon/source"
)

type fakeSource struct {
	mu      sync.Mutex
	targets map[string]Target
}

func (f *fakeSource) Targets(context.Context) ([]Target, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Target
	for _, t := range f.targets {
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeSource) Target(_ context.Context, id string) (Target, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.targets[id]
	return t, ok, nil
}

func (f *fakeSource) set(t Target) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targets[t.ID] = t
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type fixture struct {
	srv  *rcontest.Server
	src  *fakeSource
	m    *Manager
	logs *lockedBuf

	mu      sync.Mutex
	changes []Status
}

func (f *fixture) history() []Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Status(nil), f.changes...)
}

func newFixture(t *testing.T, password string, handler func(string) string) *fixture {
	t.Helper()
	srv, err := rcontest.Start("secret", rcontest.Minecraft, handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	src := &fakeSource{targets: map[string]Target{
		"s1": {ID: "s1", Name: "mc", Protocol: "source-rcon", Addr: srv.Addr, Password: logging.Secret(password), Usable: true},
	}}
	logs := &lockedBuf{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("rcon")
	f := &fixture{srv: srv, src: src, logs: logs}
	m := New(rcon.Registry{"source-rcon": source.Adapter{}}, src, log, Options{
		ProbeInterval: 50 * time.Millisecond, MaxBackoff: 200 * time.Millisecond,
		IdleProbe: 100 * time.Millisecond, ProbeTimeout: 300 * time.Millisecond,
		OnStatus: func(_ string, s Status) {
			f.mu.Lock()
			f.changes = append(f.changes, s)
			f.mu.Unlock()
		},
	})
	f.m = m
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); m.Wait() })
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) waitStatus(t *testing.T, want Status) time.Duration {
	t.Helper()
	start := time.Now()
	for {
		if s, _ := f.m.Status("s1"); s == want {
			return time.Since(start)
		}
		if time.Since(start) > 10*time.Second {
			s, _ := f.m.Status("s1")
			t.Fatalf("status %s, want %s within 10 s", s, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestOneConnectionManyUsers covers PROT-002: three users send 20
// commands each at the same time; there is exactly one connection and
// every answer reaches its sender.
func TestOneConnectionManyUsers(t *testing.T) {
	f := newFixture(t, "secret", func(cmd string) string {
		time.Sleep(time.Millisecond)
		return "answer to " + cmd
	})
	f.waitStatus(t, Connected)
	var wg sync.WaitGroup
	errs := make(chan error, 60)
	for u := range 3 {
		for c := range 20 {
			wg.Go(func() {
				cmd := fmt.Sprintf("say user%d-%d", u, c)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				got, err := f.m.Execute(ctx, "s1", cmd)
				if err != nil || got != "answer to "+cmd {
					errs <- fmt.Errorf("%s: %q %w", cmd, got, err)
				}
			})
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := f.srv.Connections(); n != 1 {
		t.Errorf("connections = %d, want 1", n)
	}
	if n := f.srv.Commands(); n != 60 {
		t.Errorf("commands at the server = %d, want 60", n)
	}
	if c := logtest.WaitCount(t, f.logs, `"event":"rcon.connected"`, 1); c != 1 {
		t.Errorf("rcon.connected %d times", c)
	}
}

// TestReconnectAfterRestart covers the second half of PROT-002 and
// SRV-004: a game server restart drops the connection; the panel notices
// it while idle and connects again on its own.
func TestReconnectAfterRestart(t *testing.T) {
	f := newFixture(t, "secret", nil)
	f.waitStatus(t, Connected)
	f.srv.DropConnections()
	start := time.Now()
	for f.srv.Connections() < 2 {
		if time.Since(start) > 10*time.Second {
			t.Fatal("no reconnect within 10 s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.waitStatus(t, Connected)
	t.Logf("reconnected after %v", time.Since(start))
	if h := f.history(); len(h) != 3 || h[0] != Connected || h[1] != Offline || h[2] != Connected {
		t.Errorf("status changes = %v, want connected, offline, connected", h)
	}
	if got, err := f.m.Execute(context.Background(), "s1", "list"); err != nil || got != "list" {
		t.Errorf("after reconnect: %q %v", got, err)
	}
	logtest.Wait(t, f.logs, `"event":"rcon.disconnected"`, `"event":"rcon.reconnect"`)
	if c := logtest.WaitCount(t, f.logs, `"event":"rcon.connected"`, 2); c != 2 {
		t.Errorf("rcon.connected %d times, want 2", c)
	}
}

// TestStatusChanges covers SRV-004: wrong password shows auth_failed, a
// stopped game shows offline, a fix shows connected, each within 10 s.
func TestStatusChanges(t *testing.T) {
	f := newFixture(t, "wrong", nil)
	f.waitStatus(t, AuthFailed)
	logtest.Wait(t, f.logs, `"event":"rcon.auth_failed"`)
	if _, err := f.m.Execute(context.Background(), "s1", "list"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("command without connection: %v", err)
	}
	t1, _, _ := f.src.Target(context.Background(), "s1")
	t1.Password = "secret"
	f.src.set(t1)
	if err := f.m.Reload(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(t, Connected)
	f.srv.Close()
	f.waitStatus(t, Offline)
}

func TestUnusableCredentials(t *testing.T) {
	f := newFixture(t, "secret", nil)
	f.waitStatus(t, Connected)
	t1, _, _ := f.src.Target(context.Background(), "s1")
	t1.Usable = false
	f.src.set(t1)
	_ = f.m.Reload(context.Background(), "s1")
	f.waitStatus(t, Offline)
	// Deleting the server ends its worker.
	f.src.mu.Lock()
	delete(f.src.targets, "s1")
	f.src.mu.Unlock()
	_ = f.m.Reload(context.Background(), "s1")
	if _, err := f.m.Execute(context.Background(), "s1", "list"); !errors.Is(err, ErrUnknownServer) {
		t.Errorf("deleted server: %v", err)
	}
}

func TestCommandTimeoutKeepsConnection(t *testing.T) {
	f := newFixture(t, "secret", func(cmd string) string {
		if cmd == "slow" {
			time.Sleep(300 * time.Millisecond)
		}
		return cmd
	})
	f.waitStatus(t, Connected)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := f.m.Execute(ctx, "s1", "slow"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("slow command: %v", err)
	}
	if got, err := f.m.Execute(context.Background(), "s1", "fast"); err != nil || got != "fast" {
		t.Errorf("next command after a timeout: %q %v", got, err)
	}
	if n := f.srv.Connections(); n != 1 {
		t.Errorf("connections = %d", n)
	}
}

// TestHalfOpenConnection covers the idle probe of Design 3.8 for SRV-004:
// a game server that goes silent without closing the connection, as after
// a host crash, shows offline once the probe gets no answer; the panel
// reconnects when the server is back.
func TestHalfOpenConnection(t *testing.T) {
	f := newFixture(t, "secret", nil)
	f.waitStatus(t, Connected)
	f.srv.Freeze()
	if d := f.waitStatus(t, Offline); d > 2*time.Second {
		t.Errorf("offline after %v", d)
	}
	logtest.Wait(t, f.logs, `"event":"rcon.disconnected","reason":"no_answer"`)
	f.srv.Thaw()
	f.waitStatus(t, Connected)
	if got, err := f.m.Execute(context.Background(), "s1", "after"); err != nil || got != "after" {
		t.Errorf("after the outage: %q %v", got, err)
	}
}

// downAdapter counts attempts at a server that never answers.
type downAdapter struct{ dials atomic.Int64 }

func (a *downAdapter) Dial(context.Context, string, logging.Secret) (rcon.Conn, error) {
	a.dials.Add(1)
	return nil, errors.New("connection refused")
}

// TestBackoffInVirtualTime covers Design 3.8 without waiting for it: the
// attempts thin out to one per MaxBackoff, at most 20 % sooner.
func TestBackoffInVirtualTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		src := &fakeSource{targets: map[string]Target{}}
		src.set(Target{ID: "s", Name: "s", Protocol: "x", Addr: "x:1", Usable: true})
		down := &downAdapter{}
		m := New(rcon.Registry{"x": down}, src, slog.New(slog.DiscardHandler), Options{})
		if err := m.Start(ctx); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Minute)
		first := down.dials.Load()
		time.Sleep(10 * time.Minute)
		n := down.dials.Load() - first
		cancel()
		m.Wait()
		if n < 100 || n > 150 {
			t.Errorf("%d attempts in 10 minutes, want about one per 5 s", n)
		}
	})
}

// TestPlayersOverTheConnection covers PROT-003: a connection with an HTTPS
// API tells its players through the queue without a console command; an
// RCON connection cannot, and stays connected.
func TestPlayersOverTheConnection(t *testing.T) {
	f := newFixture(t, "secret", func(string) string { return "ok" })
	f.waitStatus(t, Connected)
	ctx := context.Background()
	if _, err := f.m.Players(ctx, "s1"); !errors.Is(err, ErrNoPlayerCounter) {
		t.Errorf("rcon players: %v, want ErrNoPlayerCounter", err)
	}
	if s, _ := f.m.Status("s1"); s != Connected {
		t.Errorf("status after the players question: %s", s)
	}
	if _, err := f.m.Players(ctx, "nope"); !errors.Is(err, ErrUnknownServer) {
		t.Errorf("unknown server: %v", err)
	}

	s := httpapitest.Start(httpapitest.Fixtures["satisfactory"], "tok", 3, func(string) string { return "ok" })
	t.Cleanup(s.Close)
	src := &fakeSource{targets: map[string]Target{
		"h1": {ID: "h1", Name: "sf", Protocol: httpapi.Protocol, Addr: s.Addr(), Password: "tok", Usable: true,
			Fingerprint: s.Fingerprint, Dialect: "satisfactory", Parallel: 2},
	}}
	m := New(rcon.Registry{httpapi.Protocol: httpapi.Adapter{}}, src, logging.New(&lockedBuf{}, "panel", config.Log{Format: "json"}).Module("rcon"),
		Options{ProbeInterval: 50 * time.Millisecond, MaxBackoff: 200 * time.Millisecond})
	cctx, cancel := context.WithCancel(ctx)
	t.Cleanup(func() { cancel(); m.Wait() })
	if err := m.Start(cctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if st, _ := m.Status("h1"); st == Connected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("http-api server not connected")
		}
		time.Sleep(10 * time.Millisecond)
	}
	before := s.Requests.Load()
	p, err := m.Players(ctx, "h1")
	if err != nil || p.Count != 3 || p.Max != 4 || len(p.Names) != 0 {
		t.Errorf("players = %+v %v, want 3 of 4 without names", p, err)
	}
	if s.Requests.Load() != before+1 {
		t.Errorf("players took %d requests, want 1", s.Requests.Load()-before)
	}
}
