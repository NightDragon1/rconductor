// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestC348ParallelServerDelete: a server deleted while the RCON admin
// assigns it to a server admin never ends up assigned: the target is
// checked in the writing transaction (ROLE-003). 100 runs.
func TestC348ParallelServerDelete(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	for i := range 100 {
		id := fmt.Sprintf("race-%d", i)
		f.exec(`INSERT INTO servers (id, name, created_at, updated_at) VALUES (?, ?, 't', 't')`, id, id)
		done := make(chan error, 1)
		go func() {
			_, err := f.st.DB.ExecContext(ctx, `UPDATE servers SET deleted_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, id)
			done <- err
		}()
		scopes := []Target{{ServerID: id}}
		_, err := f.svc.UpdateAssignments(ctx, f.as("admin"), f.ids["sa"], AssignmentsUpdate{AdminScopes: &scopes})
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatalf("run %d: %v", i, err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		// Written at all, the assignment came before the delete.
		if n := f.count(`SELECT count(*) FROM admin_scopes a JOIN servers s ON s.id = a.server_id WHERE s.id = ? AND a.created_at > s.deleted_at`, id); n != 0 {
			t.Fatalf("run %d: an assignment written after the delete", i)
		}
		if err == nil && f.count(`SELECT count(*) FROM admin_scopes WHERE server_id = ?`, id) != 1 {
			t.Fatalf("run %d: accepted but not written", i)
		}
	}
}
