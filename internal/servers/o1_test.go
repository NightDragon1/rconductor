// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

func (f *fixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.st.DB.QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	return n
}

// removedEntries returns the payloads of the assignment.remove entries,
// keyed by the removed row, with actor and holder.
type removedEntry struct {
	actor, targetType, target, server string
	payload                           map[string]any
}

func (f *fixture) removedEntries() map[string]removedEntry {
	f.t.Helper()
	rows, err := f.st.DB.QueryContext(context.Background(), `SELECT coalesce(actor_id, ''), coalesce(target_type, ''), coalesce(target_id, ''),
		coalesce(server_id, ''), payload FROM audit_log WHERE action = 'assignment.remove' AND result = 'ok' AND kind = 'admin'`)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]removedEntry{}
	for rows.Next() {
		var e removedEntry
		var p string
		if err := rows.Scan(&e.actor, &e.targetType, &e.target, &e.server, &p); err != nil {
			f.t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(p), &e.payload); err != nil {
			f.t.Fatal(err)
		}
		id, _ := e.payload["id"].(string)
		if _, dup := out[id]; dup {
			f.t.Errorf("assignment %s audited twice", id)
		}
		out[id] = e
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out
}

// o1Assignments gives server a and server group g one assignment of every
// kind, and server b and server group h one each that must stay.
func (f *fixture) o1Assignments(a, b, g, h string) {
	f.t.Helper()
	f.ids["sup"] = storetest.CreateUser(f.t, f.st, storetest.User{Pseudonym: "sup", Role: "supporter"})
	f.exec(`INSERT INTO user_groups (id, name, kind, owner_id, created_at, updated_at) VALUES ('mg', 'mg', 'moderator', ?, 't', 't')`, f.ids["sa"])
	f.exec(`INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES ('mg', ?, ?, 't')`, f.ids["m"], f.ids["sa"])
	for _, r := range []struct{ id, server, group string }{{"sc-a", a, ""}, {"sc-b", b, ""}, {"sc-g", "", g}, {"sc-h", "", h}} {
		f.exec(`INSERT INTO admin_scopes (id, user_id, server_id, server_group_id, assigned_by, created_at) VALUES (?, ?, nullif(?, ''), nullif(?, ''), ?, 't')`,
			r.id, f.ids["sa"], r.server, r.group, f.ids["admin"])
	}
	for _, r := range []struct{ id, user, server, group string }{
		{"gr-a", "", a, ""}, {"gr-a-m", f.ids["m"], a, ""}, {"gr-b", "", b, ""}, {"gr-g", "", "", g}, {"gr-h", f.ids["m"], "", h},
	} {
		f.exec(`INSERT INTO grants (id, user_group_id, user_id, server_id, server_group_id, granted_by, created_at) VALUES (?, 'mg', nullif(?, ''), nullif(?, ''), nullif(?, ''), ?, 't')`,
			r.id, r.user, r.server, r.group, f.ids["sa"])
	}
	for _, r := range []struct{ id, server string }{{"sg-a", a}, {"sg-b", b}} {
		f.exec(`INSERT INTO supporter_grants (id, supporter_id, moderator_id, server_id, granted_by, created_at) VALUES (?, ?, ?, ?, ?, 't')`,
			r.id, f.ids["sup"], f.ids["m"], r.server, f.ids["m"])
	}
	f.c.Invalidate()
}

func (f *fixture) left(table string) []string {
	f.t.Helper()
	rows, err := f.st.DB.QueryContext(context.Background(), "SELECT id FROM "+table+" ORDER BY id")
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

func sameIDs(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s left = %v, want %v", what, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s left = %v, want %v", what, got, want)
			return
		}
	}
}

