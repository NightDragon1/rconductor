// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/perm"
)

// TestC377ParallelSupporterGrants: more parallel writes of supporter
// grants than the pool has connections finish quickly; the transaction
// reads the switch and the moderator through itself and needs no second
// connection (ROLE-006, ROLE-003).
func TestC377ParallelSupporterGrants(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	f.exec(`UPDATE settings SET value = 'true' WHERE key = ?`, perm.SettingSupportersEnabled)
	f.exec(`UPDATE users SET supporters_allowed = 1 WHERE id = ?`, f.ids["m"])
	f.checker.Invalidate()
	want := []SupporterGrant{{ModeratorID: f.ids["m"], ServerID: "s1"}}
	start := time.Now()
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() {
			if _, err := f.svc.UpdateAssignments(ctx, f.as("m"), f.ids["sup"], AssignmentsUpdate{SupporterGrants: &want}); err != nil {
				t.Errorf("write %d: %v", i, err)
			}
		})
	}
	wg.Wait()
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("40 parallel writes took %s", d)
	}
	if n := f.count(`SELECT count(*) FROM supporter_grants WHERE supporter_id = ?`, f.ids["sup"]); n != 1 {
		t.Errorf("grants = %d, want 1", n)
	}
}
