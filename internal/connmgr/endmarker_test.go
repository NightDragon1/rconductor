// SPDX-License-Identifier: AGPL-3.0-or-later

package connmgr

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
	"github.com/NightDragon1/rconductor/internal/rcon/source"
)

// pingCounter is the Source adapter with a count of idle probes.
type pingCounter struct {
	source.Adapter
	pings atomic.Int64
}

type countedConn struct {
	*source.Conn
	a *pingCounter
}

func (c countedConn) Ping(ctx context.Context) error {
	c.a.pings.Add(1)
	return c.Conn.Ping(ctx)
}

func (a *pingCounter) DialEndpoint(ctx context.Context, e rcon.Endpoint) (rcon.Conn, error) {
	c, err := a.Adapter.DialEndpoint(ctx, e)
	if err != nil {
		return nil, err
	}
	return countedConn{Conn: c.(*source.Conn), a: a}, nil
}

// TestO8FactorioThroughManager covers PROT-001 and SRV-004 for a server
// that refuses packets of type 0, like Factorio: with the end marker of
// the target the commands are answered, the idle probes keep the one
// connection, and the server never sees a packet it refuses.
func TestO8FactorioThroughManager(t *testing.T) {
	srv, err := rcontest.Start("secret", rcontest.Factorio, func(cmd string) string { return "answer to " + cmd })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	src := &fakeSource{targets: map[string]Target{
		"s1": {ID: "s1", Name: "factorio", Protocol: "source-rcon", Addr: srv.Addr, Password: "secret", Usable: true, EndMarker: source.EndMarkerNone},
	}}
	a := &pingCounter{}
	m := New(rcon.Registry{"source-rcon": a}, src, logging.New(&lockedBuf{}, "panel", config.Log{Format: "json"}).Module("rcon"), Options{
		ProbeInterval: 20 * time.Millisecond, MaxBackoff: 200 * time.Millisecond,
		IdleProbe: 20 * time.Millisecond, ProbeTimeout: 300 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); m.Wait() })
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	f := &fixture{srv: srv, m: m}
	f.waitStatus(t, Connected)
	for _, cmd := range []string{"/version", "/players online", "/time"} {
		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Second)
		got, err := m.Execute(cctx, "s1", cmd)
		ccancel()
		if err != nil || got != "answer to "+cmd {
			t.Fatalf("%s = %q, %v", cmd, got, err)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for a.pings.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("%d idle probes within 10 s", a.pings.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s, _ := m.Status("s1"); s != Connected {
		t.Errorf("status %s after idle probes", s)
	}
	if n := srv.Connections(); n != 1 {
		t.Errorf("connections = %d, want 1", n)
	}
	if n := srv.Invalid(); n != 0 {
		t.Errorf("server refused %d packets as invalid message type 0", n)
	}
}

// TestO8EndpointCarriesEndMarker: the end marker of a target reaches the
// adapter.
func TestO8EndpointCarriesEndMarker(t *testing.T) {
	if e := (Target{EndMarker: source.EndMarkerNone}).endpoint(); e.EndMarker != source.EndMarkerNone {
		t.Errorf("endpoint end marker %q", e.EndMarker)
	}
}