// TestO1DeleteServerEndsAssignments covers ROLE-003, ROLE-004 and
// ROLE-007: deleting a server removes the scopes, grants and supporter
// grants that name it in the same transaction, with one audit entry each;
// the assignments of other servers and groups stay. Parallel deletes of the
// same server audit every assignment once.
func TestO1DeleteServerEndsAssignments(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, b := f.create("a"), f.create("b")
	g, err := f.svc.CreateGroup(ctx, f.as("admin"), "g", []string{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	h, err := f.svc.CreateGroup(ctx, f.as("admin"), "h", []string{b.ID})
	if err != nil {
		t.Fatal(err)
	}
	f.o1Assignments(a.ID, b.ID, g.ID, h.ID)

	var wg sync.WaitGroup
	errs := make([]error, 4)
	admin := f.as("admin")
	for i := range errs {
		wg.Go(func() { errs[i] = f.svc.Delete(ctx, admin, a.ID) })
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, users.ErrNotFound):
			t.Errorf("parallel delete: %v", err)
		}
	}
	if ok == 0 {
		t.Fatal("no delete went through")
	}

	sameIDs(t, "scopes", f.left("admin_scopes"), "sc-b", "sc-g", "sc-h")
	sameIDs(t, "grants", f.left("grants"), "gr-b", "gr-g", "gr-h")
	sameIDs(t, "supporter grants", f.left("supporter_grants"), "sg-b")

	got := f.removedEntries()
	want := map[string]struct {
		section, targetType, target string
		entry                       map[string]any
	}{
		"sc-a":   {"admin_scopes", "user", f.ids["sa"], map[string]any{"server_id": a.ID}},
		"gr-a":   {"grants", "user_group", "mg", map[string]any{"group_id": "mg", "server_id": a.ID}},
		"gr-a-m": {"grants", "user", f.ids["m"], map[string]any{"group_id": "mg", "server_id": a.ID}},
		"sg-a":   {"supporter_grants", "user", f.ids["sup"], map[string]any{"moderator_id": f.ids["m"], "server_id": a.ID}},
	}
	if len(got) != len(want) {
		t.Errorf("audit entries for %d removed assignments, want %d: %v", len(got), len(want), got)
	}
	for id, w := range want {
		e, found := got[id]
		if !found {
			t.Errorf("no audit entry for %s", id)
			continue
		}
		if e.actor != f.ids["admin"] || e.targetType != w.targetType || e.target != w.target || e.server != a.ID ||
			e.payload["section"] != w.section || e.payload["cause"] != "server.delete" || !sameEntry(e.payload["entry"], w.entry) {
			t.Errorf("audit entry for %s = %+v", id, e)
		}
	}

	// What the user dialog reads holds no trace of the deleted server.
	r, _ := f.c.For(ctx, f.ids["sa"])
	if _, in := r.Server(a.ID); in {
		t.Error("the deleted server is still in the scope")
	}
}

// TestO1DeleteServerGroupEndsAssignments covers ROLE-003 and ROLE-004:
// deleting a server group removes the scopes and grants that name it, with
// one audit entry each; the servers and their own assignments stay.
func TestO1DeleteServerGroupEndsAssignments(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, b := f.create("a"), f.create("b")
	g, err := f.svc.CreateGroup(ctx, f.as("admin"), "g", []string{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	h, err := f.svc.CreateGroup(ctx, f.as("admin"), "h", []string{b.ID})
	if err != nil {
		t.Fatal(err)
	}
	f.o1Assignments(a.ID, b.ID, g.ID, h.ID)
	if err := f.svc.DeleteGroup(ctx, f.as("admin"), g.ID); err != nil {
		t.Fatal(err)
	}
	sameIDs(t, "scopes", f.left("admin_scopes"), "sc-a", "sc-b", "sc-h")
	sameIDs(t, "grants", f.left("grants"), "gr-a", "gr-a-m", "gr-b", "gr-h")
	sameIDs(t, "supporter grants", f.left("supporter_grants"), "sg-a", "sg-b")
	got := f.removedEntries()
	if len(got) != 2 {
		t.Errorf("audit entries = %v, want sc-g and gr-g", got)
	}
	if e := got["sc-g"]; e.targetType != "user" || e.target != f.ids["sa"] || e.payload["cause"] != "server_group.delete" ||
		!sameEntry(e.payload["entry"], map[string]any{"server_group_id": g.ID}) {
		t.Errorf("audit entry for sc-g = %+v", e)
	}
	if e := got["gr-g"]; e.targetType != "user_group" || e.target != "mg" || e.payload["section"] != "grants" {
		t.Errorf("audit entry for gr-g = %+v", e)
	}
	if err := f.svc.DeleteGroup(ctx, f.as("admin"), g.ID); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE action = 'assignment.remove'`); n != 2 {
		t.Errorf("%d removal entries after the second delete, want 2", n)
	}
}

func sameEntry(got any, want map[string]any) bool {
	m, ok := got.(map[string]any)
	if !ok || len(m) != len(want) {
		return false
	}
	for k, v := range want {
		if m[k] != v {
			return false
		}
	}
	return true
}
