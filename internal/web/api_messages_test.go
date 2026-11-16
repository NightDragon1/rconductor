// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"
	"testing"

	"github.com/NightDragon1/rconductor/internal/features"
)

// TestMessagesBehindFlag: the message operations (MSG-001 to MSG-003) are
// 404 while the flag messages is off, 401 without a session, and answer 501
// with the flag on until the backend is built (contract first).
func TestMessagesBehindFlag(t *testing.T) {
	flags, err := features.New(features.Flag{Name: "messages", Operations: []string{"ListMessages", "CreateMessage", "GetMessage", "DeleteMessage", "MarkMessageRead"}})
	if err != nil {
		t.Fatal(err)
	}
	f := newAPIFixtureFeatures(t, Proxies{}, flags)
	admin := f.login(t, "Boss")
	const origin, id = "http://panel.lan", "00000000-0000-4000-8000-000000000001"
	ops := []call{
		{method: http.MethodGet, path: "/api/v1/messages"},
		{method: http.MethodPost, path: "/api/v1/messages", body: `{"text":"hi","audience":"all"}`, origin: origin},
		{method: http.MethodGet, path: "/api/v1/messages/" + id},
		{method: http.MethodDelete, path: "/api/v1/messages/" + id, origin: origin},
		{method: http.MethodPost, path: "/api/v1/messages/" + id + "/read", origin: origin},
	}
	check := func(want int, session bool) {
		t.Helper()
		for _, c := range ops {
			if session {
				c.cookies = []*http.Cookie{admin}
			}
			if got := f.do(t, c).Code; got != want {
				t.Errorf("%s %s (session %v) = %d, want %d", c.method, c.path, session, got, want)
			}
		}
	}
	check(http.StatusNotFound, true)
	if got := f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"features.messages":true}`}).Code; got != http.StatusOK {
		t.Fatalf("switch flag on = %d", got)
	}
	check(http.StatusUnauthorized, false)
	check(http.StatusNotImplemented, true)
}
