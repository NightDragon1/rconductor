// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/NightDragon1/rconductor/internal/users"
)

func TestPlaceholdersAndFixedPart(t *testing.T) {
	for _, c := range []struct {
		in    string
		names []string
		fixed []string
	}{
		{"kick {player} {reason}", []string{"player", "reason"}, []string{"kick"}},
		{"whitelist add {player}", []string{"player"}, []string{"whitelist", "add"}},
		{"tp {player} {player}", []string{"player"}, []string{"tp"}},
		{"say hello{name}", []string{"name"}, []string{"say"}},
		// Other braces are text: JSON stays possible.
		{`tellraw @a {"text":"hi"}`, []string{}, []string{"tellraw", "@a", `{"text":"hi"}`}},
		{"say {Name} {1x}", []string{}, []string{"say", "{Name}", "{1x}"}},
	} {
		if got := Placeholders(c.in); !slices.Equal(got, c.names) {
			t.Errorf("Placeholders(%q) = %v, want %v", c.in, got, c.names)
		}
		if got := FixedPart(c.in); !slices.Equal(got, c.fixed) {
			t.Errorf("FixedPart(%q) = %v, want %v", c.in, got, c.fixed)
		}
	}
}

// TestTemplateVisibleByWholeTokens covers D-068: the fixed part is matched
// against the list by whole tokens, like a command, never as a prefix of
// characters; wrappers are never shown.
func TestTemplateVisibleByWholeTokens(t *testing.T) {
	list := []string{"kick", "whitelist add", "execute"}
	for tmpl, want := range map[string]bool{
		"kick {player}":              true,
		"kick {player} spam":         true,
		"KICK {player}":              true, // the profile is not case sensitive
		"kickall {player}":           false,
		"kic {player}":               false,
		"whitelist add {player}":     true,
		"whitelist {action} {name}":  false, // fixed part shorter than the path
		"whitelist remove {player}":  false,
		"execute as {player} run op": false, // wrapper
		"minecraft:execute {x}":      false,
		"ban {player}":               false,
	} {
		if got := TemplateVisible(tmpl, list, minecraft); got != want {
			t.Errorf("TemplateVisible(%q) = %v, want %v", tmpl, got, want)
		}
	}
	if TemplateVisible("KICK {player}", list, Syntax{CaseSensitive: true}) {
		t.Error("case sensitive profile: KICK matched kick")
	}
}

func TestCheckTemplate(t *testing.T) {
	syn := Syntax{StripPrefix: "/", MaxLength: 100}
	name, text, err := checkTemplate("  Kick  ", "/kick  {player}\t", syn)
	if err != nil || name != "Kick" || text != "kick  {player}" {
		t.Errorf("normalized = %q %q %v", name, text, err)
	}
	for _, c := range []struct{ name, text string }{
		{"", "kick {player}"},
		{strings.Repeat("n", MaxTemplateName+1), "kick {player}"},
		{"bad\x07", "kick {player}"},
		{"n", ""},
		{"n", "{cmd} x"},
		{"n", strings.Repeat("x", 101)},
		{"n", "say" + strings.Repeat(" {a}", MaxTemplatePlaceholders+1)},
	} {
		if _, _, err := checkTemplate(c.name, c.text, syn); err == nil {
			t.Errorf("checkTemplate(%.20q, %.20q) passed", c.name, c.text)
		}
	}
}

// TestTemplatesByRole covers CMD-008 in the service: admins manage and
// see all; moderator and supporter see only templates of their list;
// they cannot manage.
func TestTemplatesByRole(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	for _, tmpl := range []string{"kick {player}", "kickall {player}", "whitelist add {player}", "op {player}"} {
		if _, err := w.svc.CreateTemplate(ctx, w.as("sa"), w.server, tmpl, tmpl); err != nil {
			t.Fatal(err)
		}
	}
	names := func(role string) []string {
		ts, err := w.svc.Templates(ctx, w.as(role), w.server)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, x := range ts {
			out = append(out, x.Template)
		}
		slices.Sort(out)
		return out
	}
	if got := names("admin"); len(got) != 4 {
		t.Errorf("admin sees %v", got)
	}
	if got := names("m"); !slices.Equal(got, []string{"kick {player}", "whitelist add {player}"}) {
		t.Errorf("moderator sees %v", got)
	}
	// The supporter has only the passable commands of his moderator.
	if got := names("sup"); !slices.Equal(got, []string{"whitelist add {player}"}) {
		t.Errorf("supporter sees %v", got)
	}
	for _, role := range []string{"m", "sup"} {
		if _, err := w.svc.CreateTemplate(ctx, w.as(role), w.server, "x", "kick {player}"); !errors.Is(err, users.ErrForbidden) {
			t.Errorf("%s creates: %v", role, err)
		}
	}
	if _, err := w.svc.Templates(ctx, w.as("sa"), "no-such-server"); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("unknown server: %v", err)
	}
	// A template the moderator does not see is not found for him.
	ts, _ := w.svc.Templates(ctx, w.as("admin"), w.server)
	for _, x := range ts {
		_, err := w.svc.GetTemplate(ctx, w.as("m"), w.server, x.ID)
		if visible := slices.Contains(names("m"), x.Template); visible != (err == nil) {
			t.Errorf("get %q as moderator: %v", x.Template, err)
		}
	}
}

