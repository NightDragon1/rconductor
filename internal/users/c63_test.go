// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"slices"
	"testing"
)

// TestC63ScopesInRequestOrder: the scopes of a server admin read back in
// the order of the request, every time (ROLE-003).
func TestC63ScopesInRequestOrder(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	want := []Target{{ServerID: "s2"}, {ServerGroupID: "mc"}, {ServerID: "s1"}, {ServerGroupID: "all"}}
	for range 10 {
		none := []Target{}
		if _, err := f.svc.UpdateAssignments(ctx, f.as("admin"), f.ids["sa"], AssignmentsUpdate{AdminScopes: &none}); err != nil {
			t.Fatal(err)
		}
		a, err := f.svc.UpdateAssignments(ctx, f.as("admin"), f.ids["sa"], AssignmentsUpdate{AdminScopes: &want})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(a.AdminScopes, want) {
			t.Fatalf("scopes = %+v, want %+v", a.AdminScopes, want)
		}
	}
}
