// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/features"
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
