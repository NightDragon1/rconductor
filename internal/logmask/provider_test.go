// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

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

// TestProviderReadErrors (F-93): a broken row keeps the last state; a
// start with a broken row masks with list and profiles; texts sealed with
// another key keep switch and budget, without the texts.
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
			if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(v), MaxTexts: num(int(v) + 1)}); err != nil {
				t.Errorf("write %d: %v", v, err)
			}
		}
	})
	wg.Wait()
	if p.budget != DefaultBudget || line(p, "token=a") != "token=•••" {
		t.Errorf("after the changes: budget %s", p.budget)
	}
	if st, _ := s.Load(ctx); st.MaxTexts != 10 {
		t.Errorf("last change: %+v", st)
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
