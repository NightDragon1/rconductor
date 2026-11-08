// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi/httpapitest"
)

const token = logging.Secret("api-token-of-the-test")

func echo(cmd string) string { return "ok: " + cmd }

func endpoint(s *httpapitest.Server, dialect, pin string, secret logging.Secret, parallel int) rcon.Endpoint {
	return rcon.Endpoint{Addr: s.Addr(), Secret: secret, Fingerprint: pin, Dialect: dialect, Parallel: parallel}
}

// forEachDialect runs a test for every registered dialect with its fake
// server; a dialect without one fails.
func forEachDialect(t *testing.T, f func(t *testing.T, dialect string, fx httpapitest.Fixture)) {
	for _, name := range httpapi.Names() {
		fx, ok := httpapitest.Fixtures[name]
		if !ok {
			t.Errorf("dialect %s has no fixture in httpapitest", name)
			continue
		}
		t.Run(name, func(t *testing.T) { f(t, name, fx) })
	}
}

// TestPinning covers D-071: before the pin and on a mismatch the token
// never leaves the panel; the connection test reads the fingerprint in a
// handshake that sends no request.
func TestPinning(t *testing.T) {
	forEachDialect(t, func(t *testing.T, dialect string, fx httpapitest.Fixture) {
		ctx := context.Background()
		s := httpapitest.Start(fx, string(token), 2, echo)
		defer s.Close()

		fp, err := httpapi.Adapter{}.Fingerprint(ctx, s.Addr())
		if err != nil || fp != s.Fingerprint {
			t.Fatalf("fingerprint = %q %v, want %q", fp, err, s.Fingerprint)
		}
		if _, err := (httpapi.Adapter{}).DialEndpoint(ctx, endpoint(s, dialect, "", token, 0)); !errors.Is(err, rcon.ErrUnpinned) {
			t.Errorf("unpinned: %v", err)
		}
		if _, err := (httpapi.Adapter{}).Dial(ctx, s.Addr(), token); !errors.Is(err, rcon.ErrUnpinned) {
			t.Errorf("dial without endpoint: %v", err)
		}
		other := strings.Repeat("ab", 32)
		if _, err := (httpapi.Adapter{}).DialEndpoint(ctx, endpoint(s, dialect, other, token, 0)); !errors.Is(err, rcon.ErrCertMismatch) {
			t.Errorf("mismatch: %v", err)
		}
		if n, a := s.Requests.Load(), s.Authorized.Load(); n != 0 || a != 0 {
			t.Fatalf("before a matching pin: %d requests, %d with a token; want none", n, a)
		}

		c, err := httpapi.Adapter{}.DialEndpoint(ctx, endpoint(s, dialect, s.Fingerprint, token, 0))
		if err != nil {
			t.Fatalf("match: %v", err)
		}
		defer func() { _ = c.Close() }()
		if out, err := c.Execute(ctx, "say hi"); err != nil || out != "ok: say hi" {
			t.Errorf("execute = %q %v", out, err)
		}
		if p, err := c.(*httpapi.Conn).Players(ctx); err != nil || p.Count != 2 || p.Max != 4 {
			t.Errorf("players = %+v %v", p, err)
		}
		if s.Authorized.Load() == 0 {
			t.Error("no token sent after the pin matched")
		}
	})
}

// TestWrongTokenAndRedirect: a wrong token is an authentication error; a
// redirect is never followed.
func TestWrongTokenAndRedirect(t *testing.T) {
	forEachDialect(t, func(t *testing.T, dialect string, fx httpapitest.Fixture) {
		ctx := context.Background()
		s := httpapitest.Start(fx, string(token), 0, echo)
		defer s.Close()
		if _, err := (httpapi.Adapter{}).DialEndpoint(ctx, endpoint(s, dialect, s.Fingerprint, "wrong", 0)); !errors.Is(err, rcon.ErrAuth) {
			t.Errorf("wrong token: %v", err)
		}
		c, err := httpapi.Adapter{}.DialEndpoint(ctx, endpoint(s, dialect, s.Fingerprint, token, 0))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close() }()
		s.Redirect.Store(true)
		before := s.Requests.Load()
		if _, err := c.Execute(ctx, "say hi"); err == nil || errors.Is(err, rcon.ErrAuth) {
			t.Errorf("redirect: %v, want an error that is not auth", err)
		}
		if s.Requests.Load() != before+1 {
			t.Errorf("redirect followed: %d requests", s.Requests.Load()-before)
		}
	})
}

// TestAnswerLimitAndCancel: an answer over the limit is an error, not cut;
// a command ends with its context.
func TestAnswerLimitAndCancel(t *testing.T) {
	forEachDialect(t, func(t *testing.T, dialect string, fx httpapitest.Fixture) {
		ctx := context.Background()
		started := make(chan struct{}, 1)
		release := make(chan struct{})
		s := httpapitest.Start(fx, string(token), 0, func(cmd string) string {
			switch cmd {
			case "big":
				return strings.Repeat("x", httpapi.MaxResponse)
			case "hang":
				started <- struct{}{}
				<-release
			}
			return "ok"
		})
		defer s.Close()
		defer close(release)
		c, err := httpapi.Adapter{}.DialEndpoint(ctx, endpoint(s, dialect, s.Fingerprint, token, 0))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close() }()
		if out, err := c.Execute(ctx, "big"); !errors.Is(err, rcon.ErrTooLarge) || out != "" {
			t.Errorf("big answer = %d bytes, %v", len(out), err)
		}
		cctx, cancel := context.WithCancel(ctx)
		go func() { <-started; cancel() }()
		if _, err := c.Execute(cctx, "hang"); !errors.Is(err, context.Canceled) {
			t.Errorf("canceled command: %v", err)
		}
	})
}

// TestParallelUpToTheLimit: at most Parallel requests are open at once.
func TestParallelUpToTheLimit(t *testing.T) {
	forEachDialect(t, func(t *testing.T, dialect string, fx httpapitest.Fixture) {
		ctx := context.Background()
		var mu sync.Mutex
		inflight, peak := 0, 0
		arrived := make(chan struct{}, 10)
		release := make(chan struct{})
		s := httpapitest.Start(fx, string(token), 0, func(string) string {
			mu.Lock()
			inflight++
			peak = max(peak, inflight)
			mu.Unlock()
			arrived <- struct{}{}
			<-release
			mu.Lock()
			inflight--
			mu.Unlock()
			return "ok"
		})
		defer s.Close()
		c, err := httpapi.Adapter{}.DialEndpoint(ctx, endpoint(s, dialect, s.Fingerprint, token, 2))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close() }()
		var wg sync.WaitGroup
		for range 5 {
			wg.Go(func() { _, _ = c.Execute(ctx, "say hi") })
		}
		<-arrived
		<-arrived
		// Two are in; each release lets the next one in.
		for range 5 {
			release <- struct{}{}
		}
		wg.Wait()
		if peak != 2 {
			t.Errorf("peak = %d requests at once, want 2", peak)
		}
	})
}

func TestNormalizeFingerprint(t *testing.T) {
	want := strings.Repeat("ab", 32)
	for _, in := range []string{want, strings.ToUpper(want), "AB:" + strings.TrimPrefix(strings.ToUpper(want), "AB")} {
		if got, ok := httpapi.NormalizeFingerprint(in); !ok || got != want {
			t.Errorf("%q = %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "ab", strings.Repeat("zz", 32), strings.Repeat("ab", 33)} {
		if _, ok := httpapi.NormalizeFingerprint(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}
