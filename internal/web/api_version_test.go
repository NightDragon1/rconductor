// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// UI-015: every role learns the panel version from GET /auth/session,
// and the login answer carries it as well.
func TestSessionCarriesPanelVersion(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "0.10"
	t.Cleanup(func() { buildinfo.Version = old })

	f := newAPIFixture(t, Proxies{})
	hash := f.passwordHash(t)
	storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "Sadmin", Role: "server_admin", PasswordHash: hash, CreatedBy: f.adminID})
	storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "Low", Role: "supporter", PasswordHash: hash, CreatedBy: f.adminID})

	for _, tc := range []struct{ pseudonym, role string }{
		{"Boss", "rcon_admin"}, {"Sadmin", "server_admin"}, {"Mod", "moderator"}, {"Low", "supporter"},
	} {
		t.Run(tc.role, func(t *testing.T) {
			rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/login", body: loginBody(tc.pseudonym, testPassword), origin: "http://panel.lan"})
			var login api.Session
			if err := json.Unmarshal(rec.Body.Bytes(), &login); err != nil || rec.Code != http.StatusOK {
				t.Fatalf("login = %d %s", rec.Code, rec.Body.String())
			}
			if login.PanelVersion != "0.10" {
				t.Errorf("login panel_version = %q", login.PanelVersion)
			}
			c := sessionCookie(t, rec, "rcd_session")
			rec = f.do(t, call{method: http.MethodGet, path: "/api/v1/auth/session", cookies: []*http.Cookie{c}})
			var s api.Session
			if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil || rec.Code != http.StatusOK {
				t.Fatalf("session = %d %s", rec.Code, rec.Body.String())
			}
			if string(s.User.Role) != tc.role || s.PanelVersion != "0.10" {
				t.Errorf("session role %q panel_version %q", s.User.Role, s.PanelVersion)
			}
		})
	}
}
