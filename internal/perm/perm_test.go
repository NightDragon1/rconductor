// SPDX-License-Identifier: AGPL-3.0-or-later

package perm

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// world is a small installation:
//
//	server groups: mc = {s1, s2}, ark = {s3}; s4 alone; s5 deleted
//	server admins: sa (scope mc + s4), sa2 (scope ark)
//	moderator groups: mg1 (owner sa, list l1), mg2 (owner sa, list l2),
//	                  mg3 (owner sa2, list l3)
//	moderator m is in mg1, mg2, mg3; moderator m2 only in mg1
type world struct {
	t   *testing.T
	st  *store.Store
	c   *Checker
	log *bytes.Buffer
	ids map[string]string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	st := storetest.New(t)
	if err := EnsureDefaults(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	w := &world{t: t, st: st, log: logs, ids: map[string]string{},
		c: NewChecker(st, logging.New(logs, "panel", config.Log{Format: "json"}).Module("perm"))}

	w.ids["admin"] = storetest.CreateUser(t, st, storetest.User{Pseudonym: "admin", Role: "rcon_admin"})
	for _, p := range []string{"sa", "sa2"} {
		w.ids[p] = storetest.CreateUser(t, st, storetest.User{Pseudonym: p, Role: "server_admin"})
	}
	for _, p := range []string{"m", "m2"} {
		w.ids[p] = storetest.CreateUser(t, st, storetest.User{Pseudonym: p, Role: "moderator"})
	}
	w.ids["sup"] = storetest.CreateUser(t, st, storetest.User{Pseudonym: "sup", Role: "supporter"})

	for _, s := range []string{"s1", "s2", "s3", "s4", "s5"} {
		w.exec(`INSERT INTO servers (id, name, created_at, updated_at) VALUES (?, ?, 't', 't')`, s, s)
	}
	w.exec(`UPDATE servers SET deleted_at = 't' WHERE id = 's5'`)
	w.exec(`INSERT INTO server_groups (id, name, created_at, updated_at) VALUES ('mc', 'mc', 't', 't'), ('ark', 'ark', 't', 't')`)
	w.exec(`INSERT INTO server_group_members VALUES ('mc', 's1'), ('mc', 's2'), ('ark', 's3'), ('mc', 's5')`)

	w.exec(`INSERT INTO admin_scopes (id, user_id, server_group_id, assigned_by, created_at) VALUES ('as1', ?, 'mc', ?, 't')`, w.ids["sa"], w.ids["admin"])
	w.exec(`INSERT INTO admin_scopes (id, user_id, server_id, assigned_by, created_at) VALUES ('as2', ?, 's4', ?, 't')`, w.ids["sa"], w.ids["admin"])
	w.exec(`INSERT INTO admin_scopes (id, user_id, server_group_id, assigned_by, created_at) VALUES ('as3', ?, 'ark', ?, 't')`, w.ids["sa2"], w.ids["admin"])

	lists := map[string][][2]any{
		"l1": {{"kick", 1}, {"whitelist add", 0}},
		"l2": {{"say", 1}, {"time set", 0}},
		"l3": {{"list", 1}},
	}
	owner := map[string]string{"l1": "sa", "l2": "sa", "l3": "sa2"}
	for l, entries := range lists {
		w.exec(`INSERT INTO command_lists (id, name, owner_id, created_at, updated_at) VALUES (?, ?, ?, 't', 't')`, l, l, w.ids[owner[l]])
		for i, e := range entries {
			w.exec(`INSERT INTO command_list_entries (id, command_list_id, path, passable) VALUES (?, ?, ?, ?)`, l+string(rune('a'+i)), l, e[0], e[1])
		}
	}
	for g, l := range map[string]string{"mg1": "l1", "mg2": "l2", "mg3": "l3"} {
		w.exec(`INSERT INTO user_groups (id, name, kind, owner_id, command_list_id, created_at, updated_at) VALUES (?, ?, 'moderator', ?, ?, 't', 't')`,
			g, g, w.ids[owner[l]], l)
	}
	for _, m := range [][2]string{{"mg1", "m"}, {"mg2", "m"}, {"mg3", "m"}, {"mg1", "m2"}} {
		w.exec(`INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES (?, ?, ?, 't')`, m[0], w.ids[m[1]], w.ids["sa"])
	}

	// Grants: mg1 to s1 for all members; mg2 to group mc for m only; mg1
	// to s3, which is outside the owner's scope; mg3 to s3; mg2 to s4 for
	// m2, who is not a member of mg2.
	w.exec(`INSERT INTO grants (id, user_group_id, server_id, granted_by, created_at) VALUES ('g1', 'mg1', 's1', ?, 't')`, w.ids["sa"])
	w.exec(`INSERT INTO grants (id, user_group_id, user_id, server_group_id, granted_by, created_at) VALUES ('g2', 'mg2', ?, 'mc', ?, 't')`, w.ids["m"], w.ids["sa"])
	w.exec(`INSERT INTO grants (id, user_group_id, server_id, granted_by, created_at) VALUES ('g3', 'mg1', 's3', ?, 't')`, w.ids["sa"])
	w.exec(`INSERT INTO grants (id, user_group_id, server_group_id, granted_by, created_at) VALUES ('g4', 'mg3', 'ark', ?, 't')`, w.ids["sa2"])
	w.exec(`INSERT INTO grants (id, user_group_id, user_id, server_id, granted_by, created_at) VALUES ('g5', 'mg2', ?, 's4', ?, 't')`, w.ids["m2"], w.ids["sa"])

	for _, s := range []string{"s1", "s2", "s4"} {
		w.exec(`INSERT INTO supporter_grants (id, supporter_id, moderator_id, server_id, granted_by, created_at) VALUES (?, ?, ?, ?, ?, 't')`,
			"sg-"+s, w.ids["sup"], w.ids["m"], s, w.ids["m"])
	}
	return w
}

func (w *world) exec(q string, args ...any) {
	w.t.Helper()
	if _, err := w.st.DB.ExecContext(context.Background(), q, args...); err != nil {
		w.t.Fatalf("%s: %v", q, err)
	}
}

// change applies a modification and invalidates the cache, as every
// administration endpoint does after commit.
func (w *world) change(q string, args ...any) {
	w.t.Helper()
	w.exec(q, args...)
	w.c.Invalidate()
}

func (w *world) rights(user string) *Rights {
	w.t.Helper()
	r, err := w.c.For(context.Background(), w.ids[user])
	if err != nil {
		w.t.Fatal(err)
	}
	return r
}

func (w *world) commands(user, server string) []string {
	w.t.Helper()
	s, ok := w.rights(user).Server(server)
	if !ok {
		return nil
	}
	return s.Commands
}

func TestFourRoles(t *testing.T) {
	if len(Roles) != 4 {
		t.Fatalf("roles = %v", Roles)
	}
	for _, r := range Roles {
		if !r.Valid() {
			t.Errorf("%s not valid", r)
		}
	}
	for _, r := range []Role{"", "player", "RCON_ADMIN"} {
		if r.Valid() {
			t.Errorf("%q valid", r)
		}
	}
	if !RCONAdmin.Above(ServerAdmin) || !ServerAdmin.Above(Moderator) || !Moderator.Above(Supporter) {
		t.Error("order of roles")
	}
	if Moderator.Above(Moderator) || Supporter.Above(Moderator) || Role("x").Above(Supporter) {
		t.Error("Above must be strict and reject invalid roles")
	}
}

func TestAdminScopes(t *testing.T) {
	w := newWorld(t)
	if got := w.rights("admin").ServerIDs(); !slices.Equal(got, []string{"s1", "s2", "s3", "s4"}) {
		t.Errorf("RCON admin servers = %v, want all but the deleted one", got)
	}
	s, _ := w.rights("admin").Server("s3")
	if !s.AllCommands || !s.Agent {
		t.Errorf("RCON admin rights = %+v", s)
	}
	if got := w.rights("sa").ServerIDs(); !slices.Equal(got, []string{"s1", "s2", "s4"}) {
		t.Errorf("server admin scope = %v, want group mc plus s4", got)
	}
	if _, ok := w.rights("sa").Server("s3"); ok {
		t.Error("server admin reaches a server outside his scope")
	}
}

// TestModeratorListPerServer covers ROLE-010: on each server only the lists
// of the groups through which the moderator has that server apply, and
// several ways to the same server are united.
func TestModeratorListPerServer(t *testing.T) {
	w := newWorld(t)
	want := map[string][]string{
		"s1": {"kick", "say", "time set", "whitelist add"}, // mg1 and mg2
		"s2": {"say", "time set"},                          // mg2 only
		"s3": {"list"},                                     // mg3; mg1's grant is outside sa's scope
	}
	r := w.rights("m")
	if got := r.ServerIDs(); !slices.Equal(got, []string{"s1", "s2", "s3"}) {
		t.Fatalf("moderator servers = %v", got)
	}
	for s, cmds := range want {
		if got := w.commands("m", s); !slices.Equal(got, cmds) {
			t.Errorf("%s: commands = %v, want %v", s, got, cmds)
		}
	}
	if s, _ := r.Server("s1"); s.AllCommands || s.Agent || !slices.Equal(s.Passable, []string{"kick", "say"}) {
		t.Errorf("moderator rights on s1 = %+v", s)
	}
	// m2 is only in mg1; a direct grant of mg2 does not apply to a non-member.
	if got := w.rights("m2").ServerIDs(); !slices.Equal(got, []string{"s1"}) {
		t.Errorf("m2 servers = %v", got)
	}
	if got := w.commands("m2", "s1"); !slices.Equal(got, []string{"kick", "whitelist add"}) {
		t.Errorf("m2 commands = %v", got)
	}
}

// TestSupporter covers ROLE-006 and ROLE-007: nothing without the global
// switch and the moderator's release; then only passable commands of the
// moderator's list per assigned server.
func TestSupporter(t *testing.T) {
	w := newWorld(t)
	if got := w.rights("sup").ServerIDs(); len(got) != 0 {
		t.Errorf("supporter rights with the switch off: %v", got)
	}
	w.change(`UPDATE settings SET value = 'true' WHERE key = ?`, SettingSupportersEnabled)
	if got := w.rights("sup").ServerIDs(); len(got) != 0 {
		t.Errorf("supporter rights without the moderator's release: %v", got)
	}
	w.change(`UPDATE users SET supporters_allowed = 1 WHERE id = ?`, w.ids["m"])
	if got := w.rights("sup").ServerIDs(); !slices.Equal(got, []string{"s1", "s2"}) {
		t.Errorf("supporter servers = %v, want s1, s2 (s4 is outside m's scope)", got)
	}
	if got := w.commands("sup", "s1"); !slices.Equal(got, []string{"kick", "say"}) {
		t.Errorf("supporter commands on s1 = %v", got)
	}
	if got := w.commands("sup", "s2"); !slices.Equal(got, []string{"say"}) {
		t.Errorf("supporter commands on s2 = %v", got)
	}
}

// TestScopeShrinksDownwards covers Design 3.2: a smaller scope above is a
// smaller scope for everyone below, immediately.
func TestScopeShrinksDownwards(t *testing.T) {
	w := newWorld(t)
	w.change(`UPDATE settings SET value = 'true' WHERE key = ?`, SettingSupportersEnabled)
	w.change(`UPDATE users SET supporters_allowed = 1 WHERE id = ?`, w.ids["m"])
	if len(w.rights("sup").ServerIDs()) != 2 {
		t.Fatal("setup")
	}
	w.change(`DELETE FROM admin_scopes WHERE id = 'as1'`) // sa loses group mc
	if got := w.rights("m").ServerIDs(); !slices.Equal(got, []string{"s3"}) {
		t.Errorf("moderator after the admin lost mc: %v", got)
	}
	if got := w.rights("sup").ServerIDs(); len(got) != 0 {
		t.Errorf("supporter after the admin lost mc: %v", got)
	}

	w.change(`UPDATE users SET disabled_at = 't' WHERE id = ?`, w.ids["sa2"])
	if got := w.rights("m").ServerIDs(); len(got) != 0 {
		t.Errorf("moderator keeps servers of a disabled group owner: %v", got)
	}
}

func TestInactiveUsersHaveNoRights(t *testing.T) {
	w := newWorld(t)
	w.change(`UPDATE users SET disabled_at = 't' WHERE id = ?`, w.ids["sa"])
	w.change(`UPDATE users SET deleted_at = 't' WHERE id = ?`, w.ids["m2"])
	for _, u := range []string{"sa", "m2"} {
		if got := w.rights(u).ServerIDs(); len(got) != 0 {
			t.Errorf("%s has rights: %v", u, got)
		}
	}
	r, err := w.c.For(context.Background(), "no-such-user")
	if err != nil || len(r.ServerIDs()) != 0 || r.Role.Valid() {
		t.Errorf("unknown user = %+v, %v", r, err)
	}
}

func TestDeletedGroupsAndListsGrantNothing(t *testing.T) {
	w := newWorld(t)
	w.change(`UPDATE user_groups SET deleted_at = 't' WHERE id = 'mg2'`)
	if got := w.commands("m", "s1"); !slices.Equal(got, []string{"kick", "whitelist add"}) {
		t.Errorf("after deleting mg2: %v", got)
	}
	w.change(`UPDATE command_lists SET deleted_at = 't' WHERE id = 'l1'`)
	if got := w.commands("m", "s1"); len(got) != 0 {
		t.Errorf("deleted list still grants: %v", got)
	}
}

func TestCacheAndInvalidate(t *testing.T) {
	w := newWorld(t)
	w.rights("m")
	w.rights("m")
	if n := logtest.WaitCount(t, w.log, `"event":"perm.recomputed"`, 1); n != 1 {
		t.Errorf("perm.recomputed = %d after two lookups, want 1", n)
	}
	// A change without Invalidate is not seen (cached) ...
	w.exec(`DELETE FROM grants WHERE id = 'g1'`)
	if len(w.commands("m", "s1")) != 4 {
		t.Error("cache not used")
	}
	// ... and seen right after Invalidate.
	w.c.Invalidate()
	if got := w.commands("m", "s1"); !slices.Equal(got, []string{"say", "time set"}) {
		t.Errorf("after invalidate: %v", got)
	}
}

func TestManaged(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	// Supporters invited: one by m, one by sa; one invited by sa2's moderator.
	w.ids["sup2"] = storetest.CreateUser(t, w.st, storetest.User{Pseudonym: "sup2", Role: "supporter", CreatedBy: w.ids["m"]})
	w.ids["sup3"] = storetest.CreateUser(t, w.st, storetest.User{Pseudonym: "sup3", Role: "supporter", CreatedBy: w.ids["sa"]})
	w.ids["m3"] = storetest.CreateUser(t, w.st, storetest.User{Pseudonym: "m3", Role: "moderator", CreatedBy: w.ids["sa2"]})
	w.ids["sa3"] = storetest.CreateUser(t, w.st, storetest.User{Pseudonym: "sa3", Role: "server_admin", CreatedBy: w.ids["admin"]})

	managed := func(actor string) []string {
		t.Helper()
		m, err := w.c.Managed(ctx, Actor{ID: w.ids[actor], Role: w.rights(actor).Role})
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for name, id := range w.ids {
			if m[id] {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		return names
	}
	cases := map[string][]string{
		"admin": {"m", "m2", "m3", "sa", "sa2", "sa3", "sup", "sup2", "sup3"},
		"sa":    {"m", "m2", "sup", "sup2", "sup3"}, // m, m2 via his groups, their supporters, sup3 invited
		"sa2":   {"m", "m3", "sup", "sup2"},         // m via mg3 with his supporters, m3 invited
		"sup":   nil,
	}
	for actor, want := range cases {
		if got := managed(actor); !slices.Equal(got, want) {
			t.Errorf("%s manages %v, want %v", actor, got, want)
		}
	}
	// A moderator manages supporters only with both switches on (ROLE-006).
	if got := managed("m"); got != nil {
		t.Errorf("m manages %v with supporters disabled", got)
	}
	w.change(`UPDATE settings SET value = 'true' WHERE key = ?`, SettingSupportersEnabled)
	if got := managed("m"); got != nil {
		t.Errorf("m manages %v without his own release", got)
	}
	w.change(`UPDATE users SET supporters_allowed = 1 WHERE id = ?`, w.ids["m"])
	if got := managed("m"); !slices.Equal(got, []string{"sup", "sup2"}) {
		t.Errorf("m manages %v", got)
	}
	// Deleted users are never managed.
	w.change(`UPDATE users SET deleted_at = 't' WHERE id = ?`, w.ids["sup2"])
	if got := managed("m"); !slices.Equal(got, []string{"sup"}) {
		t.Errorf("after deleting sup2: %v", got)
	}
}
