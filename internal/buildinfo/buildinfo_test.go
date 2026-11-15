// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package buildinfo

import (
	"slices"
	"testing"
)

// Without ldflags the build reports its version as 0.0.0-dev and commit and
// build date as "unknown" (AGT-016).
func TestDefaults(t *testing.T) {
	if Version != "0.0.0-dev" || Commit != "unknown" || BuildDate != "unknown" {
		t.Fatalf("Version, Commit, BuildDate = %q, %q, %q", Version, Commit, BuildDate)
	}
	if Major() != 0 {
		t.Fatalf("Major() = %d, want 0", Major())
	}
}

// The banner has the fixed wording of AGT-016.
func TestBanner(t *testing.T) {
	v, c, d := Version, Commit, BuildDate
	t.Cleanup(func() { Version, Commit, BuildDate = v, c, d })
	Version, Commit, BuildDate = "1.2", "1b04aa2", "2026-11-15T08:00:00Z"
	want := []string{
		"RCONductor Agent 1.2 (1b04aa2, 2026-11-15T08:00:00Z)",
		"Copyright (C) 2026 NightDragon · ShadowGhost.NET",
		"Licensed under AGPL-3.0-or-later. This program comes with ABSOLUTELY NO WARRANTY.",
		"For more see https://github.com/NightDragon1/rconductor",
	}
	if got := Banner("Agent"); !slices.Equal(got, want) {
		t.Fatalf("Banner = %q, want %q", got, want)
	}
}

func TestMajor(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	cases := map[string]int{
		"1.0":       1,
		"0.9":       0,
		"v2.1":      2,
		"1.0-rc1":   1,
		"10.1-rc1":  10,
		"0.0.0-dev": 0,
		"1.2.3":     1,
		"dev":       -1,
		"":          -1,
	}
	for in, want := range cases {
		Version = in
		if got := Major(); got != want {
			t.Errorf("Major(%q) = %d, want %d", in, got, want)
		}
	}
}
