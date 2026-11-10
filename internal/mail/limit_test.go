// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"context"
	"strconv"
	"testing"
	"time"
)

// TestTestMailFullTableKeepsCountsD076: a full table of test mail windows
// is never cleared as a whole: windows that still count stay, a new user is
// refused while it is full, and room comes back as windows end (NFR-017,
// D-076).
func TestTestMailFullTableKeepsCountsD076(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	f.svc.now = func() time.Time { return now }
	for i := range maxTesters - 1 {
		if !f.svc.take(ctx, "u"+strconv.Itoa(i)) {
			t.Fatal("refused while not full")
		}
	}
	for range DefaultTestPerMinute {
		if !f.svc.take(ctx, "spent") {
			t.Fatal("refused within the limit")
		}
	}
	now = now.Add(10 * time.Second)
	if f.svc.take(ctx, "new") {
		t.Error("new user on a full table was let through")
	}
	if f.svc.take(ctx, "spent") {
		t.Error("spent window lost its count on a full table")
	}
	now = now.Add(time.Minute)
	if !f.svc.take(ctx, "new") {
		t.Error("no room after the windows ended")
	}
	if !f.svc.take(ctx, "spent") {
		t.Error("spent window did not start a new minute")
	}
}
