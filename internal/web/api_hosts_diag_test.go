// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"
	"testing"
)

// TestHostsAndDiagnostics: for the display name of a host (UI-021, D-089)
// and the diagnostic export (NFR-018, D-090) every role but the RCON admin is refused
// (403); the display name of an unknown agent is 404, the export checks
// the period (400) and gives the RCON admin the ZIP.
func TestHostsAndDiagnostics(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	admin, mod := f.login(t, "Boss"), f.login(t, "Mod")
	const origin, id = "http://panel.lan", "00000000-0000-4000-8000-000000000001"
	rename := call{method: http.MethodPatch, path: "/api/v1/agents/" + id, body: `{"display_name":"Rack 1"}`, origin: origin}
	export := call{method: http.MethodGet, path: "/api/v1/diagnostics"}
	check := func(want int, who *http.Cookie, calls ...call) {
		t.Helper()
		for _, c := range calls {
			c.cookies = []*http.Cookie{who}
			if got := f.do(t, c).Code; got != want {
				t.Errorf("%s %s = %d, want %d", c.method, c.path, got, want)
			}
		}
	}
	check(http.StatusForbidden, mod, rename, export, call{method: http.MethodGet, path: "/api/v1/diagnostics?from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z"})
	check(http.StatusBadRequest, admin, call{method: http.MethodGet, path: "/api/v1/diagnostics?from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z"})
	check(http.StatusNotFound, admin, rename)
	if rec := f.do(t, call{method: export.method, path: export.path, cookies: []*http.Cookie{admin}}); rec.Code != http.StatusOK ||
		rec.Header().Get("Content-Type") != "application/zip" || rec.Body.Len() == 0 {
		t.Errorf("export = %d %s, %d bytes", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	var refused int
	if err := f.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action IN ('agent.display_name_set', 'diagnostics.exported') AND result = 'denied'`).Scan(&refused); err != nil || refused != 3 {
		t.Errorf("audited refusals = %d (%v), want 3", refused, err)
	}
}
