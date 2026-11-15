// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// TestAboutFieldsByRole: GET /about (UI-024, D-080) gives name, version,
// license and repository without a session, adds commit and build date
// with one, and the agents with their version only for the RCON admin. An
// agent version that is missing (agents 1.0, 1.1) or longer than 64
// characters is unknown; control characters are dropped (SEC-006).
func TestAboutFieldsByRole(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	admin, mod := f.login(t, "Boss"), f.login(t, "Mod")
	now := time.Now().UTC().Format(time.RFC3339)
	for i, v := range []string{"1.2.0", "", strings.Repeat("9", 65), "1.2\u202e.1"} {
		if err := f.st.Queries.InsertAgent(context.Background(), sqlitedb.InsertAgentParams{
			ID: []string{"01a1deac-0000-7000-8000-000000000001", "01a1deac-0000-7000-8000-000000000002",
				"01a1deac-0000-7000-8000-000000000003", "01a1deac-0000-7000-8000-000000000004"}[i],
			Name: "agent-" + string(rune('a'+i)), PublicKey: []byte{byte(i)}, Fingerprint: "fp" + string(rune('a'+i)),
			Status: "active", Version: v, Capabilities: "[]", CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	about := func(c *http.Cookie) map[string]any {
		t.Helper()
		var cookies []*http.Cookie
		if c != nil {
			cookies = []*http.Cookie{c}
		}
		rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/about", cookies: cookies})
		if rec.Code != http.StatusOK {
			t.Fatalf("about = %d %s", rec.Code, rec.Body.String())
		}
		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	keys := func(m map[string]any) string {
		var k []string
		for _, n := range []string{"name", "version", "license", "repo_url", "commit", "build_date", "agents"} {
			if _, ok := m[n]; ok {
				k = append(k, n)
			}
		}
		return strings.Join(k, ",")
	}
	anon := about(nil)
	if got := keys(anon); got != "name,version,license,repo_url" || len(anon) != 4 {
		t.Errorf("without session: %v", anon)
	}
	if anon["name"] != "RCONductor" || anon["license"] != "AGPL-3.0-or-later" || anon["repo_url"] != "https://github.com/NightDragon1/rconductor" {
		t.Errorf("without session: %v", anon)
	}
	if got := about(mod); keys(got) != "name,version,license,repo_url,commit,build_date" || len(got) != 6 {
		t.Errorf("moderator: %v", got)
	}
	got := about(admin)
	if keys(got) != "name,version,license,repo_url,commit,build_date,agents" || got["commit"] != "unknown" {
		t.Fatalf("RCON admin: %v", got)
	}
	versions := map[string]string{}
	for _, a := range got["agents"].([]any) {
		m := a.(map[string]any)
		versions[m["name"].(string)] = m["version"].(string)
	}
	want := map[string]string{"agent-a": "1.2.0", "agent-b": "unknown", "agent-c": "unknown", "agent-d": "1.2.1"}
	for n, v := range want {
		if versions[n] != v {
			t.Errorf("agent %s version = %q, want %q", n, versions[n], v)
		}
	}
}
