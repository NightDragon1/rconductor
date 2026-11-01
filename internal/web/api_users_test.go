// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/api"
)

func (f *apiFixture) login(t *testing.T, pseudonym string) *http.Cookie {
	t.Helper()
	rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/login", body: loginBody(pseudonym, testPassword), origin: "http://panel.lan"})
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s = %d %s", pseudonym, rec.Code, rec.Body.String())
	}
	return sessionCookie(t, rec, "rcd_session")
}

func TestUsersAPI(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	admin := f.login(t, "Boss")
	mod := f.login(t, "Mod")

	rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/users", cookies: []*http.Cookie{admin}})
	var list api.UserList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	if len(list.Items) != 1 || list.Items[0].Pseudonym != "Mod" {
		t.Errorf("admin lists %+v", list.Items)
	}

	// Outside the scope looks like not existing (Design 3.1).
	rec = f.do(t, call{method: http.MethodGet, path: "/api/v1/users/" + f.adminID, cookies: []*http.Cookie{mod}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("moderator reads the admin = %d", rec.Code)
	}
	// ROLE-008: no promotion to the caller's level.
	rec = f.do(t, call{method: http.MethodPatch, path: "/api/v1/users/" + f.userID, body: `{"role":"rcon_admin"}`,
		cookies: []*http.Cookie{admin}, origin: "http://panel.lan"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("promotion = %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(t, call{method: http.MethodPatch, path: "/api/v1/users/" + f.userID, body: `{"role":"supporter"}`,
		cookies: []*http.Cookie{admin}, origin: "http://panel.lan"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"role":"supporter"`) {
		t.Errorf("demotion = %d %s", rec.Code, rec.Body.String())
	}
	// W2: the changed user's session has ended.
	if rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/auth/session", cookies: []*http.Cookie{mod}}); rec.Code != http.StatusUnauthorized {
		t.Errorf("session after rights change = %d", rec.Code)
	}
}
