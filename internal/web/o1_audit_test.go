// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/migrations"
)

// TestO1MigrationEntriesInAuditAPI covers AUD-001 and AUD-004 for the
// entries of migration 00018 (ROLE-003): GET /audit filters them by kind
// and server and pages through them like every other entry, and gives them
// without actor, role and session.
func TestO1MigrationEntriesInAuditAPI(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	admin := f.login(t, "Boss")
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := f.st.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	dead, group, live := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO servers (id, name, created_at, updated_at, deleted_at) VALUES (?, 'gone', 't', 't', 't'), (?, 'live', 't', 't', NULL)`, dead, live)
	exec(`INSERT INTO server_groups (id, name, created_at, updated_at, deleted_at) VALUES (?, 'gone', 't', 't', 't')`, group)
	scopeServer, scopeGroup := uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO admin_scopes (id, user_id, server_id, assigned_by, created_at) VALUES (?, ?, ?, ?, 't')`, scopeServer, f.userID, dead, f.adminID)
	exec(`INSERT INTO admin_scopes (id, user_id, server_group_id, assigned_by, created_at) VALUES (?, ?, ?, ?, 't')`, scopeGroup, f.userID, group, f.adminID)
	// Normal entries before and after: one on the live server, a later login.
	exec(`INSERT INTO audit_log (id, ts, kind, actor_id, actor_role, server_id, action, result) VALUES (?, '2000-01-01T00:00:00.000Z', 'admin', ?, 'rcon_admin', ?, 'server.update', 'ok')`,
		uuid.NewString(), f.adminID, live)

	// Run 00018 again on this database, as on an update from 1.0.
	p, err := goose.NewProvider(goose.DialectSQLite3, f.st.DB, migrations.SQLite(), goose.WithGoMigrations(migrations.SQLiteGo()...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 17); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	f.login(t, "Mod") // a later auth entry

	get := func(q url.Values) api.AuditPage {
		t.Helper()
		rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/audit?" + q.Encode(), cookies: []*http.Cookie{admin}})
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /audit?%s = %d %s", q.Encode(), rec.Code, rec.Body.String())
		}
		var page api.AuditPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	removed := func(items []api.AuditEntry) map[string]api.AuditEntry {
		out := map[string]api.AuditEntry{}
		for _, e := range items {
			if e.Action == "assignment.remove" {
				id, _ := e.Payload["id"].(string)
				out[id] = e
			}
		}
		return out
	}

	all := get(url.Values{"limit": {"200"}})
	got := removed(all.Items)
	if len(got) != 2 {
		t.Fatalf("removal entries = %v", got)
	}
	for id, e := range got {
		if e.ActorId != nil || e.ActorRole != nil || e.Session != nil || e.Kind != api.AuditEntryKindAdmin || e.Result != api.AuditEntryResultOk ||
			e.TargetType == nil || *e.TargetType != "user" || e.TargetId == nil || *e.TargetId != f.userID ||
			e.Payload["cause"] != "target_deleted" || e.Payload["section"] != "admin_scopes" {
			t.Errorf("entry for %s = %+v", id, e)
		}
	}
	if e := got[scopeServer]; e.ServerId == nil || e.ServerId.String() != dead {
		t.Errorf("entry of the server scope names server %v", e.ServerId)
	}
	if e := got[scopeGroup]; e.ServerId != nil {
		t.Errorf("entry of the group scope names server %v", e.ServerId)
	}

	if n := len(removed(get(url.Values{"kind": {"admin"}, "limit": {"200"}}).Items)); n != 2 {
		t.Errorf("kind admin gives %d removal entries", n)
	}
	if n := len(removed(get(url.Values{"kind": {"auth"}, "limit": {"200"}}).Items)); n != 0 {
		t.Errorf("kind auth gives %d removal entries", n)
	}
	byServer := get(url.Values{"server_id": {dead}, "limit": {"200"}})
	if len(byServer.Items) != 1 || removed(byServer.Items)[scopeServer].Id == (uuid.UUID{}) {
		t.Errorf("server filter gives %+v", byServer.Items)
	}
	if n := len(get(url.Values{"server_id": {live}, "limit": {"200"}}).Items); n != 1 {
		t.Errorf("filter on the live server gives %d entries", n)
	}

	// Pages of one entry: every entry once, in the order of the full list.
	var paged []string
	cursor := ""
	for range len(all.Items) + 1 {
		q := url.Values{"limit": {"1"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		page := get(q)
		for _, e := range page.Items {
			paged = append(paged, e.Id.String())
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(paged) != len(all.Items) {
		t.Fatalf("paged %d entries, the full list has %d", len(paged), len(all.Items))
	}
	for i, e := range all.Items {
		if paged[i] != e.Id.String() {
			t.Fatalf("page %d gives %s, the full list %s", i, paged[i], e.Id)
		}
	}
}
