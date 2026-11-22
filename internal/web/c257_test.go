// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestC257CursorOverHTTP: next_cursor comes while more follow and leads to
// the next page; a broken cursor or one over 256 characters is 400.
func TestC257CursorOverHTTP(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	admin := f.login(t, "Boss")
	const origin = "http://panel.lan"
	for range 3 {
		if rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/messages", cookies: []*http.Cookie{admin}, origin: origin,
			body: `{"text":"hi","audience":"users","user_ids":["` + f.userID + `"]}`}); rec.Code != http.StatusCreated {
			t.Fatalf("send = %d %s", rec.Code, rec.Body)
		}
	}
	page := func(query string) (int, struct {
		Items      []struct{ ID string }
		NextCursor *string `json:"next_cursor"`
	}) {
		t.Helper()
		rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/messages" + query, cookies: []*http.Cookie{admin}})
		var p struct {
			Items      []struct{ ID string }
			NextCursor *string `json:"next_cursor"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		return rec.Code, p
	}
	code, p := page("?limit=2")
	if code != http.StatusOK || len(p.Items) != 2 || p.NextCursor == nil {
		t.Fatalf("first page %d %+v", code, p)
	}
	code, p2 := page("?limit=2&cursor=" + *p.NextCursor)
	if code != http.StatusOK || len(p2.Items) != 1 || p2.NextCursor != nil {
		t.Errorf("last page %d %+v", code, p2)
	}
	for _, bad := range []string{"%25%25", strings.Repeat("A", 257)} {
		if code, _ := page("?cursor=" + bad); code != http.StatusBadRequest {
			t.Errorf("cursor %.10q = %d, want 400", bad, code)
		}
	}
}
