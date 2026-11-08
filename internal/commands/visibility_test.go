// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"slices"
	"testing"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// sent writes a sent command of a user into the audit as the pipeline does;
// role "" stands for an entry of 1.0 without the role.
func (w *world) sent(t *testing.T, userID, role, command string) {
	t.Helper()
	if _, err := audit.Write(context.Background(), w.st.DB, audit.Entry{
		Kind: audit.KindCommand, ActorID: userID, ActorRole: role, ServerID: w.server,
		TargetType: "server", TargetID: w.server, Action: ActionSent, Result: audit.ResultOK,
		Payload: map[string]string{"command": command},
	}); err != nil {
		t.Fatal(err)
	}
}

func (w *world) visible(t *testing.T, viewer string, own bool) []string {
	t.Helper()
	h, _, err := w.svc.History(context.Background(), w.as(viewer), w.server, 200, "", own)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range h {
		out = append(out, e.Command)
	}
	slices.Sort(out)
	return out
}

// TestHistoryVisibilityByRole covers ROLE-009 (D-067) for the history:
// viewer role × author role; the filter own; a role change of the author
// after the command; entries of 1.0 without the role; deleted users keep
// the class of their commands.
func TestHistoryVisibilityByRole(t *testing.T) {
	w := newWorld(t)
	for name, role := range map[string]string{"admin": "rcon_admin", "sa": "server_admin", "m": "moderator", "sup": "supporter"} {
		w.sent(t, w.ids[name], role, "by "+name)
	}
	want := map[string][]string{
		"admin": {"by admin", "by m", "by sa", "by sup"},
		"sa":    {"by m", "by sa", "by sup"},
		"m":     {"by m", "by sup"},
		"sup":   {"by m", "by sup"},
	}
	for viewer, cmds := range want {
		if got := w.visible(t, viewer, false); !slices.Equal(got, cmds) {
			t.Errorf("%s sees %v, want %v", viewer, got, cmds)
		}
		if got := w.visible(t, viewer, true); !slices.Equal(got, []string{"by " + viewer}) {
			t.Errorf("%s own sees %v", viewer, got)
		}
	}

	// The role at the time counts: a moderator who became a server admin
	// keeps his old commands in the pool, his new ones leave it.
	ctx := context.Background()
	mod2 := storetest.CreateUser(t, w.st, storetest.User{Pseudonym: "mod2", Role: "moderator"})
	w.sent(t, mod2, "moderator", "mod2 as moderator")
	if _, err := w.st.DB.ExecContext(ctx, `UPDATE users SET role = 'server_admin' WHERE id = ?`, mod2); err != nil {
		t.Fatal(err)
	}
	w.sent(t, mod2, "server_admin", "mod2 as server admin")
	if got := w.visible(t, "m", false); !slices.Contains(got, "mod2 as moderator") || slices.Contains(got, "mod2 as server admin") {
		t.Errorf("after the role change m sees %v", got)
	}

	// Entries of 1.0 without the role take the stored one, also of a
	// deleted user (D-028).
	for _, u := range []struct{ name, role, command string }{
		{"oldmod", "moderator", "deleted moderator"},
		{"oldsa", "server_admin", "deleted server admin"},
		{"oldadmin", "rcon_admin", "deleted rcon admin"},
	} {
		id := storetest.CreateUser(t, w.st, storetest.User{Pseudonym: u.name, Role: u.role})
		w.sent(t, id, "", u.command)
		if _, err := w.st.DB.ExecContext(ctx, `UPDATE users SET deleted_at = 't' WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
	}
	for viewer, seen := range map[string]map[string]bool{
		"admin": {"deleted moderator": true, "deleted server admin": true, "deleted rcon admin": true},
		"sa":    {"deleted moderator": true, "deleted server admin": true, "deleted rcon admin": false},
		"m":     {"deleted moderator": true, "deleted server admin": false, "deleted rcon admin": false},
		"sup":   {"deleted moderator": true, "deleted server admin": false, "deleted rcon admin": false},
	} {
		got := w.visible(t, viewer, false)
		for cmd, want := range seen {
			if slices.Contains(got, cmd) != want {
				t.Errorf("%s sees %q: %v, want %v", viewer, cmd, !want, want)
			}
		}
	}

	// An entry without any user record counts as an RCON admin's.
	w.sent(t, "", "", "no user")
	if slices.Contains(w.visible(t, "sa", false), "no user") || !slices.Contains(w.visible(t, "admin", false), "no user") {
		t.Error("entry without a user")
	}
}

// TestHistoryParametersCannotWiden covers the abuse case of ROLE-009: a
// moderator who pages with any cursor or asks for own gets no command of
// an admin.
func TestHistoryParametersCannotWiden(t *testing.T) {
	w := newWorld(t)
	for range 5 {
		w.sent(t, w.ids["admin"], "rcon_admin", "admin")
		w.sent(t, w.ids["sa"], "server_admin", "sa")
		w.sent(t, w.ids["m"], "moderator", "m")
	}
	cursor := ""
	for range 10 {
		h, next, err := w.svc.History(context.Background(), w.as("m"), w.server, 1, cursor, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range h {
			if e.Command != "m" {
				t.Fatalf("moderator got %q", e.Command)
			}
		}
		if next == "" {
			break
		}
		cursor = next
	}
	for _, c := range []string{"2099-01-01T00:00:00.000Z|zzzz", "x", "|"} {
		h, _, _ := w.svc.History(context.Background(), w.as("m"), w.server, 50, c, false)
		for _, e := range h {
			if e.Command != "m" {
				t.Errorf("cursor %q: moderator got %q", c, e.Command)
			}
		}
	}
}

// TestVisibleAuthorsAndRanks keeps the ranks of the query equal to
// VisibleAuthors.
func TestVisibleAuthorsAndRanks(t *testing.T) {
	for _, viewer := range append(slices.Clone(perm.Roles), "nobody") {
		for _, author := range append(slices.Clone(perm.Roles), "") {
			byList := slices.Contains(perm.VisibleAuthors(viewer), perm.AuthorRole(string(author), "", false))
			byRank := perm.AuthorRank(perm.AuthorRole(string(author), "", false)) >= perm.MinVisibleRank(viewer)
			if byList != byRank {
				t.Errorf("viewer %s, author %q: list %v, rank %v", viewer, author, byList, byRank)
			}
		}
	}
}
