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
			_, _ = s.Write(ctx, admin, SettingsInput{IfMatch: version(v), MaxTexts: num(int(v) + 1)})
		}
	})
	wg.Wait()
}
