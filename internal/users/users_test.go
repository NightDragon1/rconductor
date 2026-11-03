// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

type fixture struct {
	t       *testing.T
	st      *store.Store
	svc     *Service
	checker *perm.Checker
	logs    *bytes.Buffer
	ids     map[string]string
	revoked []string
}

// newFixture: admin (rcon_admin), sa and sa2 (server admins), m (moderator
// in sa's group mg), m2 (moderator invited by sa2), sup (supporter
// invited by m).
func newFixture(t *testing.T) *fixture {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	if err := perm.EnsureDefaults(ctx, st); err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("users")
	f := &fixture{t: t, st: st, logs: logs, ids: map[string]string{}, checker: perm.NewChecker(st, log)}
	f.svc = NewService(st, f.checker, log, func(_ context.Context, id, _, _ string) error {
		f.revoked = append(f.revoked, id)
		return nil
	})
	add := func(name, role, by string) {
		f.ids[name] = storetest.CreateUser(t, st, storetest.User{Pseudonym: name, Role: role, CreatedBy: f.ids[by]})
	}
	add("admin", "rcon_admin", "")
	add("sa", "server_admin", "admin")
	add("sa2", "server_admin", "admin")
	add("m", "moderator", "")
	add("m2", "moderator", "sa2")
	add("sup", "supporter", "m")
	f.exec(`INSERT INTO servers (id, name, created_at, updated_at) VALUES ('s1', 's1', 't', 't')`)
	f.exec(`INSERT INTO admin_scopes (id, user_id, server_id, assigned_by, created_at) VALUES ('as1', ?, 's1', ?, 't')`, f.ids["sa"], f.ids["admin"])
	f.exec(`INSERT INTO command_lists (id, name, owner_id, created_at, updated_at) VALUES ('l1', 'l1', ?, 't', 't')`, f.ids["sa"])
	f.exec(`INSERT INTO user_groups (id, name, kind, owner_id, command_list_id, created_at, updated_at) VALUES ('mg', 'mg', 'moderator', ?, 'l1', 't', 't')`, f.ids["sa"])
	f.exec(`INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES ('mg', ?, ?, 't')`, f.ids["m"], f.ids["sa"])
	f.exec(`INSERT INTO grants (id, user_group_id, server_id, granted_by, created_at) VALUES ('g1', 'mg', 's1', ?, 't')`, f.ids["sa"])
	return f
}

func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.st.DB.ExecContext(context.Background(), q, args...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

func (f *fixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.st.DB.QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *fixture) as(name string) Request {
	f.t.Helper()
	r, err := f.checker.For(context.Background(), f.ids[name])
	if err != nil {
		f.t.Fatal(err)
	}
	return Request{Actor: perm.Actor{ID: f.ids[name], Role: r.Role}, Pseudonym: name, RequestID: "req-" + name}
}

func names(us []User) []string {
	out := []string{}
	for _, u := range us {
		out = append(out, u.Pseudonym)
	}
	return out
}

func TestListAndFilter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	got, err := f.svc.List(ctx, f.as("sa"), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names(got), []string{"m", "sup"}) {
		t.Errorf("sa lists %v", names(got))
	}
	got, _ = f.svc.List(ctx, f.as("admin"), Filter{Role: perm.ServerAdmin})
	if !slices.Equal(names(got), []string{"sa", "sa2"}) {
		t.Errorf("admin lists server admins %v", names(got))
	}
	got, _ = f.svc.List(ctx, f.as("admin"), Filter{Query: "SA2"})
	if !slices.Equal(names(got), []string{"sa2"}) {
		t.Errorf("search: %v", names(got))
	}
	if got, _ := f.svc.List(ctx, f.as("sup"), Filter{}); len(got) != 0 {
		t.Errorf("supporter lists %v", names(got))
	}
}

