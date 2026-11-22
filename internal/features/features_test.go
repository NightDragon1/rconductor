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

// TestDefaultHasNoFlags: the flags of 1.2 left with 1.3; their keys are
// unknown settings again and guard no operation.
func TestDefaultHasNoFlags(t *testing.T) {
	s := Default()
	for _, name := range []string{"messages", "server_save_as_new", "diagnostics_export"} {
		if s.Known(Key(name)) || s.Enabled(t.Context(), nil, name) {
			t.Errorf("flag %s still in the build", name)
		}
	}
	for _, op := range []string{"ListMessages", "GetServerProfile", "GetDiagnostics"} {
		if name, ok := s.ForOperation(op); ok {
			t.Errorf("%s guarded by %s", op, name)
		}
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
}
