// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/perm"
)

// withMore adds s2 (outside sa's scope), server group mc = {s1}, a second
// moderator group mg0 of sa without grants, and mg3 owned by sa2 with m as
// member.
func withMore(t *testing.T) *fixture {
	f := newFixture(t)
	f.exec(`INSERT INTO servers (id, name, created_at, updated_at) VALUES ('s2', 's2', 't', 't')`)
	f.exec(`INSERT INTO server_groups (id, name, created_at, updated_at) VALUES ('mc', 'mc', 't', 't'), ('all', 'all', 't', 't')`)
	f.exec(`INSERT INTO server_group_members VALUES ('mc', 's1'), ('all', 's1'), ('all', 's2')`)
	f.exec(`INSERT INTO user_groups (id, name, kind, owner_id, command_list_id, created_at, updated_at) VALUES ('mg0', 'mg0', 'moderator', ?, 'l1', 't', 't')`, f.ids["sa"])
	f.exec(`INSERT INTO user_groups (id, name, kind, owner_id, created_at, updated_at) VALUES ('mg3', 'mg3', 'moderator', ?, 't', 't')`, f.ids["sa2"])
	f.exec(`INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES ('mg3', ?, ?, 't')`, f.ids["m"], f.ids["sa2"])
	return f
}

func (f *fixture) deniedCount(t *testing.T, want int) (logged, audited int) {
	return logtest.WaitCount(t, f.logs, `"event":"perm.denied"`, want),
		f.count(`SELECT count(*) FROM audit_log WHERE action = 'assignments.update' AND result = 'denied'`)
}

// TestAdminScopes covers ROLE-003: only the RCON admin assigns server
// groups and servers to server admins.
func TestAdminScopes(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	scopes := []Target{{ServerGroupID: "all"}, {ServerID: "s2"}}
	a, err := f.svc.UpdateAssignments(ctx, f.as("admin"), f.ids["sa"], AssignmentsUpdate{AdminScopes: &scopes})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.AdminScopes) != 2 {
		t.Errorf("scopes = %+v", a.AdminScopes)
	}
	r, _ := f.checker.For(ctx, f.ids["sa"])
	if !slices.Equal(r.ServerIDs(), []string{"s1", "s2"}) {
		t.Errorf("server admin scope = %v", r.ServerIDs())
	}
	if len(f.revoked) != 0 {
		t.Errorf("a scope change ended sessions of %v (D-044)", f.revoked)
	}

	// Sections without a change do not count; a removed scope is gone at once.
	f.revoked = nil
	none := []string{}
	both := AssignmentsUpdate{AdminScopes: &[]Target{{ServerID: "s2"}}, Groups: &none}
	if _, err := f.svc.UpdateAssignments(ctx, f.as("admin"), f.ids["sa"], both); err != nil {
		t.Fatal(err)
	}
	if len(f.revoked) != 0 {
		t.Errorf("groups without a change ended sessions of %v", f.revoked)
	}
	if r, _ := f.checker.For(ctx, f.ids["sa"]); !slices.Equal(r.ServerIDs(), []string{"s2"}) {
		t.Errorf("removed scope still counts: %v", r.ServerIDs())
	}

	// A server admin cannot change his own scope: he does not manage himself.
	if _, err := f.svc.UpdateAssignments(ctx, f.as("sa"), f.ids["sa"], AssignmentsUpdate{AdminScopes: &scopes}); !errors.Is(err, ErrNotFound) {
		t.Errorf("own scope: %v", err)
	}
	// Scopes are only for server admins.
	if _, err := f.svc.UpdateAssignments(ctx, f.as("admin"), f.ids["m"], AssignmentsUpdate{AdminScopes: &scopes}); !errors.Is(err, ErrForbidden) {
		t.Errorf("scope for a moderator: %v", err)
	}
	unknown := []Target{{ServerID: "nope"}}
	if _, err := f.svc.UpdateAssignments(ctx, f.as("admin"), f.ids["sa"], AssignmentsUpdate{AdminScopes: &unknown}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown server: %v", err)
	}
}

// TestModeratorAssignmentWithinScope covers ROLE-004: a server admin
// assigns servers to moderators only within his scope; attempts outside
// are refused, logged and audited, and change nothing.
func TestModeratorAssignmentWithinScope(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	groups := []string{"mg", "mg0"}
	grants := []Grant{{GroupID: "mg0", Target: Target{ServerGroupID: "mc"}}}
	a, err := f.svc.UpdateAssignments(ctx, f.as("sa"), f.ids["m"], AssignmentsUpdate{Groups: &groups, Grants: &grants})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.Groups, []string{"mg", "mg0", "mg3"}) || len(a.Grants) != 1 {
		t.Errorf("assignments = %+v", a)
	}

	before := f.count(`SELECT count(*) FROM grants`)
	for name, g := range map[string]Grant{
		"server outside the scope":      {GroupID: "mg0", Target: Target{ServerID: "s2"}},
		"server group partly outside":   {GroupID: "mg0", Target: Target{ServerGroupID: "all"}},
		"group of another server admin": {GroupID: "mg3", Target: Target{ServerID: "s1"}},
		"unknown server":                {GroupID: "mg0", Target: Target{ServerID: "nope"}},
		"server and group at once":      {GroupID: "mg0", Target: Target{ServerID: "s1", ServerGroupID: "mc"}},
	} {
		up := []Grant{g}
		_, err := f.svc.UpdateAssignments(ctx, f.as("sa"), f.ids["m"], AssignmentsUpdate{Grants: &up})
		var inv *InvalidError
		if !errors.Is(err, ErrNotFound) && !errors.As(err, &inv) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if f.count(`SELECT count(*) FROM grants`) != before {
		t.Error("a refused assignment changed grants")
	}
	if logged, audited := f.deniedCount(t, 4); logged != 4 || audited != 4 {
		t.Errorf("refusals logged %d, audited %d; want 4 each", logged, audited)
	}

	// Membership in groups of others is not the caller's to change.
	other := []string{"mg3"}
	if _, err := f.svc.UpdateAssignments(ctx, f.as("sa"), f.ids["m"], AssignmentsUpdate{Groups: &other}); !errors.Is(err, ErrNotFound) {
		t.Errorf("join a group of another server admin: %v", err)
	}
	// A grant needs membership in its group.
	none := []string{}
	if _, err := f.svc.UpdateAssignments(ctx, f.as("sa"), f.ids["m"], AssignmentsUpdate{Groups: &none, Grants: &grants}); err == nil {
		t.Error("grant without membership accepted")
	}
	// Leaving sa's groups keeps sa2's group and drops the grants in sa's.
	a, err = f.svc.UpdateAssignments(ctx, f.as("sa"), f.ids["m"], AssignmentsUpdate{Groups: &none})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.Groups, []string{"mg3"}) || len(a.Grants) != 0 {
		t.Errorf("after leaving sa's groups: %+v", a)
	}
}

