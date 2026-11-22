// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"context"
	"strings"
	"testing"
)

// oneProfileServer stores server s1 with profile p1 and its patterns.
func oneProfileServer(t *testing.T, s *Settings, rules string) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO game_profiles (id, slug, name, protocol, default_port, rules, source_file, content_hash, imported_at)
			VALUES ('p1', 'g1', 'g1', 'source-rcon', 1, '` + rules + `', 'f', 'h', 't')`,
		`INSERT INTO servers (id, name, created_at, updated_at) VALUES ('s1', 's1', 't', 't')`,
		`INSERT INTO server_profiles (server_id, profile_id, position) VALUES ('s1', 'p1', 0)`,
	} {
		if _, err := s.st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func setRules(t *testing.T, s *Settings, rules string) {
	t.Helper()
	if _, err := s.st.DB.Exec(`UPDATE game_profiles SET rules = ? WHERE id = 'p1'`, rules); err != nil {
		t.Fatal(err)
	}
}

// TestC254ChangeDuringRead (UI-022, D-088 b): a change told while the
// patterns are read leaves them stale, so the next batch reads them again
// and masks with the new ones.
func TestC254ChangeDuringRead(t *testing.T) {
	s, _ := newSettings(t)
	ctx := context.Background()
	oneProfileServer(t, s, `{"log_masks":["(pw=)\\S+"]}`)
	p := NewProvider(ctx, s, s.log)
	p.mu.RLock()
	gen := p.gen
	p.mu.RUnlock()
	// The read starts at gen; the change comes before it ends.
	setRules(t, s, `{"log_masks":["(k=)\\S+"]}`)
	p.ProfilesChanged()
	p.readPatterns(ctx, "s1", gen, nil)
	if got := p.ServerLines(ctx, "s1", []string{"pw=y k=z"})[0]; got != "pw=y k=•••" {
		t.Errorf("after a change during the read: %q", got)
	}
}

// TestC254ReadErrorLoggedAgain (UI-022, F-93): a read error is logged
// once, a read that succeeds ends it, and a new error is logged again.
func TestC254ReadErrorLoggedAgain(t *testing.T) {
	s, logs := newSettings(t)
	ctx := context.Background()
	oneProfileServer(t, s, `{"log_masks":["(pw=)\\S+"]}`)
	p := NewProvider(ctx, s, s.log)
	count := func() int { return strings.Count(logs.String(), "profile masking patterns not readable") }
	read := func() string {
		p.ProfilesChanged()
		return p.ServerLines(ctx, "s1", []string{"pw=y k=z"})[0]
	}
	for i, step := range []struct {
		rules, want string
		logged      int
	}{
		{`broken`, "pw=y k=z", 1},
		{`broken`, "pw=y k=z", 1},
		{`{"log_masks":["(pw=)\\S+"]}`, "pw=••• k=z", 1},
		{`broken`, "pw=••• k=z", 2}, // as last read
		{`broken`, "pw=••• k=z", 2},
	} {
		setRules(t, s, step.rules)
		if got := read(); got != step.want || count() != step.logged {
			t.Errorf("step %d: %q, logged %d, want %q and %d", i, got, count(), step.want, step.logged)
		}
	}
}
