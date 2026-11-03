// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// TestGroupAssignmentAppliesToMembers covers AUTH-002: a grant to a
// moderator group applies to all members; removing a member takes his
// rights at once.
func TestGroupAssignmentAppliesToMembers(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	sa := f.as("sa")
	// sa invited m3; both m and m3 are his to manage.
	f.ids["m3"] = storeUser(t, f, "m3", "moderator", "sa")

	g, err := f.svc.CreateGroup(ctx, sa, "night shift", KindModerator, "l1")
	if err != nil {
		t.Fatal(err)
	}
	members := []string{f.ids["m"], f.ids["m3"]}
	grants := []Target{{ServerID: "s1"}}
	if _, err := f.svc.UpdateGroup(ctx, sa, g.ID, GroupUpdate{Members: &members, Grants: &grants, Version: f.version("user_groups", g.ID)}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"m", "m3"} {
		r, _ := f.checker.For(ctx, f.ids[m])
		if _, ok := r.Server("s1"); !ok {
			t.Errorf("%s has no rights on s1 through the group", m)
		}
	}
	f.revoked = nil
	only := []string{f.ids["m"]}
	if _, err := f.svc.UpdateGroup(ctx, sa, g.ID, GroupUpdate{Members: &only, Version: f.version("user_groups", g.ID)}); err != nil {
		t.Fatal(err)
	}
	r, _ := f.checker.For(ctx, f.ids["m3"])
	if _, ok := r.Server("s1"); ok {
		t.Error("removed member keeps the group's rights")
	}
	if !slices.Equal(f.revoked, []string{f.ids["m3"]}) {
		t.Errorf("sessions ended for %v, want only the removed member", f.revoked)
	}
}

func TestGroupRules(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	sa := f.as("sa")
	if _, err := f.svc.CreateGroup(ctx, f.as("m"), "x", KindGeneral, ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("moderator creates a group: %v", err)
	}
	if _, err := f.svc.CreateGroup(ctx, sa, "x", KindGeneral, "l1"); err == nil {
		t.Error("general group with a list accepted")
	}
	if _, err := f.svc.CreateGroup(ctx, sa, " ", KindModerator, ""); err == nil {
		t.Error("empty name accepted")
	}
	if _, err := f.svc.CreateGroup(ctx, sa, "mg", KindModerator, ""); err == nil {
		t.Error("duplicate name of the same owner accepted")
	}
	// Another server admin's group is invisible.
	if _, err := f.svc.UpdateGroup(ctx, sa, "mg3", GroupUpdate{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign group: %v", err)
	}
	gs, _ := f.svc.Groups(ctx, sa)
	var names []string
	for _, g := range gs {
		names = append(names, g.Name)
	}
	if !slices.Equal(names, []string{"mg", "mg0"}) {
		t.Errorf("sa sees groups %v", names)
	}
	// Grants outside the scope and members outside management are refused.
	out := []Target{{ServerID: "s2"}}
	if _, err := f.svc.UpdateGroup(ctx, sa, "mg0", GroupUpdate{Grants: &out}); !errors.Is(err, ErrNotFound) {
		t.Errorf("grant outside the scope: %v", err)
	}
	foreign := []string{f.ids["m2"]}
	if _, err := f.svc.UpdateGroup(ctx, sa, "mg0", GroupUpdate{Members: &foreign}); !errors.Is(err, ErrNotFound) {
		t.Errorf("member outside management: %v", err)
	}
	sup := []string{f.ids["sup"]}
	var inv *InvalidError
	if _, err := f.svc.UpdateGroup(ctx, sa, "mg0", GroupUpdate{Members: &sup}); !errors.As(err, &inv) {
		t.Errorf("supporter in a moderator group: %v", err)
	}
	// Deleting a group takes its rights and ends members' sessions.
	f.revoked = nil
	if err := f.svc.DeleteGroup(ctx, sa, "mg"); err != nil {
		t.Fatal(err)
	}
	r, _ := f.checker.For(ctx, f.ids["m"])
	if _, ok := r.Server("s1"); ok {
		t.Error("rights survive the deleted group")
	}
	if !slices.Contains(f.revoked, f.ids["m"]) {
		t.Error("member's sessions not ended")
	}
}

// TestVersionFollowsEveryChange: the version of a group rises with every
// change, also a member added through the user's assignments; a stale
// version is refused and writes nothing (revision 2.4 candidate 24).
func TestVersionFollowsEveryChange(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	sa := f.as("sa")
	g, err := f.svc.CreateGroup(ctx, sa, "versioned", KindModerator, "l1")
	if err != nil {
		t.Fatal(err)
	}
	start := *f.version("user_groups", g.ID)
	groups := []string{g.ID}
	if _, err := f.svc.UpdateAssignments(ctx, sa, f.ids["m"], AssignmentsUpdate{Groups: &groups}); err != nil {
		t.Fatal(err)
	}
	if v := *f.version("user_groups", g.ID); v <= start {
		t.Fatalf("version %d after a member came through assignments, was %d", v, start)
	}
	name := "renamed"
	stale := start
	if _, err := f.svc.UpdateGroup(ctx, sa, g.ID, GroupUpdate{Name: &name, Version: &stale}); !errors.Is(err, ErrConflict) {
		t.Errorf("stale version: %v, want ErrConflict", err)
	}
	if got, _ := f.svc.Group(ctx, sa, g.ID); got.Name != "versioned" {
		t.Errorf("a stale write renamed the group to %q", got.Name)
	}
	if _, err := f.svc.UpdateGroup(ctx, sa, g.ID, GroupUpdate{Name: &name}); !errors.Is(err, ErrPreconditionRequired) {
		t.Errorf("no version: %v, want ErrPreconditionRequired", err)
	}
	// Settings: a change by another way raises the version too.
	before := *f.version("settings", "")
	if _, err := f.st.DB.ExecContext(ctx, `INSERT INTO settings (id, key, value, updated_at) VALUES ('v1', 'invite.lifetime', '"80h"', 't')`); err != nil {
		t.Fatal(err)
	}
	if v := *f.version("settings", ""); v <= before {
		t.Errorf("settings version %d after a change, was %d", v, before)
	}
}
