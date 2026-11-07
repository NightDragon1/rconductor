// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestB11_1GroupStaleRead (blind round B11, finding 1, same pattern as in
// servers): UpdateGroup read the group outside the transaction and wrote
// its name back with a list change. A list change sent with a predicted
// version that read the group before a concurrent rename committed wrote
// the old name back, and its audit named only the list. Now the group is
// read in the transaction after the version check. Fails on a416454.
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
