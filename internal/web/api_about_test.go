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

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// aboutKeys names the fields of an answer of GET /about in a fixed order.
func aboutKeys(m map[string]any) string {
	var k []string
	for _, n := range []string{"name", "version", "license", "repo_url", "commit", "build_date", "agents"} {
		if _, ok := m[n]; ok {
			k = append(k, n)
		}
	}
	if len(k) != len(m) {
		return "unexpected fields"
	}
	return strings.Join(k, ",")
}

// TestAboutFieldsByRole: GET /about (UI-024, D-080) gives name, version,
// license and repository without a session, adds commit and build date
// with one, and the paired agents with their version only for the RCON
// admin; a session that must change its password gets the answer without
// a session. Pending and revoked agents are left out. An agent version that
// is missing (agents 1.0, 1.1) or longer than 64 characters is unknown;
// control characters are dropped (SEC-006).
func TestAboutFieldsByRole(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	ctx := context.Background()
	var hash string
	if err := f.st.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, f.userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	for _, u := range []storetest.User{
		{Pseudonym: "SA", Role: "server_admin"}, {Pseudonym: "Sup", Role: "supporter"}, {Pseudonym: "New", Role: "rcon_admin"},
	} {
		u.PasswordHash, u.CreatedBy = hash, f.adminID
		storetest.CreateUser(t, f.st, u)
	}
	if _, err := f.st.DB.Exec(`UPDATE users SET must_change_password = 1 WHERE pseudonym = 'New'`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i, a := range []struct{ version, status string }{
		{"1.2.0", "active"}, {"", "active"}, {strings.Repeat("9", 65), "active"}, {"1.2\u202e.1", "active"},
		{"1.2.0", "pending"}, {"1.2.0", "active"},
	} {
		if err := f.st.Queries.InsertAgent(ctx, sqlitedb.InsertAgentParams{
			ID:   "01a1deac-0000-7000-8000-00000000000" + string(rune('1'+i)),
			Name: "agent-" + string(rune('a'+i)), PublicKey: []byte{byte(i)}, Fingerprint: "fp" + string(rune('a'+i)),
			Status: a.status, Version: a.version, Capabilities: "[]", CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.st.DB.Exec(`UPDATE agents SET status = 'revoked', revoked_at = ? WHERE name = 'agent-f'`, now); err != nil {
		t.Fatal(err)
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
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", cc)
		}
		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	const anon, signed, admin = "name,version,license,repo_url", "name,version,license,repo_url,commit,build_date",
		"name,version,license,repo_url,commit,build_date,agents"
	for who, want := range map[string]string{
		"": anon, "Mod": signed, "Sup": signed, "SA": signed, "New": anon, "Boss": admin,
	} {
		var c *http.Cookie
		if who != "" {
			c = f.login(t, who)
		}
		if got := aboutKeys(about(c)); got != want {
			t.Errorf("%q: fields %s, want %s", who, got, want)
		}
	}
	got := about(f.login(t, "Boss"))
	if got["name"] != "RCONductor" || got["license"] != "AGPL-3.0-or-later" ||
		got["repo_url"] != "https://github.com/NightDragon1/rconductor" || got["commit"] != "unknown" {
		t.Errorf("RCON admin: %v", got)
	}
	versions := map[string]string{}
	for _, a := range got["agents"].([]any) {
		m := a.(map[string]any)
		versions[m["name"].(string)] = m["version"].(string)
	}
	want := map[string]string{"agent-a": "1.2.0", "agent-b": "unknown", "agent-c": "unknown", "agent-d": "1.2.1"}
	if len(versions) != len(want) {
		t.Errorf("agents %v, want the four active ones", versions)
	}
	for n, v := range want {
		if versions[n] != v {
			t.Errorf("agent %s version = %q, want %q", n, versions[n], v)
		}
	}
}

// TestAboutWhileSecondFactorSetup: an RCON admin session that must set up
// the second factor first gets the answer without a session (D-080).
func TestAboutWhileSecondFactorSetup(t *testing.T) {
	s := &apiServer{}
	ctx := context.WithValue(context.Background(), principalKey{}, auth.Principal{Role: "rcon_admin", TOTPSetupRequired: true})
	res, err := s.GetAbout(ctx, api.GetAboutRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	a := res.(api.GetAbout200JSONResponse)
	if a.Commit != nil || a.BuildDate != nil || a.Agents != nil {
		t.Errorf("restricted session got %+v", a)
	}
}