// TestSupporterGrants covers ROLE-006: with supporters disabled globally
// or for the moderator, he cannot assign supporters.
func TestSupporterGrants(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	want := []SupporterGrant{{ModeratorID: f.ids["m"], ServerID: "s1"}}
	up := AssignmentsUpdate{SupporterGrants: &want}

	// m manages sup only with both switches on; first the server admin tries.
	if _, err := f.svc.UpdateAssignments(ctx, f.as("sa"), f.ids["sup"], up); !errors.Is(err, ErrForbidden) {
		t.Errorf("supporters disabled: %v", err)
	}
	f.exec(`UPDATE settings SET value = 'true' WHERE key = ?`, perm.SettingSupportersEnabled)
	f.checker.Invalidate()
	if _, err := f.svc.UpdateAssignments(ctx, f.as("sa"), f.ids["sup"], up); !errors.Is(err, ErrForbidden) {
		t.Errorf("moderator without release: %v", err)
	}
	f.exec(`UPDATE users SET supporters_allowed = 1 WHERE id = ?`, f.ids["m"])
	f.checker.Invalidate()
	a, err := f.svc.UpdateAssignments(ctx, f.as("m"), f.ids["sup"], up)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.SupporterGrants) != 1 {
		t.Errorf("supporter grants = %+v", a.SupporterGrants)
	}
	r, _ := f.checker.For(ctx, f.ids["sup"])
	if _, ok := r.Server("s1"); !ok {
		t.Error("supporter has no rights on s1")
	}
	outside := []SupporterGrant{{ModeratorID: f.ids["m"], ServerID: "s2"}}
	if _, err := f.svc.UpdateAssignments(ctx, f.as("m"), f.ids["sup"], AssignmentsUpdate{SupporterGrants: &outside}); !errors.Is(err, ErrNotFound) {
		t.Errorf("server outside the moderator's scope: %v", err)
	}
	foreign := []SupporterGrant{{ModeratorID: f.ids["m2"], ServerID: "s1"}}
	if _, err := f.svc.UpdateAssignments(ctx, f.as("m"), f.ids["sup"], AssignmentsUpdate{SupporterGrants: &foreign}); !errors.Is(err, ErrNotFound) {
		t.Errorf("for another moderator: %v", err)
	}
}

// TestSupporterRelease covers ROLE-006 per moderator: only a server admin
// releases supporters for his moderator; switching off takes the
// supporters' rights and sessions.
func TestSupporterRelease(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	f.exec(`UPDATE settings SET value = 'true' WHERE key = ?`, perm.SettingSupportersEnabled)
	on, off := true, false

	if _, err := f.svc.Update(ctx, f.as("sa"), f.ids["sa2"], Update{SupportersAllowed: &on}); !errors.Is(err, ErrNotFound) {
		t.Errorf("release for a server admin: %v", err)
	}
	if _, err := f.svc.Update(ctx, f.as("admin"), f.ids["sa"], Update{SupportersAllowed: &on}); !errors.Is(err, ErrForbidden) {
		t.Errorf("release for a non-moderator: %v", err)
	}
	u, err := f.svc.Update(ctx, f.as("sa"), f.ids["m"], Update{SupportersAllowed: &on})
	if err != nil || !u.SupportersAllowed {
		t.Fatalf("release: %+v %v", u, err)
	}
	grant := []SupporterGrant{{ModeratorID: f.ids["m"], ServerID: "s1"}}
	if _, err := f.svc.UpdateAssignments(ctx, f.as("m"), f.ids["sup"], AssignmentsUpdate{SupporterGrants: &grant}); err != nil {
		t.Fatal(err)
	}
	f.revoked = nil
	if _, err := f.svc.Update(ctx, f.as("sa"), f.ids["m"], Update{SupportersAllowed: &off}); err != nil {
		t.Fatal(err)
	}
	if r, _ := f.checker.For(ctx, f.ids["sup"]); len(r.ServerIDs()) != 0 {
		t.Errorf("supporter keeps rights after the release ended: %v", r.ServerIDs())
	}
	if !slices.Contains(f.revoked, f.ids["sup"]) {
		t.Error("supporter's sessions not ended")
	}
	// The moderator cannot assign supporters any more: without the release
	// he has no user administration (Design 2.2, 3.6).
	if _, err := f.svc.UpdateAssignments(ctx, f.as("m"), f.ids["sup"], AssignmentsUpdate{SupporterGrants: &grant}); !errors.Is(err, ErrForbidden) {
		t.Errorf("assign after the release ended: %v", err)
	}
}
