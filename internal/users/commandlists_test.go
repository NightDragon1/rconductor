// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// TestAllPassableListRefused covers the storage rule of ROLE-007: a list
// in which every command would be passable cannot be saved.
func TestAllPassableListRefused(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	sa := f.as("sa")
	var inv *InvalidError
	if _, err := f.svc.CreateCommandList(ctx, sa, "all open", []Entry{{Path: "kick", Passable: true}, {Path: "say", Passable: true}}); !errors.As(err, &inv) {
		t.Errorf("all passable on create: %v", err)
	}
	l, err := f.svc.CreateCommandList(ctx, sa, "basic", []Entry{{Path: "  whitelist   add ", Passable: false}, {Path: "say", Passable: true}})
	if err != nil {
		t.Fatal(err)
	}
	if l.Entries[0].Path != "say" || l.Entries[1].Path != "whitelist add" {
		t.Errorf("entries = %+v", l.Entries)
	}
	all := []Entry{{Path: "say", Passable: true}, {Path: "whitelist add", Passable: true}}
	if _, err := f.svc.UpdateCommandList(ctx, sa, l.ID, nil, &all, f.version("command_lists", l.ID)); !errors.As(err, &inv) {
		t.Errorf("all passable on update: %v", err)
	}
	if got, _ := f.svc.CommandList(ctx, sa, l.ID); !slices.Equal(got.Entries, l.Entries) {
		t.Error("refused update changed the list")
	}
}

func TestCommandListRules(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	sa := f.as("sa")
	var inv *InvalidError
	for name, entries := range map[string][]Entry{
		"duplicate":    {{Path: "kick"}, {Path: "kick "}},
		"control char": {{Path: "say a\nop x"}},
		"empty":        {{Path: "   "}},
	} {
		if _, err := f.svc.CreateCommandList(ctx, sa, name, entries); !errors.As(err, &inv) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	// CAT-002: any command, also one without catalog entry.
	if _, err := f.svc.CreateCommandList(ctx, sa, "custom", []Entry{{Path: "mymod reload"}}); err != nil {
		t.Errorf("command without catalog entry: %v", err)
	}
	if _, err := f.svc.CreateCommandList(ctx, f.as("m"), "x", nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("moderator creates a list: %v", err)
	}
	if _, err := f.svc.CommandList(ctx, f.as("sa2"), "l1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign list: %v", err)
	}
	// Changing the list changes the moderators' commands without logout.
	f.revoked = nil
	entries := []Entry{{Path: "list"}}
	if _, err := f.svc.UpdateCommandList(ctx, sa, "l1", nil, &entries, f.version("command_lists", "l1")); err != nil {
		t.Fatal(err)
	}
	r, _ := f.checker.For(ctx, f.ids["m"])
	if s, _ := r.Server("s1"); !slices.Equal(s.Commands, []string{"list"}) {
		t.Errorf("moderator commands after the list change: %v", s.Commands)
	}
	if len(f.revoked) != 0 {
		t.Errorf("list change ended sessions of %v", f.revoked)
	}
	if err := f.svc.DeleteCommandList(ctx, sa, "l1"); err != nil {
		t.Fatal(err)
	}
	r, _ = f.checker.For(ctx, f.ids["m"])
	if s, _ := r.Server("s1"); len(s.Commands) != 0 {
		t.Errorf("deleted list still grants %v", s.Commands)
	}
}
