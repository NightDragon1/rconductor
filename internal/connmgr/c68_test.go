// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package connmgr

import (
	"context"
	"testing"

	"github.com/NightDragon1/rconductor/internal/rcon/source"
)

// TestC68RefreshTakesChangedTargets: after a profile change Refresh hands
// a changed connection (here the end marker) to its worker, which then
// reconnects; an unchanged server is left alone (PROT-001).
func TestC68RefreshTakesChangedTargets(t *testing.T) {
	f := newFixture(t, "secret", func(cmd string) string { return cmd })
	f.waitStatus(t, Connected)
	f.m.mu.Lock()
	w := f.m.workers["s1"]
	f.m.mu.Unlock()
	ctx := context.Background()

	if err := f.m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if len(w.reload) != 0 {
		t.Error("an unchanged server was told to reconnect")
	}

	changed := w.current()
	changed.EndMarker = source.EndMarkerNone
	f.src.set(changed)
	if err := f.m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if got := w.current().EndMarker; got != source.EndMarkerNone {
		t.Errorf("end marker = %q, want %q", got, source.EndMarkerNone)
	}
	f.waitStatus(t, Connected)
}
