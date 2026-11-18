// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/users"
)

func line(p *Provider, s string, patterns ...[]string) string {
	out, _ := p.Lines([]string{s}, patterns)
	return out[0]
}

// TestProviderFollowsSettings: without a row the list masks; a change of
// the admin applies at once; off, lines pass as they are (UI-022).
func TestProviderFollowsSettings(t *testing.T) {
	s, _ := newSettings(t)
	ctx := context.Background()
	p := NewProvider(ctx, s, nil)
	if got := line(p, "token=a "+secretText); got != "token=••• "+secretText {
		t.Errorf("defaults: %q", got)
	}
	admin := settingsRequest(t, s, perm.RCONAdmin)
	if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(0), SetTexts: true, Texts: []Text{{Value: secretText}}}); err != nil {
		t.Fatal(err)
	}
	if got := line(p, "token=a "+secretText); got != "token=••• •••" {
		t.Errorf("after the change: %q", got)
	}
	off := false
	if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(1), Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if got := line(p, "token=a "+secretText); got != "token=a "+secretText {
		t.Errorf("off: %q", got)
	}
}

// TestProviderReadErrors (F-93): a broken row switches masking on and
// keeps budget and texts as last read; a start with a broken row masks
// with list and profiles; texts sealed with another key keep switch and
// budget, without the texts.
func TestProviderReadErrors(t *testing.T) {
	s, logs := newSettings(t)
	ctx := context.Background()
	admin := settingsRequest(t, s, perm.RCONAdmin)
	if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(0), SetTexts: true, Texts: []Text{{Value: secretText}}}); err != nil {
		t.Fatal(err)
	}
	p := NewProvider(ctx, s, nil)
	other := NewProvider(ctx, NewSettings(s.st, newBox(t), nil), nil)
	off := false
	if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(1), Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	other.Reload(ctx)
	if got := line(other, "token=a"); got != "token=a" {
		t.Errorf("other master key, stored off: %q", got)
	}
	on := true
	if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(2), Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	other.Reload(ctx)
	if _, err := s.st.DB.Exec(`UPDATE settings SET value = 'broken' WHERE key = ?`, users.SettingLogMasking); err != nil {
		t.Fatal(err)
	}
	p.Reload(ctx)
	fresh := NewProvider(ctx, s, nil)
	// A stored "off" that can no longer be read counts as on (F-93).
	q := NewProvider(ctx, NewSettings(s.st, s.box, nil), nil)
	if _, err := s.st.DB.Exec(`UPDATE settings SET value = ? WHERE key = ?`, `{"version":9,"enabled":false,"max_texts":50,"max_text_chars":256,"budget_ms":1234}`, users.SettingLogMasking); err != nil {
		t.Fatal(err)
	}
	q.Reload(ctx)
	if got := line(q, "token=a"); got != "token=a" {
		t.Fatalf("stored off: %q", got)
	}
	if _, err := s.st.DB.Exec(`UPDATE settings SET value = 'broken' WHERE key = ?`, users.SettingLogMasking); err != nil {
		t.Fatal(err)
	}
	q.Reload(ctx)
	if got := line(q, "token=a"); got != "token=•••" {
		t.Errorf("off, then unreadable: %q", got)
	}
	if q.budget != 1234*time.Millisecond {
		t.Errorf("off, then unreadable: budget %s, want the one last read", q.budget)
	}
	for name, c := range map[string]struct {
		p    *Provider
		want string
	}{
		"last state kept":     {p, "token=••• •••"},
		"start without state": {fresh, "token=••• " + secretText},
		"other master key":    {other, "token=••• " + secretText},
	} {
		if got := line(c.p, "token=a "+secretText); got != c.want {
			t.Errorf("%s: %q", name, got)
		}
	}
	if !strings.Contains(logs.String(), "setting.read_failed") || strings.Contains(logs.String(), "s3cr3t") {
		t.Errorf("log: %s", logs.String())
	}
}

// TestProviderProfiles: the patterns of the profiles mask, a pattern the
// check refuses is left out; the budget of the settings bounds a batch;
// the cache of compiled patterns stays bounded.
func TestProviderProfiles(t *testing.T) {
	s, _ := newSettings(t)
	ctx := context.Background()
	p := NewProvider(ctx, s, nil)
	patterns := [][]string{{`(adminpass=)\S+`, `k=\S+`}, {`(pw=)\S+`}}
	if got := line(p, "adminpass=x k=y pw=z", patterns...); got != "adminpass=••• k=y pw=•••" {
		t.Errorf("profiles: %q", got)
	}
	admin := settingsRequest(t, s, perm.RCONAdmin)
	if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(0), BudgetMS: num(MinBudgetMS)}); err != nil {
		t.Fatal(err)
	}
	slow := [][]string{{`(pw=)\S+(?: .*END)?`}}
	lines := make([]string, agentproto.MaxLogBytes/agentproto.MaxLogLine)
	for i := range lines {
		lines[i] = strings.Repeat("pw=a ", agentproto.MaxLogLine/5)
	}
	if _, withheld := p.Lines(lines, slow); withheld < len(lines)-2 {
		t.Errorf("budget of %d ms: %d of %d withheld", MinBudgetMS, withheld, len(lines))
	}
	for i := range maxCompiled + 10 {
		p.Lines([]string{"x"}, [][]string{{fmt.Sprintf(`(k%d=)\S+`, i)}})
	}
	p.mu.RLock()
	n := len(p.compiled)
	p.mu.RUnlock()
	if n > maxCompiled {
		t.Errorf("cache holds %d", n)
	}
}

