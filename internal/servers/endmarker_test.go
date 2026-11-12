// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"testing"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon/source"
)

// TestO8TargetEndMarker covers PROT-001: a server with the Factorio
// profile gets the end marker none for its connection, a Minecraft server
// the default.
func TestO8TargetEndMarker(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var factorio string
	if err := f.st.DB.QueryRowContext(ctx, `SELECT id FROM game_profiles WHERE slug = 'factorio'`).Scan(&factorio); err != nil {
		t.Fatal(err)
	}
	name, host, pw := "Factory", "factorio.lan", logging.Secret(rconPassword)
	fs, err := f.svc.Create(ctx, f.as("admin"), Input{Name: &name, ProfileIDs: &[]string{factorio}, Host: &host, Password: &pw})
	if err != nil {
		t.Fatal(err)
	}
	mc := f.create("Survival")
	for id, want := range map[string]string{fs.ID: source.EndMarkerNone, mc.ID: ""} {
		tg, ok, err := f.svc.Target(ctx, id)
		if err != nil || !ok || tg.EndMarker != want {
			t.Errorf("end marker %q (%v, %v), want %q", tg.EndMarker, ok, err, want)
		}
	}
	targets, err := f.svc.Targets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range targets {
		if tg.ID == fs.ID && tg.EndMarker != source.EndMarkerNone {
			t.Errorf("targets: end marker %q", tg.EndMarker)
		}
	}
}
