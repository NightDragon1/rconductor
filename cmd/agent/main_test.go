// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/buildinfo"
)

// "version" and "--version" print the banner of AGT-016; anything else
// starts the agent.
func TestPrintVersion(t *testing.T) {
	want := strings.Join(buildinfo.Banner("Agent"), "\n") + "\n"
	for _, args := range [][]string{{"version"}, {"--version"}} {
		var b strings.Builder
		if !printVersion(args, &b) || b.String() != want {
			t.Errorf("printVersion(%q) wrote %q, want %q", args, b.String(), want)
		}
	}
	for _, args := range [][]string{nil, {"-version"}, {"version", "x"}, {"run"}} {
		var b strings.Builder
		if printVersion(args, &b) || b.Len() != 0 {
			t.Errorf("printVersion(%q) handled the call", args)
		}
	}
}
