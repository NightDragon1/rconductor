// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/features"
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
	if got := get(admin, id); got != http.StatusNotImplemented {
		t.Errorf("in scope = %d, want 501 until built", got)
	}
}
