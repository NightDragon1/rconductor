// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/servers"
)

// TestO9FactorioSlashPipeline covers CAT-005, CMD-004 and CMD-005 with the
// shipped Factorio profile, which keeps the leading "/" (strip_prefix ""):
// the command reaches the game with its slash, list entries and catalog
// paths match with the slash, a list entry without it does not permit the
// slash command, and the wrappers stay refused for moderators.
func TestO9FactorioSlashPipeline(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	var factorio string
	_ = w.st.DB.QueryRowContext(ctx, `SELECT id FROM game_profiles WHERE slug = 'factorio'`).Scan(&factorio)
	var v int64
	_ = w.st.DB.QueryRowContext(ctx, `SELECT version FROM servers WHERE id = ?`, w.server).Scan(&v)
	if _, err := w.srv.Update(ctx, w.as("admin"), w.server, servers.Input{ProfileIDs: &[]string{factorio}, Version: &v}); err != nil {
		t.Fatal(err)
	}
	// The connection follows the new profile; wait until it answers.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := w.send("admin", "/version"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server not usable with the Factorio profile")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := w.send("admin", "/players online"); err != nil {
		t.Fatalf("admin /players online: %v", err)
	}
	if !slices.Contains(w.gameReceived(), "/players online") {
		t.Errorf("game received %q, want the slash kept", w.gameReceived())
	}

	// The moderator's list holds "kick" from before, without the slash.
	if _, err := w.send("m", "/kick Steve griefing"); reason(err) != ReasonNotInList {
		t.Errorf("/kick with list entry kick: %v, want not_in_list", err)
	}
	w.exec(`INSERT INTO command_list_entries (id, command_list_id, path, passable) VALUES ('o9', 'l', '/kick', 0)`)
	w.c.Invalidate()
	if _, err := w.send("m", "/kick Steve griefing"); err != nil {
		t.Errorf("/kick with list entry /kick: %v", err)
	}
	if !slices.Contains(w.gameReceived(), "/kick Steve griefing") {
		t.Errorf("game received %q", w.gameReceived())
	}
	a, err := w.svc.AllowedCommands(ctx, w.as("m"), w.server)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(a.Commands, func(c profiles.Command) bool { return c.Path == "/kick" })
	if i < 0 || len(a.Commands[i].Params) != 2 || a.Commands[i].Description == "" {
		t.Errorf("/kick without its catalog entry: %+v", a.Commands)
	}

	for _, cmd := range []string{"/c game.print(1)", "/silent-command game.print(1)"} {
		if _, err := w.send("m", cmd); reason(err) != ReasonWrapperBlocked {
			t.Errorf("%s: %v, want wrapper_blocked", cmd, err)
		}
	}
}
