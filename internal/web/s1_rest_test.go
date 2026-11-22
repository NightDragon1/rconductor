// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// TestC85ProfilePortsSupporter: a supporter sees the profiles without any
// port (D-056) and with formatting (CMD-017), as a moderator does.
func TestC85ProfilePortsSupporter(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	var hash string
	if err := f.st.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, f.userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "Sup", Role: "supporter", PasswordHash: hash, CreatedBy: f.adminID})
	admin := f.login(t, "Boss")
	if rec := f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: "http://panel.lan",
		body: `{"supporters.enabled":true}`}); rec.Code != http.StatusOK {
		t.Fatalf("supporters on: %d %s", rec.Code, rec.Body.String())
	}
	rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/profiles", cookies: []*http.Cookie{f.login(t, "Sup")}})
	var raw struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || len(raw.Items) == 0 {
		t.Fatalf("supporter: %d %s", rec.Code, rec.Body.String())
	}
	seen := false
	for _, p := range raw.Items {
		seen = seen || string(p["formatting"]) == `"minecraft"`
		_, rcon := p["default_port"]
		_, game := p["game_ports"]
		if rcon || game {
			t.Fatalf("supporter: default_port present=%t, game_ports present=%t", rcon, game)
		}
	}
	if !seen {
		t.Error("supporter: no profile with formatting minecraft")
	}
}

// TestC144NullOnOffSetting: null for an on/off setting is refused over the
// API (422 as the contract says); value and version stay as they were.
func TestC144NullOnOffSetting(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	admin := f.login(t, "Boss")
	patch := func(body string) int {
		return f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: "http://panel.lan", body: body}).Code
	}
	if got := patch(`{"ui.shortcuts_enabled":true}`); got != http.StatusOK {
		t.Fatalf("set = %d", got)
	}
	read := func() string {
		var out string
		if err := f.st.DB.QueryRow(`SELECT group_concat(key || '=' || value || '@' || updated_at, ';') FROM
			(SELECT * FROM settings WHERE key IN ('ui.shortcuts_enabled', 'supporters.enabled') ORDER BY key)`).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := read()
	for _, body := range []string{`{"ui.shortcuts_enabled":null}`, `{"supporters.enabled":null}`} {
		if got := patch(body); got != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d, want 422", body, got)
		}
	}
	if after := read(); after != before {
		t.Errorf("after null: %s, want %s", after, before)
	}
}
