// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"
	"testing"

	"github.com/NightDragon1/rconductor/internal/features"
)

// TestHostsAndDiagnosticsBehindFlags: the display name of a host (UI-021,
// D-089) and the diagnostic export (NFR-018, D-090) are 404 while their
// flag is off. With the flag on, the skeleton refuses every role but the
// RCON admin (403); the display name of an unknown agent is 404, the
// export checks the period (400) and answers 501 for the rest (contract
// first).
func TestHostsAndDiagnosticsBehindFlags(t *testing.T) {
	flags, err := features.New(features.Flag{Name: "host_groups", Operations: []string{"UpdateAgent"}},
		features.Flag{Name: "diagnostics_export", Operations: []string{"GetDiagnostics"}})
	if err != nil {
		t.Fatal(err)
	}
	f := newAPIFixtureFeatures(t, Proxies{}, flags)
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
	check(http.StatusNotFound, admin, rename, export)
	for _, flag := range []string{"host_groups", "diagnostics_export"} {
		if got := f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: origin,
			body: `{"features.` + flag + `":true}`}).Code; got != http.StatusOK {
			t.Fatalf("switch %s on = %d", flag, got)
		}
	}
	check(http.StatusForbidden, mod, rename, export, call{method: http.MethodGet, path: "/api/v1/diagnostics?from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z"})
	check(http.StatusBadRequest, admin, call{method: http.MethodGet, path: "/api/v1/diagnostics?from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z"})
	check(http.StatusNotFound, admin, rename)
	check(http.StatusNotImplemented, admin, export)
	var refused int
	if err := f.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action IN ('agent.display_name_set', 'diagnostics.exported') AND result = 'denied'`).Scan(&refused); err != nil || refused != 3 {
		t.Errorf("audited refusals = %d (%v), want 3", refused, err)
	}
}
