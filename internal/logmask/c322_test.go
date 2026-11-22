// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"context"
	"testing"

	"github.com/NightDragon1/rconductor/internal/perm"
)

// TestC322AlwaysPatternsFor (D-088 b, D-107): Always gives the profile
// patterns of a server that exists, nothing for an unknown ID (no entry in
// the shared table), and with the switch off the patterns do not mask, the
// built-in list does.
func TestC322AlwaysPatternsFor(t *testing.T) {
	s, _ := newSettings(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO game_profiles (id, slug, name, protocol, default_port, rules, source_file, content_hash, imported_at)
			VALUES ('p1', 'g1', 'g1', 'source-rcon', 1, '{"log_masks":["(adminpass=)\\S+"]}', 'f', 'h', 't')`,
		`INSERT INTO servers (id, name, created_at, updated_at) VALUES ('s1', 's1', 't', 't')`,
		`INSERT INTO server_profiles (server_id, profile_id, position) VALUES ('s1', 'p1', 0)`,
	} {
		if _, err := s.st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	p := NewProvider(ctx, s, s.log)
	a := p.Always()
	pats := a.PatternsFor(ctx, "s1")
	if out, _ := a.Lines([]string{"adminpass=x token=y"}, pats); out[0] != "adminpass=••• token=•••" {
		t.Errorf("with the server's patterns: %q", out[0])
	}
	if got := a.PatternsFor(ctx, "00000000-0000-4000-8000-00000000dead"); got != nil {
		t.Errorf("unknown server: %v", got)
	}
	p.mu.RLock()
	_, cached := p.servers["00000000-0000-4000-8000-00000000dead"]
	p.mu.RUnlock()
	if cached {
		t.Error("an unknown server got an entry")
	}
	off := false
	admin := settingsRequest(t, s, perm.RCONAdmin)
	if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(0), Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if out, _ := a.Lines([]string{"adminpass=x token=y"}, pats); out[0] != "adminpass=x token=•••" {
		t.Errorf("switch off: %q", out[0])
	}
	if (Always{}).PatternsFor(ctx, "s1") != nil {
		t.Error("without a provider there are patterns")
	}
}