// TestProviderParallel: lines in parallel with changes of the settings.
func TestProviderParallel(t *testing.T) {
	s, _ := newSettings(t)
	ctx := context.Background()
	p := NewProvider(ctx, s, nil)
	admin := settingsRequest(t, s, perm.RCONAdmin)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for range 50 {
				if got := line(p, "token=a", []string{fmt.Sprintf(`(k%d=)\S+`, i)}); got != "token=•••" {
					t.Errorf("line: %q", got)
					return
				}
			}
		})
	}
	wg.Go(func() {
		for v := range int64(10) {
			if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(v), BudgetMS: num(MaxBudgetMS - int(v))}); err != nil {
				t.Errorf("write %d: %v", v, err)
			}
		}
	})
	wg.Wait()
	// Budgets near the upper end: a loaded runner never withholds the line.
	if want := time.Duration(MaxBudgetMS-9) * time.Millisecond; p.budget != want {
		t.Errorf("after the changes: budget %s, want %s", p.budget, want)
	}
}

// TestProviderOrder: two providers on the same settings both follow every
// change, also a repair that lowers the version.
func TestProviderOrder(t *testing.T) {
	s, _ := newSettings(t)
	ctx := context.Background()
	p, q := NewProvider(ctx, s, nil), NewProvider(ctx, s, nil)
	admin := settingsRequest(t, s, perm.RCONAdmin)
	off := false
	if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(0), Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	for name, pr := range map[string]*Provider{"first": p, "second": q} {
		if got := line(pr, "token=a"); got != "token=a" {
			t.Errorf("%s provider: %q", name, got)
		}
	}
	for v := int64(1); v < 6; v++ {
		if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(v), MaxTexts: num(int(v))}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.st.DB.Exec(`UPDATE settings SET value = 'broken' WHERE key = ?`, users.SettingLogMasking); err != nil {
		t.Fatal(err)
	}
	on := true
	st, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(0), Enabled: &on, MaxTexts: num(5), TextRunes: num(64), BudgetMS: num(90),
		SetTexts: true, Texts: []Text{}})
	if err != nil || st.Version != 1 {
		t.Fatalf("repair: %+v %v", st, err)
	}
	for name, pr := range map[string]*Provider{"first": p, "second": q} {
		if got := line(pr, "token=a"); got != "token=•••" {
			t.Errorf("%s provider after the repair: %q", name, got)
		}
	}
}

// TestProviderServerLines: the patterns of each profile of a server mask
// its lines; when the profiles cannot be read, the patterns as last read
// hold, without any the list and the texts (F-93).
func TestProviderServerLines(t *testing.T) {
	s, logs := newSettings(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO game_profiles (id, slug, name, protocol, default_port, rules, source_file, content_hash, imported_at)
			VALUES ('p1', 'g1', 'g1', 'source-rcon', 1, '{"log_masks":["(adminpass=)\\S+"]}', 'f', 'h', 't'),
			('p2', 'g2', 'g2', 'source-rcon', 1, '{"log_masks":["(pw=)\\S+"]}', 'f', 'h', 't')`,
		`INSERT INTO servers (id, name, created_at, updated_at) VALUES ('s1', 's1', 't', 't'), ('s2', 's2', 't', 't')`,
		`INSERT INTO server_profiles (server_id, profile_id, position) VALUES ('s1', 'p1', 0), ('s1', 'p2', 1), ('s2', 'p2', 0)`,
	} {
		if _, err := s.st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	p := NewProvider(ctx, s, s.log)
	const in = "adminpass=x pw=y token=z"
	if got := p.ServerLines(ctx, "s1", []string{in})[0]; got != "adminpass=••• pw=••• token=•••" {
		t.Errorf("both profiles: %q", got)
	}
	if _, err := s.st.DB.Exec(`ALTER TABLE server_profiles RENAME TO server_profiles_gone`); err != nil {
		t.Fatal(err)
	}
	for server, want := range map[string]string{
		"s1": "adminpass=••• pw=••• token=•••", // as last read
		"s2": "adminpass=x pw=y token=•••",     // never read: the list
	} {
		if got := p.ServerLines(ctx, server, []string{in})[0]; got != want {
			t.Errorf("%s unreadable: %q", server, got)
		}
	}
	if !strings.Contains(logs.String(), "setting.read_failed") {
		t.Errorf("log: %s", logs.String())
	}
}
