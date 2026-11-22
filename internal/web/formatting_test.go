// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// formattingFixture is an API fixture with the shipped profiles by slug and
// a supporter, with supporters on.
type formattingFixture struct {
	*apiFixture
	admin  *http.Cookie
	byslug map[string]api.GameProfile
	supID  string
}

func newFormattingFixture(t *testing.T) formattingFixture {
	t.Helper()
	f := newAPIFixture(t, Proxies{})
	var hash string
	if err := f.st.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, f.userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	supID := storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "Sup", Role: "supporter", PasswordHash: hash, CreatedBy: f.adminID})
	admin := f.login(t, "Boss")
	if rec := f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: "http://panel.lan",
		body: `{"supporters.enabled":true}`}); rec.Code != http.StatusOK {
		t.Fatalf("supporters on: %d %s", rec.Code, rec.Body.String())
	}
	var list api.ProfileList
	_ = json.Unmarshal(f.do(t, call{method: http.MethodGet, path: "/api/v1/profiles", cookies: []*http.Cookie{admin}}).Body.Bytes(), &list)
	byslug := map[string]api.GameProfile{}
	for _, p := range list.Items {
		byslug[p.Slug] = p
	}
	return formattingFixture{apiFixture: f, admin: admin, byslug: byslug, supID: supID}
}

// server creates a server with the profiles of the slugs, top first.
func (f formattingFixture) server(t *testing.T, name string, slugs ...string) string {
	t.Helper()
	ids := ""
	for i, s := range slugs {
		if f.byslug[s].Slug == "" {
			t.Fatalf("no profile %s", s)
		}
		if i > 0 {
			ids += ","
		}
		ids += `"` + f.byslug[s].Id.String() + `"`
	}
	rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/servers", cookies: []*http.Cookie{f.admin}, origin: "http://panel.lan",
		body: `{"name":"` + name + `","profile_ids":[` + ids + `],"host":"mc.lan","password":"pw-123456"}`})
	var srv api.Server
	if err := json.Unmarshal(rec.Body.Bytes(), &srv); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	return srv.Id.String()
}

// grant puts the servers into one server group, grants it to a moderator
// group with the moderator, and the moderator grants each server to the
// supporter.
func (f formattingFixture) grant(t *testing.T, ids ...string) {
	t.Helper()
	rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/server-groups", cookies: []*http.Cookie{f.admin}, origin: "http://panel.lan",
		body: `{"name":"fmt","server_ids":["` + strings.Join(ids, `","`) + `"]}`})
	var g api.ServerGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("group = %d %s", rec.Code, rec.Body.String())
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO user_groups (id, name, kind, owner_id, created_at, updated_at) VALUES ('mg', 'mg', 'moderator', ?, 't', 't')`, []any{f.adminID}},
		{`INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES ('mg', ?, ?, 't')`, []any{f.userID, f.adminID}},
		{`UPDATE users SET supporters_allowed = 1 WHERE id = ?`, []any{f.userID}},
		{`INSERT INTO grants (id, user_group_id, server_group_id, granted_by, created_at) VALUES ('g', 'mg', ?, ?, 't')`, []any{g.Id.String(), f.adminID}},
	} {
		if _, err := f.st.DB.ExecContext(context.Background(), q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range ids {
		if _, err := f.st.DB.Exec(`INSERT INTO supporter_grants (id, supporter_id, moderator_id, server_id, granted_by, created_at) VALUES (?, ?, ?, ?, ?, 't')`,
			"sg-"+id, f.supID, f.userID, id, f.userID); err != nil {
			t.Fatal(err)
		}
	}
	f.checker.Invalidate()
}

// formatting reads the formatting of the effective view and of the server
// as who; nil stands for a missing field.
func (f formattingFixture) formatting(t *testing.T, who, id string) (effective, server *api.Formatting) {
	t.Helper()
	cookie := f.login(t, who)
	var p api.EffectiveProfile
	rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/servers/" + id + "/profile", cookies: []*http.Cookie{cookie}})
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &p) != nil {
		t.Fatalf("%s: profile %d %s", who, rec.Code, rec.Body.String())
	}
	var s api.Server
	rec = f.do(t, call{method: http.MethodGet, path: "/api/v1/servers/" + id, cookies: []*http.Cookie{cookie}})
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &s) != nil {
		t.Fatalf("%s: server %d %s", who, rec.Code, rec.Body.String())
	}
	return p.Formatting, s.Formatting
}

// TestC207StoredFormattingChecked: a stored formatting the panel does not
// know, written without the check of the profile, never reaches the API;
// the field is missing as for no formatting (CMD-017).
func TestC207StoredFormattingChecked(t *testing.T) {
	f := newFormattingFixture(t)
	id := f.server(t, "mc", "minecraft-java")
	f.grant(t, id)
	if _, err := f.st.DB.Exec(`UPDATE game_profiles SET rules = json_set(rules, '$.formatting', '<b>html</b>') WHERE slug = 'minecraft-java'`); err != nil {
		t.Fatal(err)
	}
	for _, who := range []string{"Boss", "Sup"} {
		if eff, srv := f.formatting(t, who, id); eff != nil || srv != nil {
			t.Errorf("%s: effective %v, server %v, want both missing", who, eff, srv)
		}
	}
	rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/profiles", cookies: []*http.Cookie{f.admin}})
	var list api.ProfileList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	for _, p := range list.Items {
		if p.Slug == "minecraft-java" && p.Formatting != nil {
			t.Errorf("profile list: formatting %v, want missing", *p.Formatting)
		}
	}
}

// TestC208EffectiveFormattingSupporter: the topmost profile that sets the
// formatting wins, one without it leaves the field to the profile below,
// and none sets no formatting (D-074, CMD-017), for the supporter as for
// the RCON admin.
func TestC208EffectiveFormattingSupporter(t *testing.T) {
	f := newFormattingFixture(t)
	inherited := f.server(t, "inherited", "source-engine", "minecraft-java")
	none := f.server(t, "none", "source-engine")
	f.grant(t, inherited, none)
	for _, who := range []string{"Boss", "Sup"} {
		if eff, srv := f.formatting(t, who, inherited); eff == nil || *eff != api.Minecraft || srv == nil || *srv != api.Minecraft {
			t.Errorf("%s: profile below sets it: effective %v, server %v, want minecraft", who, eff, srv)
		}
		if eff, srv := f.formatting(t, who, none); eff != nil || srv != nil {
			t.Errorf("%s: no profile sets it: effective %v, server %v, want missing", who, eff, srv)
		}
	}
}
