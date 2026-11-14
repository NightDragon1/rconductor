// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/features"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// TestServerProfileBehindFlagAndScope: GET /servers/{id}/profile (UI-020)
// is 404 while the flag is off and for a server outside the scope; only
// then does the handler answer.
func TestServerProfileBehindFlagAndScope(t *testing.T) {
	flags, err := features.New(features.Flag{Name: "profile_browser", Operations: []string{"GetServerProfile"}})
	if err != nil {
		t.Fatal(err)
	}
	f := newAPIFixtureFeatures(t, Proxies{}, flags)
	admin, mod := f.login(t, "Boss"), f.login(t, "Mod")
	const origin = "http://panel.lan"
	var list api.ProfileList
	_ = json.Unmarshal(f.do(t, call{method: http.MethodGet, path: "/api/v1/profiles", cookies: []*http.Cookie{admin}}).Body.Bytes(), &list)
	rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/servers", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"name":"mc","profile_ids":["` + list.Items[0].Id.String() + `"],"host":"mc.lan","password":"pw-123456"}`})
	var srv api.Server
	if err := json.Unmarshal(rec.Body.Bytes(), &srv); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	get := func(c *http.Cookie, id string) int {
		return f.do(t, call{method: http.MethodGet, path: "/api/v1/servers/" + id + "/profile", cookies: []*http.Cookie{c}}).Code
	}
	id := srv.Id.String()
	if got := get(admin, id); got != http.StatusNotFound {
		t.Errorf("flag off = %d, want 404", got)
	}
	if got := f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"features.profile_browser":true}`}).Code; got != http.StatusOK {
		t.Fatalf("switch flag on = %d", got)
	}
	if got := f.do(t, call{method: http.MethodGet, path: "/api/v1/servers/" + id + "/profile"}).Code; got != http.StatusUnauthorized {
		t.Errorf("without session = %d, want 401", got)
	}
	if got := get(mod, id); got != http.StatusNotFound {
		t.Errorf("moderator without scope = %d, want 404", got)
	}
	if got := get(admin, "00000000-0000-4000-8000-000000000000"); got != http.StatusNotFound {
		t.Errorf("unknown server = %d, want 404", got)
	}
	if got := get(admin, id); got != http.StatusOK {
		t.Errorf("in scope = %d, want 200", got)
	}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if got := f.do(t, call{method: m, path: "/api/v1/servers/" + id + "/profile", cookies: []*http.Cookie{admin}, origin: origin}).Code; got != http.StatusMethodNotAllowed && got != http.StatusNotFound {
			t.Errorf("%s = %d, want no write method", m, got)
		}
	}
}

// TestServerProfileEffectiveView: the server view of the profile browser
// (UI-020) is the backend merge of all profiles of the server (top wins,
// wrappers united, catalog entry from the upper profile), and the game
// ports follow D-056: RCON admin and server admin of the server, not the
// moderator in scope.
func TestServerProfileEffectiveView(t *testing.T) {
	flags, err := features.New(features.Flag{Name: "profile_browser", Operations: []string{"GetServerProfile"}})
	if err != nil {
		t.Fatal(err)
	}
	f := newAPIFixtureFeatures(t, Proxies{}, flags)
	ctx := context.Background()
	var hash string
	if err := f.st.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, f.userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "SA", Role: "server_admin", PasswordHash: hash, CreatedBy: f.adminID})
	admin := f.login(t, "Boss")
	const origin = "http://panel.lan"
	var list api.ProfileList
	_ = json.Unmarshal(f.do(t, call{method: http.MethodGet, path: "/api/v1/profiles", cookies: []*http.Cookie{admin}}).Body.Bytes(), &list)
	byslug := map[string]api.GameProfile{}
	for _, p := range list.Items {
		byslug[p.Slug] = p
	}
	top, base := byslug["minecraft-gtnh"], byslug["minecraft-java"]
	if top.Slug == "" || base.Slug == "" {
		t.Fatalf("profiles = %v", byslug)
	}
	rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/servers", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"name":"mc","profile_ids":["` + top.Id.String() + `","` + base.Id.String() + `"],"host":"mc.lan","password":"pw-123456"}`})
	var srv api.Server
	if err := json.Unmarshal(rec.Body.Bytes(), &srv); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	id := srv.Id.String()
	if got := f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"features.profile_browser":true}`}).Code; got != http.StatusOK {
		t.Fatalf("switch flag on = %d", got)
	}
	// Scope: the server group as admin scope of the server admin and as
	// grant to a moderator group of the RCON admin with the moderator.
	rec = f.do(t, call{method: http.MethodPost, path: "/api/v1/server-groups", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"name":"mc","server_ids":["` + id + `"]}`})
	var g api.ServerGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("group = %d %s", rec.Code, rec.Body.String())
	}
	var saID string
	if err := f.st.DB.QueryRow(`SELECT id FROM users WHERE pseudonym = 'SA'`).Scan(&saID); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO user_groups (id, name, kind, owner_id, created_at, updated_at) VALUES ('mg', 'mg', 'moderator', ?, 't', 't')`, []any{f.adminID}},
		{`INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES ('mg', ?, ?, 't')`, []any{f.userID, f.adminID}},
		{`INSERT INTO admin_scopes (id, user_id, server_group_id, assigned_by, created_at) VALUES ('as', ?, ?, ?, 't')`, []any{saID, g.Id.String(), f.adminID}},
		{`INSERT INTO grants (id, user_group_id, server_group_id, granted_by, created_at) VALUES ('g', 'mg', ?, ?, 't')`, []any{g.Id.String(), f.adminID}},
	} {
		if _, err := f.st.DB.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	f.checker.Invalidate()

	view := func(who string) (map[string]json.RawMessage, api.EffectiveProfile) {
		t.Helper()
		rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/servers/" + id + "/profile", cookies: []*http.Cookie{f.login(t, who)}})
		var raw map[string]json.RawMessage
		var p api.EffectiveProfile
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &raw) != nil || json.Unmarshal(rec.Body.Bytes(), &p) != nil {
			t.Fatalf("%s: %d %s", who, rec.Code, rec.Body.String())
		}
		return raw, p
	}
	for who, ports := range map[string]bool{"Boss": true, "SA": true, "Mod": false} {
		raw, _ := view(who)
		if _, ok := raw["game_ports"]; ok != ports {
			t.Errorf("%s: game_ports present = %t, want %t", who, ok, ports)
		}
	}

	// Same merge as the backend: profile list, syntax and catalog.
	_, got := view("Boss")
	eff, ids, err := profiles.ForServer(ctx, f.st.Queries, id)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := profiles.CatalogForServer(ctx, f.st.Queries, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ProfileIds) != 2 || got.ProfileIds[0].String() != ids[0] || got.ProfileIds[1].String() != ids[1] || ids[0] != top.Id.String() {
		t.Errorf("profile ids = %v, want %v top first", got.ProfileIds, ids)
	}
	if got.Protocol != eff.Protocol.Type || got.CaseSensitive != *eff.Syntax.CaseSensitive || got.StripPrefix != *eff.Syntax.StripPrefix ||
		got.MaxLength != eff.Syntax.MaxLength || len(got.Wrappers) != len(eff.Syntax.Wrappers) || len(got.Separators) != len(eff.Syntax.Separators) {
		t.Errorf("syntax = %+v, want the merge %+v", got, eff.Syntax)
	}
	if len(got.Catalog) != len(cat) || len(cat) == 0 {
		t.Fatalf("catalog = %d entries, want %d", len(got.Catalog), len(cat))
	}
	inTop := map[string]bool{}
	for _, c := range top.Catalog {
		inTop[profiles.NormalizePath(c.Path)] = true
	}
	for i, c := range got.Catalog {
		if c.Path != cat[i].Path || c.Description != cat[i].Description {
			t.Fatalf("catalog[%d] = %q, want %q", i, c.Path, cat[i].Path)
		}
		want := base.Id
		if inTop[profiles.NormalizePath(c.Path)] {
			want = top.Id
		}
		if c.ProfileId != want {
			t.Errorf("%s from %s, want %s", c.Path, c.ProfileId, want)
		}
	}
}
