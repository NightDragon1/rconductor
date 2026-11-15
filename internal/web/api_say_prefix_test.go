// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/features"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// TestSayPrefixBehindFlag: GET and PUT /server-groups/{id}/say-prefix
// (CMD-020, D-082) are 404 while the flag say_sender is off and for an
// unknown group; only then does the handler answer.
func TestSayPrefixBehindFlag(t *testing.T) {
	flags, err := features.New(features.Flag{Name: "say_sender", Operations: []string{"GetServerGroupSayPrefix", "SetServerGroupSayPrefix"}})
	if err != nil {
		t.Fatal(err)
	}
	f := newAPIFixtureFeatures(t, Proxies{}, flags)
	admin := f.login(t, "Boss")
	const origin = "http://panel.lan"
	rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/server-groups", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"name":"mc"}`})
	var g api.ServerGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("group = %d %s", rec.Code, rec.Body.String())
	}
	path := "/api/v1/server-groups/" + g.Id.String() + "/say-prefix"
	get := func(c *http.Cookie, p string) int {
		return f.do(t, call{method: http.MethodGet, path: p, cookies: []*http.Cookie{c}}).Code
	}
	put := func(c *http.Cookie, p string) int {
		return f.do(t, call{method: http.MethodPut, path: p, cookies: []*http.Cookie{c}, origin: origin,
			body: `{"say_prefix":"[R]"}`, headers: map[string]string{"If-Match": "1"}}).Code
	}
	if got, got2 := get(admin, path), put(admin, path); got != http.StatusNotFound || got2 != http.StatusNotFound {
		t.Errorf("flag off = %d, %d, want 404", got, got2)
	}
	if got := f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"features.say_sender":true}`}).Code; got != http.StatusOK {
		t.Fatalf("switch flag on = %d", got)
	}
	if got := f.do(t, call{method: http.MethodGet, path: path}).Code; got != http.StatusUnauthorized {
		t.Errorf("without session = %d, want 401", got)
	}
	unknown := "/api/v1/server-groups/00000000-0000-4000-8000-000000000000/say-prefix"
	if got, got2 := get(admin, unknown), put(admin, unknown); got != http.StatusNotFound || got2 != http.StatusNotFound {
		t.Errorf("unknown group = %d, %d, want 404", got, got2)
	}
	if got, got2 := get(admin, path), put(admin, path); got != http.StatusNotImplemented || got2 != http.StatusNotImplemented {
		t.Errorf("flag on = %d, %d, want 501 (contract first)", got, got2)
	}
}

// TestSayPrefixRolesWithFlag: with the flag on, the RCON admin and a server
// admin with every server of the group in scope reach the handler; a
// server admin with only part of the group in scope gets 404 as outside the
// scope; moderators get 403 (D-082, D-051).
func TestSayPrefixRolesWithFlag(t *testing.T) {
	flags, err := features.New(features.Flag{Name: "say_sender", Operations: []string{"GetServerGroupSayPrefix", "SetServerGroupSayPrefix"}})
	if err != nil {
		t.Fatal(err)
	}
	f := newAPIFixtureFeatures(t, Proxies{}, flags)
	var hash string
	if err := f.st.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, f.userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	saID := storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "SA", Role: "server_admin", PasswordHash: hash, CreatedBy: f.adminID})
	admin := f.login(t, "Boss")
	const origin = "http://panel.lan"
	if got := f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"features.say_sender":true}`}).Code; got != http.StatusOK {
		t.Fatalf("switch flag on = %d", got)
	}
	var list api.ProfileList
	_ = json.Unmarshal(f.do(t, call{method: http.MethodGet, path: "/api/v1/profiles", cookies: []*http.Cookie{admin}}).Body.Bytes(), &list)
	server := func(name string) string {
		t.Helper()
		rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/servers", cookies: []*http.Cookie{admin}, origin: origin,
			body: `{"name":"` + name + `","profile_ids":["` + list.Items[0].Id.String() + `"],"host":"` + name + `.lan","password":"pw-123456"}`})
		var srv api.Server
		if err := json.Unmarshal(rec.Body.Bytes(), &srv); err != nil || rec.Code != http.StatusCreated {
			t.Fatalf("server %s = %d %s", name, rec.Code, rec.Body.String())
		}
		return srv.Id.String()
	}
	group := func(name string, ids ...string) string {
		t.Helper()
		body := `{"name":"` + name + `","server_ids":[`
		for i, id := range ids {
			if i > 0 {
				body += ","
			}
			body += `"` + id + `"`
		}
		rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/server-groups", cookies: []*http.Cookie{admin}, origin: origin, body: body + "]}"})
		var g api.ServerGroup
		if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil || rec.Code != http.StatusCreated {
			t.Fatalf("group %s = %d %s", name, rec.Code, rec.Body.String())
		}
		return g.Id.String()
	}
	a, b := server("a"), server("b")
	full, mixed := group("full", a), group("mixed", a, b)
	if _, err := f.st.DB.Exec(`INSERT INTO admin_scopes (id, user_id, server_group_id, assigned_by, created_at) VALUES ('as', ?, ?, ?, 't')`,
		saID, full, f.adminID); err != nil {
		t.Fatal(err)
	}
	f.checker.Invalidate()
	sa, mod := f.login(t, "SA"), f.login(t, "Mod")
	for _, tc := range []struct {
		who   string
		c     *http.Cookie
		group string
		want  int
	}{
		{"RCON admin", admin, mixed, http.StatusNotImplemented},
		{"server admin, whole group in scope", sa, full, http.StatusNotImplemented},
		{"server admin, part of the group in scope", sa, mixed, http.StatusNotFound},
		{"moderator", mod, full, http.StatusForbidden},
	} {
		path := "/api/v1/server-groups/" + tc.group + "/say-prefix"
		get := f.do(t, call{method: http.MethodGet, path: path, cookies: []*http.Cookie{tc.c}}).Code
		put := f.do(t, call{method: http.MethodPut, path: path, cookies: []*http.Cookie{tc.c}, origin: origin,
			body: `{"say_prefix":"[R]"}`, headers: map[string]string{"If-Match": "1"}}).Code
		if get != tc.want || put != tc.want {
			t.Errorf("%s: GET %d, PUT %d, want %d", tc.who, get, put, tc.want)
		}
	}
}
