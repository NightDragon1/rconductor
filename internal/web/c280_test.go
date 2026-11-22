// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"
	"testing"

	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// TestC280C303EveryOtherRoleRefused: the display name of a host, the
// diagnostic export and the system log are for the RCON admin alone; the
// server admin and the supporter get 403 as the moderator does, each
// refusal audited (UI-021, NFR-018, UI-025).
func TestC280C303EveryOtherRoleRefused(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	var hash string
	if err := f.st.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, f.userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "SA", Role: "server_admin", PasswordHash: hash, CreatedBy: f.adminID})
	storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "Sup", Role: "supporter", PasswordHash: hash, CreatedBy: f.adminID})
	const origin, id = "http://panel.lan", "00000000-0000-4000-8000-000000000001"
	calls := []call{
		{method: http.MethodPatch, path: "/api/v1/agents/" + id, body: `{"display_name":"Rack 1"}`, origin: origin},
		{method: http.MethodGet, path: "/api/v1/diagnostics"},
		{method: http.MethodGet, path: "/api/v1/system-log"},
	}
	for _, who := range []string{"SA", "Sup"} {
		c := f.login(t, who)
		for _, k := range calls {
			k.cookies = []*http.Cookie{c}
			if got := f.do(t, k).Code; got != http.StatusForbidden {
				t.Errorf("%s %s %s = %d, want 403", who, k.method, k.path, got)
			}
		}
	}
	var refused int
	if err := f.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE result = 'denied' AND action IN ('agent.display_name_set', 'diagnostics.exported', 'system_log.viewed')`).Scan(&refused); err != nil || refused != 6 {
		t.Errorf("audited refusals = %d (%v), want 6", refused, err)
	}
}
