// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/users"
)

func (f *fixture) profileID(t *testing.T, slug string) string {
	t.Helper()
	var id string
	if err := f.st.DB.QueryRowContext(context.Background(), `SELECT id FROM game_profiles WHERE slug = ?`, slug).Scan(&id); err != nil {
		t.Fatalf("%s: %v", slug, err)
	}
	return id
}

func (f *fixture) storedProfiles(t *testing.T, id string) []string {
	t.Helper()
	got, err := f.st.Queries.GetServerProfiles(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestProfileList covers SRV-008 and CAT-007 (D-074): a specific profile
// is sorted in above the topmost generic one, a generic one goes to the
// end, a given order of profiles on the list stays; the list comes back
// with the server; all profiles need the same protocol (422); unknown,
// twice, none or too many are refused.
func TestProfileList(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mc, gtnh, paper := f.profileID(t, "minecraft-java"), f.profileID(t, "minecraft-gtnh"), f.profileID(t, "minecraft-paper")
	sf := f.profileID(t, "satisfactory")

	// New on a new server: GTNH (specific) goes above Minecraft (generic).
	name, host, pw := "pack", "mc.lan", secretOf(rconPassword)
	sv, err := f.svc.Create(ctx, f.as("admin"), Input{Name: &name, ProfileIDs: &[]string{mc, gtnh}, Host: &host, Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sv.ProfileIDs, []string{gtnh, mc}) || sv.ProfileID != gtnh {
		t.Fatalf("created with %v (top %s), want GTNH above Minecraft", sv.ProfileIDs, sv.ProfileID)
	}
	if got := f.storedProfiles(t, sv.ID); !slices.Equal(got, []string{gtnh, mc}) {
		t.Errorf("stored %v", got)
	}
	// Adding a generic profile puts it at the end.
	got, err := f.svc.Update(ctx, f.as("admin"), sv.ID, Input{ProfileIDs: &[]string{paper, gtnh, mc}, Version: f.version(sv.ID)})
	if err != nil || !slices.Equal(got.ProfileIDs, []string{gtnh, mc, paper}) {
		t.Fatalf("add generic: %v %v", got.ProfileIDs, err)
	}
	// Reordering what is on the list keeps the given order, even generic
	// above specific (the interface shows a hint then).
	got, err = f.svc.Update(ctx, f.as("admin"), sv.ID, Input{ProfileIDs: &[]string{mc, gtnh, paper}, Version: f.version(sv.ID)})
	if err != nil || !slices.Equal(got.ProfileIDs, []string{mc, gtnh, paper}) {
		t.Fatalf("reorder: %v %v", got.ProfileIDs, err)
	}

	before := f.storedProfiles(t, sv.ID)
	tooMany := make([]string, maxProfiles+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("p%d", i)
	}
	for label, list := range map[string][]string{
		"mixed protocols": {gtnh, sf},
		"unknown":         {gtnh, "nope"},
		"twice":           {gtnh, gtnh},
		"none":            {},
		"too many":        tooMany,
	} {
		var inv *users.InvalidError
		if _, err := f.svc.Update(ctx, f.as("admin"), sv.ID, Input{ProfileIDs: &list, Version: f.version(sv.ID)}); !errors.As(err, &inv) {
			t.Errorf("%s: %v, want 422", label, err)
		}
	}
	if got := f.storedProfiles(t, sv.ID); !slices.Equal(got, before) {
		t.Errorf("a refused list changed the stored one: %v", got)
	}
	// The audit names the old and the new list.
	var payload string
	if err := f.st.DB.QueryRow(`SELECT payload FROM audit_log WHERE action = 'server.update' AND target_id = ? ORDER BY ts DESC, rowid DESC LIMIT 1`, sv.ID).Scan(&payload); err != nil || !strings.Contains(payload, `"profile_ids"`) || !strings.Contains(payload, gtnh) {
		t.Errorf("audit payload %q %v", payload, err)
	}
}

// TestProfileListRights is the rights matrix for profile_ids (SRV-008,
// D-074): the RCON admin changes everything; a server admin, in its scope,
// only the profile list; with any other field 403 and nothing changes;
// outside its scope 404; a moderator 403.
func TestProfileListRights(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mc, gtnh := f.profileID(t, "minecraft-java"), f.profileID(t, "minecraft-gtnh")
	in, out := f.create("in-scope"), f.create("out-of-scope")
	g, err := f.svc.CreateGroup(ctx, f.as("admin"), "scope", []string{in.ID})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO admin_scopes (id, user_id, server_group_id, assigned_by, created_at) VALUES ('s1', ?, ?, ?, 't')`, f.ids["sa"], g.ID, f.ids["admin"])
	f.c.Invalidate()

	both := []string{gtnh, mc}
	newName, newHost, newPort := "renamed", "other.lan", 4000
	cases := []struct {
		who, server, label string
		in                 Input
		want               error
	}{
		{"admin", in.ID, "admin: list", Input{ProfileIDs: &both}, nil},
		{"admin", in.ID, "admin: list and name", Input{ProfileIDs: &[]string{mc}, Name: &newName}, nil},
		{"sa", in.ID, "server admin: list", Input{ProfileIDs: &both}, nil},
		{"sa", in.ID, "server admin: list and name", Input{ProfileIDs: &[]string{mc}, Name: &newName}, users.ErrForbidden},
		{"sa", in.ID, "server admin: list and host", Input{ProfileIDs: &[]string{mc}, Host: &newHost}, users.ErrForbidden},
		{"sa", in.ID, "server admin: list and port", Input{ProfileIDs: &[]string{mc}, Port: &newPort}, users.ErrForbidden},
		{"sa", in.ID, "server admin: password", Input{Password: secretOf("x")}, users.ErrForbidden},
		{"sa", in.ID, "server admin: nothing", Input{}, users.ErrForbidden},
		{"sa", out.ID, "server admin: other server", Input{ProfileIDs: &both}, users.ErrNotFound},
		{"sa", out.ID, "server admin: other server, name", Input{Name: &newName}, users.ErrForbidden},
		{"sa", "no-such-server", "server admin: missing server, name", Input{Name: &newName}, users.ErrForbidden},
		{"m", in.ID, "moderator: list", Input{ProfileIDs: &both}, users.ErrForbidden},
	}
	for _, tc := range cases {
		before := f.storedProfiles(t, tc.server)
		beforeSrv, _ := f.st.Queries.GetServer(ctx, tc.server)
		if tc.server == "no-such-server" {
			one := int64(1)
			tc.in.Version = &one
		} else {
			tc.in.Version = f.version(tc.server)
		}
		_, err := f.svc.Update(ctx, f.as(tc.who), tc.server, tc.in)
		switch {
		case tc.want == nil && err != nil:
			t.Errorf("%s: %v", tc.label, err)
		case tc.want != nil && !errors.Is(err, tc.want):
			t.Errorf("%s: %v, want %v", tc.label, err, tc.want)
		case tc.want != nil:
			// Refused as a whole: neither the list nor any field changed.
			after, _ := f.st.Queries.GetServer(ctx, tc.server)
			if got := f.storedProfiles(t, tc.server); !slices.Equal(got, before) || after.Name != beforeSrv.Name || after.Host != beforeSrv.Host || after.Port != beforeSrv.Port {
				t.Errorf("%s: refused but changed: %v %+v", tc.label, got, after)
			}
		}
	}
	if got := f.storedProfiles(t, in.ID); !slices.Equal(got, []string{gtnh, mc}) {
		t.Errorf("after the server admin's change: %v", got)
	}
}

// TestProfileListKeepsPortC10 covers D-076: a server admin's profile list
// keeps the port the RCON admin set.
func TestProfileListKeepsPortC10(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mc, src := f.profileID(t, "minecraft-java"), f.profileID(t, "source-engine")
	name, host, port, pw := "own-port", "mc.lan", 30000, secretOf(rconPassword)
	sv, err := f.svc.Create(ctx, f.as("admin"), Input{Name: &name, ProfileIDs: &[]string{mc}, Host: &host, Port: &port, Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	g, err := f.svc.CreateGroup(ctx, f.as("admin"), "scope", []string{sv.ID})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO admin_scopes (id, user_id, server_group_id, assigned_by, created_at) VALUES ('c10', ?, ?, ?, 't')`, f.ids["sa"], g.ID, f.ids["admin"])
	f.c.Invalidate()
	got, err := f.svc.Update(ctx, f.as("sa"), sv.ID, Input{ProfileIDs: &[]string{src}, Version: f.version(sv.ID)})
	if err != nil {
		t.Fatal(err)
	}
	// The server admin's view carries no RCON port (D-056); the stored one
	// stays.
	if got.Port != 0 || got.Host != "" {
		t.Errorf("server admin's view: host %q port %d, want none", got.Host, got.Port)
	}
	if row, err := f.st.Queries.GetServer(ctx, sv.ID); err != nil || row.Port != 30000 {
		t.Errorf("stored port %d (%v), want 30000", row.Port, err)
	}
}
