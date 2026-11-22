// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/diag"
	"github.com/NightDragon1/rconductor/internal/logmask"
	"github.com/NightDragon1/rconductor/internal/store"
)

const c382Agent = "00000000-0000-4000-8000-0000000000a1"

// rateLimited refuses every order for its rate limit.
type rateLimited struct{}

func (rateLimited) RequestData(context.Context, string, string, any, func(agentproto.Envelope)) (agentproto.Result, error) {
	return agentproto.Result{Code: agentproto.CodeRateLimited}, nil
}

// listMask masks with the built-in list.
type listMask struct{}

func (listMask) Lines(lines []string, _ [][]string) ([]string, int) {
	return logmask.ListLines(lines), 0
}

func c382Fixture(t *testing.T, agents diag.Agents) *apiFixture {
	t.Helper()
	f := newAPIFixtureDiag(t, Proxies{}, nil, func(st *store.Store) *diag.Service {
		d := diag.NewService(st, nil)
		d.Use(agents, listMask{})
		return d
	})
	for _, q := range []string{
		`UPDATE settings SET value = '1' WHERE key = 'diagnostics.parallel'`,
		`INSERT INTO agents (id, name, public_key, fingerprint, status, remote, created_at) VALUES ('` + c382Agent + `', 'a', x'00', 'fp', 'active', '', 't')`,
	} {
		if _, err := f.st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// TestC382ExportBusyThroughTheStack (D-090): over the whole handler stack,
// a second export while diagnostics.parallel packages are built is 503
// busy with Retry-After.
func TestC382ExportBusyThroughTheStack(t *testing.T) {
	h := heldAgent{asked: make(chan struct{}), release: make(chan struct{})}
	f := c382Fixture(t, h)
	admin := f.login(t, "Boss")
	first := make(chan int, 1)
	go func() {
		first <- f.do(t, call{method: http.MethodGet, path: "/api/v1/diagnostics", cookies: []*http.Cookie{admin}}).Code
	}()
	<-h.asked
	rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/diagnostics", cookies: []*http.Cookie{admin}})
	close(h.release)
	if code := <-first; code != http.StatusOK {
		t.Errorf("first export = %d", code)
	}
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), `"reason":"busy"`) {
		t.Errorf("second export = %d, Retry-After %q, %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}

// TestC383SystemLogBusyThroughTheStack (D-091): the log of an agent that
// takes no orders for now is 503 busy with Retry-After over the whole
// handler stack.
func TestC383SystemLogBusyThroughTheStack(t *testing.T) {
	f := c382Fixture(t, rateLimited{})
	admin := f.login(t, "Boss")
	rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/system-log?source=" + c382Agent, cookies: []*http.Cookie{admin}})
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "10" || !strings.Contains(rec.Body.String(), `"reason":"busy"`) {
		t.Errorf("system log = %d, Retry-After %q, %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}
