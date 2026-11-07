// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestB11_1GroupStaleRead (same pattern as in servers): UpdateGroup reads
// the group in the transaction after the version check. A list change sent
// with a predicted version while a concurrent rename commits never writes
// the old name back, and its audit names what it changed.
func TestB11_1GroupStaleRead(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	sa := f.as("sa")
	for i := range 50 {
		g, err := f.svc.CreateGroup(ctx, sa, fmt.Sprintf("g%d", i), KindModerator, "l1")
		if err != nil {
			t.Fatal(err)
		}
		v := *f.version("user_groups", g.ID)
		name, none := fmt.Sprintf("renamed%d", i), ""
		var wg sync.WaitGroup
		var errA, errB error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, errA = f.svc.UpdateGroup(ctx, sa, g.ID, GroupUpdate{Name: &name, Version: &v})
		}()
		go func() {
			defer wg.Done()
			for range 200 { // too early is a harmless conflict
				for next := v + 1; next <= v+2; next++ {
					if _, errB = f.svc.UpdateGroup(ctx, sa, g.ID, GroupUpdate{CommandListID: &none, Version: &next}); errB == nil {
						return
					}
				}
			}
		}()
		wg.Wait()
		if errA != nil || errB != nil {
			continue
		}
		got, err := f.svc.Group(ctx, sa, g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != name || got.CommandListID != "" {
			t.Fatalf("round %d: name %q, list %q after a rename and a list change", i, got.Name, got.CommandListID)
		}
	}
}
