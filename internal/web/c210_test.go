// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/buildinfo"
)

// TestC210AgentVersionLengthBeforeCut: a version longer than the bound is
// unknown also when a space sits at the cut; one of the bound is kept.
func TestC210AgentVersionLengthBeforeCut(t *testing.T) {
	long := strings.Repeat("1", maxAgentVersion) + " x"
	if got := agentVersion(long); got != buildinfo.Unknown {
		t.Errorf("overlong version with a space at the cut = %q, want unknown", got)
	}
	exact := strings.Repeat("1", maxAgentVersion)
	if got := agentVersion(" " + exact + " "); got != exact {
		t.Errorf("version of the bound = %q", got)
	}
}
