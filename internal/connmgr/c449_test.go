// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package connmgr

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/rcon/source"
)

// failingOne cannot read one server again; it lists that server first.
type failingOne struct {
	*fakeSource
	bad string
}

func (f failingOne) Targets(ctx context.Context) ([]Target, error) {
	all, err := f.fakeSource.Targets(ctx)
	slices.SortFunc(all, func(a, b Target) int {
		switch {
		case a.ID == f.bad:
			return -1
		case b.ID == f.bad:
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return all, err
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
	// The failing server comes first; s1 is still taken over.
	if err := f.m.Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "server bad") {
		t.Fatalf("the failure is not told: %v", err)
	}
	if got := w.current().EndMarker; got != source.EndMarkerNone {
		t.Errorf("end marker = %q, the change of s1 was not taken", got)
	}
}
