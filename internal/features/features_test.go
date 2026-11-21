// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package features

import (
	"testing"

	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

func TestNewRefusesBadFlags(t *testing.T) {
	for _, bad := range [][]Flag{{{Name: ""}}, {{Name: "Upper"}}, {{Name: "a.b"}}, {{Name: "a"}, {Name: "a"}},
		{{Name: "a", Operations: []string{"X"}}, {Name: "b", Operations: []string{"X"}}}} {
		if _, err := New(bad...); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}

func TestDefaultKnowsServerSaveAsNew(t *testing.T) {
	s := Default()
	if !s.Known(Key("server_save_as_new")) {
		t.Fatal("flag server_save_as_new missing")
	}
	if _, ok := s.ForOperation("CreateServer"); ok {
		t.Fatal("server_save_as_new must not guard an operation")
	}
}

func TestDefaultHoldsTheBuildFlags(t *testing.T) {
	s := Default()
	// Frontend-only flags guard no operation (UI-019).
	if !s.Known(Key("log_filter_split")) {
		t.Fatal("log_filter_split missing")
	}
	for _, op := range []string{"ListServers", "GetServer"} {
		if name, ok := s.ForOperation(op); ok {
			t.Errorf("%s guarded by %s", op, name)
		}
	}
	// The flags of 1.2 are on by default, also without a store.
	if !s.Enabled(t.Context(), nil, "log_filter_split") {
		t.Error("flag off without a store")
	}
}

// TestDefaultsAndStoredValues covers NFR-019: each flag has its default,
// EnsureDefaults stores it, and a stored value wins over it.
func TestDefaultsAndStoredValues(t *testing.T) {
	ctx := t.Context()
	st := storetest.New(t)
	s, err := New(Flag{Name: "on_flag", On: true}, Flag{Name: "off_flag"}, Flag{Name: "kept_off", On: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefault(ctx, Key("kept_off"), false); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureDefaults(ctx, st); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"on_flag": true, "off_flag": false, "kept_off": false, "unknown": false} {
		if got := s.Enabled(ctx, st, name); got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	if got := s.EnabledNames(ctx, st); len(got) != 1 || got[0] != "on_flag" {
		t.Errorf("enabled names = %v, want [on_flag]", got)
	}
	// Every flag of this build is on by default.
	for _, f := range production {
		if !f.On {
			t.Errorf("flag %s is off by default", f.Name)
		}
	}
}