// TestRightsFlowDownwards covers ROLE-008: no role can create, change or
// demote an equal or higher role; every attempt is refused, logged and
// audited.
func TestRightsFlowDownwards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sa := perm.ServerAdmin
	mod := perm.Moderator
	admin := perm.RCONAdmin

	attempts := []struct {
		name, actor, target string
		update              Update
		want                error
	}{
		{"server admin changes the RCON admin", "sa", "admin", Update{Role: &mod}, ErrNotFound},
		{"server admin demotes another server admin", "sa", "sa2", Update{Role: &mod}, ErrNotFound},
		{"server admin promotes his moderator to his level", "sa", "m", Update{Role: &sa}, ErrForbidden},
		{"RCON admin promotes to RCON admin", "admin", "sa", Update{Role: &admin}, ErrForbidden},
		{"server admin touches a moderator outside his scope", "sa", "m2", Update{Role: &mod}, ErrNotFound},
		// Without a supporter release a moderator, and a supporter always,
		// has no user administration at all (Design 2.2, 3.6).
		{"moderator changes himself", "m", "m", Update{Role: &mod}, ErrForbidden},
		{"supporter locks his moderator", "sup", "m", Update{Disabled: new(true)}, ErrForbidden},
	}
	for _, a := range attempts {
		if _, err := f.svc.Update(ctx, f.as(a.actor), f.ids[a.target], a.update); !errors.Is(err, a.want) {
			t.Errorf("%s: %v, want %v", a.name, err, a.want)
		}
	}
	if err := f.svc.Delete(ctx, f.as("sa"), f.ids["sa2"]); !errors.Is(err, ErrNotFound) {
		t.Errorf("server admin deletes a server admin: %v", err)
	}
	n := len(attempts) + 1
	if got := strings.Count(f.logs.String(), `"event":"perm.denied"`); got != n {
		t.Errorf("perm.denied = %d, want %d", got, n)
	}
	if got := f.count(`SELECT count(*) FROM audit_log WHERE kind = 'admin' AND result = 'denied'`); got != n {
		t.Errorf("denied audit entries = %d, want %d", got, n)
	}
	if f.count(`SELECT count(*) FROM users WHERE role = 'rcon_admin'`) != 1 || len(f.revoked) != 0 {
		t.Error("a refused attempt changed something")
	}
}

func TestRoleChangeDropsAssignments(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sup := perm.Supporter
	u, err := f.svc.Update(ctx, f.as("sa"), f.ids["m"], Update{Role: &sup})
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != perm.Supporter {
		t.Errorf("role = %s", u.Role)
	}
	if f.count(`SELECT count(*) FROM user_group_members WHERE user_id = ?`, f.ids["m"]) != 0 {
		t.Error("group membership of the old role kept")
	}
	if !slices.Equal(f.revoked, []string{f.ids["m"]}) {
		t.Errorf("sessions ended for %v", f.revoked)
	}
	if f.count(`SELECT count(*) FROM audit_log WHERE action = 'user.update' AND result = 'ok' AND payload LIKE '%supporter%'`) != 1 {
		t.Error("role change not audited")
	}
}

func TestDisableAndDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	u, err := f.svc.Update(ctx, f.as("admin"), f.ids["sa"], Update{Disabled: new(true)})
	if err != nil || !u.Disabled {
		t.Fatalf("disable: %+v %v", u, err)
	}
	if !slices.Contains(f.revoked, f.ids["sa"]) {
		t.Error("sessions of the locked user not ended")
	}
	if r, _ := f.checker.For(ctx, f.ids["sa"]); len(r.ServerIDs()) != 0 {
		t.Error("locked user keeps rights")
	}
	if u, err = f.svc.Update(ctx, f.as("admin"), f.ids["sa"], Update{Disabled: new(false)}); err != nil || u.Disabled {
		t.Errorf("unlock: %+v %v", u, err)
	}

	if err := f.svc.Delete(ctx, f.as("sa"), f.ids["m"]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Get(ctx, f.as("sa"), f.ids["m"]); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted user still visible: %v", err)
	}
	if f.count(`SELECT count(*) FROM users WHERE id = ? AND deleted_at IS NOT NULL`, f.ids["m"]) != 1 {
		t.Error("user not soft-deleted")
	}
	// The pseudonym is free again.
	storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "m"})
}

func storeUser(t *testing.T, f *fixture, name, role, by string) string {
	t.Helper()
	return storetest.CreateUser(t, f.st, storetest.User{Pseudonym: name, Role: role, CreatedBy: f.ids[by]})
}

// version reads the stored version of an object for a versioned write
// (revision 2.4 candidate 24); table "settings" means the settings.
func (f *fixture) version(table, id string) *int64 {
	f.t.Helper()
	var v int64
	q := "SELECT version FROM " + table + " WHERE id = ?"
	args := []any{id}
	if table == "settings" {
		q, args = "SELECT version FROM settings_version WHERE id = 1", nil
	}
	if err := f.st.DB.QueryRowContext(context.Background(), q, args...).Scan(&v); err != nil {
		f.t.Fatal(err)
	}
	return &v
}
