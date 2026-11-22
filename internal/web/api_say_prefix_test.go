// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// sayPrefixOf reads the prefix of a group as c, with status.
func sayPrefixOf(t *testing.T, f *apiFixture, c *http.Cookie, group string) (api.SayPrefix, int) {
	t.Helper()
	rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/server-groups/" + group + "/say-prefix", cookies: []*http.Cookie{c}})
	var p api.SayPrefix
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
	}
	return p, rec.Code
}

// putSayPrefix writes body as c; ifMatch "" sends no If-Match.
func putSayPrefix(t *testing.T, f *apiFixture, c *http.Cookie, group, body, ifMatch string) (int, string) {
	t.Helper()
	h := map[string]string{}
	if ifMatch != "" {
		h["If-Match"] = ifMatch
	}
	rec := f.do(t, call{method: http.MethodPut, path: "/api/v1/server-groups/" + group + "/say-prefix", cookies: []*http.Cookie{c},
		origin: "http://panel.lan", body: body, headers: h})
	return rec.Code, rec.Body.String()
}

// TestSayPrefix: GET and PUT /server-groups/{id}/say-prefix (CMD-020,
// D-082) are 404 for an unknown group; the RCON admin reads, sets and clears the
// prefix; invalid prefixes are 400, a stale version 409, and every change
// is in the audit.
func TestSayPrefix(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	admin := f.login(t, "Boss")
	const origin = "http://panel.lan"
	rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/server-groups", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"name":"mc"}`})
	var g api.ServerGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("group = %d %s", rec.Code, rec.Body.String())
	}
	id := g.Id.String()
	if got := f.do(t, call{method: http.MethodGet, path: "/api/v1/server-groups/" + id + "/say-prefix"}).Code; got != http.StatusUnauthorized {
		t.Errorf("without session = %d, want 401", got)
	}
	const unknown = "00000000-0000-4000-8000-000000000000"
	if _, got := sayPrefixOf(t, f, admin, unknown); got != http.StatusNotFound {
		t.Errorf("GET unknown group = %d, want 404", got)
	}
	if got, _ := putSayPrefix(t, f, admin, unknown, `{"say_prefix":"[R]"}`, "1"); got != http.StatusNotFound {
		t.Errorf("PUT unknown group = %d, want 404", got)
	}
	p, got := sayPrefixOf(t, f, admin, id)
	if got != http.StatusOK || p.SayPrefix != "" {
		t.Fatalf("GET = %d %+v, want no prefix", got, p)
	}
	for _, bad := range []string{strings.Repeat("x", 17), "[§cR]", "a\u0001", "a\u202eb", "a\nb"} {
		body, _ := json.Marshal(map[string]string{"say_prefix": bad})
		if got, b := putSayPrefix(t, f, admin, id, string(body), p.Version); got != http.StatusBadRequest {
			t.Errorf("prefix %q = %d %s, want 400", bad, got, b)
		}
	}
	// CMD-020: a missing or null say_prefix removes nothing; "" does.
	for _, body := range []string{`{}`, `{"say_prefix":null}`} {
		if got, b := putSayPrefix(t, f, admin, id, body, p.Version); got != http.StatusBadRequest || !strings.Contains(b, "required") {
			t.Errorf("%s = %d %s, want 400", body, got, b)
		}
	}
	if got, b := putSayPrefix(t, f, admin, id, `{"say_prefix":" [R] "}`, p.Version); got != http.StatusOK || !strings.Contains(b, `"say_prefix":"[R]"`) {
		t.Fatalf("set = %d %s", got, b)
	}
	if got, _ := putSayPrefix(t, f, admin, id, `{"say_prefix":"[X]"}`, p.Version); got != http.StatusConflict {
		t.Errorf("stale version = %d, want 409", got)
	}
	q, _ := sayPrefixOf(t, f, admin, id)
	if q.SayPrefix != "[R]" || q.Version == p.Version {
		t.Errorf("after set: %+v, before %+v", q, p)
	}
	if got, _ := putSayPrefix(t, f, admin, id, `{"say_prefix":""}`, q.Version); got != http.StatusOK {
		t.Errorf("clear = %d", got)
	}
	if r, _ := sayPrefixOf(t, f, admin, id); r.SayPrefix != "" {
		t.Errorf("after clear: %+v", r)
	}
	var n int
	if err := f.st.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = 'server_group.say_prefix.update' AND target_id = ?`, id).Scan(&n); err != nil || n != 2 {
		t.Errorf("audit entries = %d %v, want 2", n, err)
	}
}

// TestSayPrefixRoles: the RCON admin and a server
// admin with every server of the group in scope read and write the prefix;
// a server admin with only part of the group in scope gets 404 as outside
// the scope; moderators and supporters get 403 (D-082). The rights come
// before the version: without If-Match only those who may write get 428
// (D-051).
func TestSayPrefixRoles(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	var hash string
	if err := f.st.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, f.userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	saID := storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "SA", Role: "server_admin", PasswordHash: hash, CreatedBy: f.adminID})
	storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "Sup", Role: "supporter", PasswordHash: hash, CreatedBy: f.adminID})
	admin := f.login(t, "Boss")
	const origin = "http://panel.lan"
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
		body, _ := json.Marshal(map[string]any{"name": name, "server_ids": ids})
		rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/server-groups", cookies: []*http.Cookie{admin}, origin: origin, body: string(body)})
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
	sa, mod, sup := f.login(t, "SA"), f.login(t, "Mod"), f.login(t, "Sup")
	for _, tc := range []struct {
		who   string
		c     *http.Cookie
		group string
		want  int // refusal, or 0 for allowed
	}{
		{"RCON admin", admin, mixed, 0},
		{"server admin, whole group in scope", sa, full, 0},
		{"server admin, part of the group in scope", sa, mixed, http.StatusNotFound},
		{"moderator", mod, full, http.StatusForbidden},
		{"supporter", sup, full, http.StatusForbidden},
	} {
		p, get := sayPrefixOf(t, f, tc.c, tc.group)
		noVersion, _ := putSayPrefix(t, f, tc.c, tc.group, `{"say_prefix":"[R]"}`, "")
		version := p.Version
		if version == "" {
			version = "1"
		}
		put, _ := putSayPrefix(t, f, tc.c, tc.group, `{"say_prefix":"[R]"}`, version)
		if tc.want == 0 {
			if get != http.StatusOK || noVersion != http.StatusPreconditionRequired || put != http.StatusOK {
				t.Errorf("%s: GET %d, PUT without If-Match %d, PUT %d; want 200, 428, 200", tc.who, get, noVersion, put)
			}
			continue
		}
		if get != tc.want || noVersion != tc.want || put != tc.want {
			t.Errorf("%s: GET %d, PUT without If-Match %d, PUT %d; want %d each", tc.who, get, noVersion, put, tc.want)
		}
	}
}