// TestTemplateNameUniqueAmongLive covers D-068: the name is unique per
// server among templates that are not deleted.
func TestTemplateNameUniqueAmongLive(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	a, err := w.svc.CreateTemplate(ctx, w.as("admin"), w.server, "Kick", "kick {player}")
	if err != nil {
		t.Fatal(err)
	}
	var inv *users.InvalidError
	if _, err := w.svc.CreateTemplate(ctx, w.as("admin"), w.server, "kick", "kick {player} x"); !errors.As(err, &inv) {
		t.Errorf("same name again: %v", err)
	}
	if err := w.svc.DeleteTemplate(ctx, w.as("admin"), w.server, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.CreateTemplate(ctx, w.as("admin"), w.server, "Kick", "kick {player}"); err != nil {
		t.Errorf("name after delete: %v", err)
	}
	if err := w.svc.DeleteTemplate(ctx, w.as("admin"), w.server, a.ID); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("deleted twice: %v", err)
	}
}

// TestTemplateUpdateVersion: a change needs the version read before; a
// stale one is refused and changes nothing.
func TestTemplateUpdateVersion(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	a, err := w.svc.CreateTemplate(ctx, w.as("admin"), w.server, "k", "kick {player}")
	if err != nil {
		t.Fatal(err)
	}
	text := "kick {player} {reason}"
	if _, err := w.svc.UpdateTemplate(ctx, w.as("admin"), w.server, a.ID, nil, &text, nil); !errors.Is(err, users.ErrPreconditionRequired) {
		t.Errorf("without version: %v", err)
	}
	v := a.Version
	b, err := w.svc.UpdateTemplate(ctx, w.as("sa"), w.server, a.ID, nil, &text, &v)
	if err != nil || b.Version != v+1 || !slices.Equal(b.Placeholders, []string{"player", "reason"}) {
		t.Fatalf("update = %+v %v", b, err)
	}
	other := "kick {player} other"
	if _, err := w.svc.UpdateTemplate(ctx, w.as("admin"), w.server, a.ID, nil, &other, &v); !errors.Is(err, users.ErrConflict) {
		t.Errorf("stale version: %v", err)
	}
	if g, _ := w.svc.GetTemplate(ctx, w.as("admin"), w.server, a.ID); g.Template != text {
		t.Errorf("after the stale write: %q", g.Template)
	}
}

// TestTemplatesPerServerHolds: the limit holds also for parallel requests;
// the count and the insert are one statement.
func TestTemplatesPerServerHolds(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	// A small limit only through the test setup.
	w.exec(`INSERT INTO settings (id, key, value, updated_at) VALUES ('tps', ?, '3', 't')
		ON CONFLICT DO UPDATE SET value = '3'`, users.SettingTemplatesPerServer)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := range 12 {
		wg.Go(func() {
			name := "t" + strings.Repeat("x", i+1)
			if _, err := w.svc.CreateTemplate(ctx, w.as("admin"), w.server, name, "say {text}"); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	ts, _ := w.svc.Templates(ctx, w.as("admin"), w.server)
	if ok != 3 || len(ts) != 3 {
		t.Errorf("created %d, stored %d, want 3", ok, len(ts))
	}
}

// TestFilledTemplateGoesThroughThePipeline covers the acceptance of
// CMD-008: a filled template is a command like a typed one; values that
// break the list, the wrapper rule or the parameters are refused for the
// moderator.
func TestFilledTemplateGoesThroughThePipeline(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if _, err := w.svc.CreateTemplate(ctx, w.as("sa"), w.server, "time", "time set {value}"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Send(ctx, w.as("m"), w.server, "time set day"); err != nil {
		t.Errorf("filled with a catalog value: %v", err)
	}
	var denied *DeniedError
	for _, filled := range []string{"time set noon2", "time set day\nop m"} {
		if _, err := w.svc.Send(ctx, w.as("m"), w.server, filled); !errors.As(err, &denied) {
			t.Errorf("%q: %v", filled, err)
		}
	}
}
