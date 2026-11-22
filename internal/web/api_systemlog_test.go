// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/api"
)

// TestSystemLog: for the system log (UI-025, D-091) the skeleton refuses every
// role but the RCON admin (403, audited), then checks source and period
// (400); the panel's entries come from the level up and after an ID, an
// agent that is not connected gives 503.
func TestSystemLog(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	admin, mod := f.login(t, "Boss"), f.login(t, "Mod")
	const agent = "/api/v1/system-log?source=00000000-0000-4000-8000-000000000001"
	panel := call{method: http.MethodGet, path: "/api/v1/system-log"}
	check := func(want int, who *http.Cookie, paths ...string) {
		t.Helper()
		for _, p := range paths {
			if got := f.do(t, call{method: http.MethodGet, path: p, cookies: []*http.Cookie{who}}).Code; got != want {
				t.Errorf("GET %s = %d, want %d", p, got, want)
			}
		}
	}
	check(http.StatusForbidden, mod, panel.path, agent, "/api/v1/system-log?source=nope")
	check(http.StatusBadRequest, admin, "/api/v1/system-log?source=nope", "/api/v1/system-log?source=",
		"/api/v1/system-log?from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z",
		"/api/v1/system-log?from=2026-11-19T00:00:00Z&to=2026-11-18T00:00:00Z",
		"/api/v1/system-log?limit=501", "/api/v1/system-log?level=debug", "/api/v1/system-log?after=0")
	check(http.StatusServiceUnavailable, admin, agent)
	// The panel: from the level up, oldest first, polling after an ID.
	now := time.Now().UTC()
	for i, level := range []string{"info", "warn", "error", "info"} {
		if _, err := f.st.DB.Exec(`INSERT INTO system_log (ts, level, code, text, size) VALUES (?, ?, 'x.y', ?, 1)`,
			now.Add(time.Duration(i-10)*time.Minute).Format("2006-01-02T15:04:05.000Z"), level, fmt.Sprint("line ", i)); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) (texts []string) {
		t.Helper()
		rec := f.do(t, call{method: http.MethodGet, path: path, cookies: []*http.Cookie{admin}})
		var p api.SystemLogPage
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", path, rec.Code, rec.Body)
		}
		for _, e := range p.Items {
			texts = append(texts, e.Text)
		}
		return texts
	}
	if got := strings.Join(read("/api/v1/system-log?level=warn"), ","); got != "line 1,line 2" {
		t.Errorf("from warn: %s", got)
	}
	if got := strings.Join(read("/api/v1/system-log?after=2&limit=1"), ","); got != "line 2" {
		t.Errorf("after 2: %s", got)
	}
	// Audited without content: the calls without after (agent, warn).
	var viewed int
	if err := f.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'system_log.viewed' AND result = 'ok' AND coalesce(payload, '') NOT LIKE '%line%'`).Scan(&viewed); err != nil || viewed != 2 {
		t.Errorf("audited reads = %d (%v), want 2", viewed, err)
	}
	var refused int
	if err := f.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'system_log.viewed' AND result = 'denied'`).Scan(&refused); err != nil || refused != 3 {
		t.Errorf("audited refusals = %d (%v), want 3", refused, err)
	}
}
