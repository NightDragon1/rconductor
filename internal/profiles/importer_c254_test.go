// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"context"
	"testing"
)

// TestC254RejectedProfileNotTold (D-088 b): the receiver of stored
// profiles is told for each stored profile, never for a rejected change,
// so the masking keeps the patterns as last stored.
func TestC254RejectedProfileNotTold(t *testing.T) {
	f := newImportFixture(t)
	ctx := context.Background()
	told := 0
	f.im.OnImported(func() { told++ })
	f.write(t, "ark.yaml", custom("ark", "ARK"))
	if err := f.im.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	stored := told
	if stored == 0 {
		t.Fatal("a stored profile was not told")
	}
	f.write(t, "ark.yaml", "schema: 1\nid: ark\nname: ARK broken\n") // protocol missing
	if err := f.im.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if told != stored {
		t.Errorf("rejected change told %d times", told-stored)
	}
	f.write(t, "ark.yaml", custom("ark", "ARK fixed"))
	if err := f.im.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if told != stored+1 {
		t.Errorf("fixed profile told %d times, want once", told-stored)
	}
}
