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

// TestForcedPasswordChange covers AUTH-010 over HTTP: with the flag set
// only /me/password, /auth/logout and /auth/session work.
func TestForcedPasswordChange(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	admin := f.login(t, "Boss")
	rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/users/" + f.userID + "/password-reset", cookies: []*http.Cookie{admin}, origin: "http://panel.lan"})
	var reset api.PasswordReset
	if err := json.Unmarshal(rec.Body.Bytes(), &reset); err != nil || rec.Code != http.StatusOK || len(reset.Password) != 20 {
		t.Fatalf("reset = %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("temporary password may be cached")
	}

	r := f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/login", body: loginBody("Mod", reset.Password), origin: "http://panel.lan"})
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"must_change_password":true`) {
		t.Fatalf("login with temporary password = %d %s", r.Code, r.Body.String())
	}
	mod := sessionCookie(t, r, "rcd_session")
	rec = f.do(t, call{method: http.MethodGet, path: "/api/v1/me", cookies: []*http.Cookie{mod}})
	var p api.Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != http.StatusForbidden || p.Reason == nil || *p.Reason != api.ReasonCodePasswordChangeRequired {
		t.Errorf("GET /me before the change = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/auth/session", cookies: []*http.Cookie{mod}}); rec.Code != http.StatusOK {
		t.Errorf("session before the change = %d", rec.Code)
	}
	body := `{"current_password":"` + reset.Password + `","new_password":"my own new password"}`
	if rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/me/password", body: body, cookies: []*http.Cookie{mod}, origin: "http://panel.lan"}); rec.Code != http.StatusNoContent {
		t.Fatalf("change = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/me", cookies: []*http.Cookie{mod}}); rec.Code != http.StatusOK {
		t.Errorf("GET /me after the change = %d %s", rec.Code, rec.Body.String())
	}
}

func TestProfilesAPI(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	mod := f.login(t, "Mod")
	rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/profiles", cookies: []*http.Cookie{mod}})
	var list api.ProfileList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("profiles = %d %s", rec.Code, rec.Body.String())
	}
	if len(list.Items) != 1 || list.Items[0].Slug != "minecraft-java" || len(list.Items[0].Catalog) != 5 {
		t.Errorf("profiles = %+v", list.Items)
	}
	if list.Items[0].MaxLength != 1000 || list.Items[0].Wrappers[0] != "execute" || list.Items[0].LastError != nil {
		t.Errorf("profile details = %+v", list.Items[0])
	}
}
