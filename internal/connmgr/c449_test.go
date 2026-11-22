// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package connmgr

import (
	"context"
	"errors"
	"testing"

	"github.com/NightDragon1/rconductor/internal/rcon/source"
)

// failingOne cannot read one server again.
type failingOne struct {
	*fakeSource
	bad string
}

func (f failingOne) Target(ctx context.Context, id string) (Target, bool, error) {
	if id == f.bad {
		return Target{}, false, errors.New("read failed")
	}
	return f.fakeSource.Target(ctx, id)
}

// TestC449RefreshGoesOnAfterAFailure (PROT-001): a server that cannot be
// read again does not keep the other servers on their old setup; Refresh
// tells the error after all were tried, whatever the order.
func TestC449RefreshGoesOnAfterAFailure(t *testing.T) {
	f := newFixture(t, "secret", func(cmd string) string { return cmd })
	f.waitStatus(t, Connected)
	f.m.mu.Lock()
	w := f.m.workers["s1"]
	f.m.mu.Unlock()
	changed := w.current()
	changed.EndMarker = source.EndMarkerNone
	f.src.set(changed)
	broken := changed
	broken.ID = "bad"
	f.src.set(broken)
	f.m.source = failingOne{fakeSource: f.src, bad: "bad"}
	for range 5 { // map order: the failing server before and after s1
		if err := f.m.Refresh(context.Background()); err == nil {
			t.Fatal("the failure is not told")
		}
	}
	if got := w.current().EndMarker; got != source.EndMarkerNone {
		t.Errorf("end marker = %q, the change of s1 was not taken", got)
	}
}
