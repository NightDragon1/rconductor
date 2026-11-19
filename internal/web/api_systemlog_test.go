// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"
	"testing"

	"github.com/NightDragon1/rconductor/internal/features"
)

// TestSystemLogBehindFlag: the system log (UI-025, D-091) is 404 while
// diagnostics_export is off. With the flag on, the skeleton refuses every
// role but the RCON admin (403, audited), then checks source and period
// (400), and answers 501 for the rest (contract first).
func TestSystemLogBehindFlag(t *testing.T) {
	flags, err := features.New(features.Flag{Name: "diagnostics_export", Operations: []string{"GetDiagnostics", "ListSystemLog"}})
	if err != nil {
		t.Fatal(err)
	}
	f := newAPIFixtureFeatures(t, Proxies{}, flags)
	admin, mod := f.login(t, "Boss"), f.login(t, "Mod")
	const agent = "/api/v1/system-log?source=00000000-0000-4000-8000-000000000001"
	panel := call{method: http.MethodGet, path: "/api/v1/system-log"}
	check := func(want int, who *http.Cookie, paths ...string) {
		t.Helper()
		for _, p := range paths {
			if got := f.do(t, call{method: http.MethodGet, path: p, cookies: []*http.Cookie{who}}).Code; got != want {
				t.Errorf("GET %s = %d, want %d", p, got, want)
			}
		}
	}
	check(http.StatusNotFound, admin, panel.path, agent)
	if got := f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: "http://panel.lan",
		body: `{"features.diagnostics_export":true}`}).Code; got != http.StatusOK {
		t.Fatalf("switch on = %d", got)
	}
	check(http.StatusForbidden, mod, panel.path, agent, "/api/v1/system-log?source=nope")
	check(http.StatusBadRequest, admin, "/api/v1/system-log?source=nope", "/api/v1/system-log?source=",
		"/api/v1/system-log?from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z",
		"/api/v1/system-log?from=2026-11-19T00:00:00Z&to=2026-11-18T00:00:00Z",
		"/api/v1/system-log?limit=501", "/api/v1/system-log?level=debug", "/api/v1/system-log?after=0")
	check(http.StatusNotImplemented, admin, panel.path, agent, "/api/v1/system-log?level=error&limit=500&after=7")
	var refused int
	if err := f.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'system_log.viewed' AND result = 'denied'`).Scan(&refused); err != nil || refused != 3 {
		t.Errorf("audited refusals = %d (%v), want 3", refused, err)
	}
}
