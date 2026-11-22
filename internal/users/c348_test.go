// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// TestC348ServerDeleteAndAssignment: a server deleted around an assignment
// to a server admin never keeps a scope: the delete first refuses the
// assignment, the assignment first is taken away by the delete (ROLE-003).
// 100 runs, both orders.
func TestC348ServerDeleteAndAssignment(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	accepted, refused := 0, 0
	for i := range 100 {
		id := fmt.Sprintf("race-%d", i)
		f.exec(`INSERT INTO servers (id, name, created_at, updated_at) VALUES (?, ?, 't', 't')`, id, id)
		f.checker.Invalidate() // as a real create does
		// Both orders: even runs delete first, odd runs assign first.
		assign := func() error {
			scopes := []Target{{ServerID: id}}
			_, err := f.svc.UpdateAssignments(ctx, f.as("admin"), f.ids["sa"], AssignmentsUpdate{AdminScopes: &scopes})
			return err
		}
		done := make(chan error, 1)
		var err error
		if i%2 == 0 {
			done <- deleteServer(ctx, f, id)
			err = assign()
		} else {
			err = assign()
			done <- deleteServer(ctx, f, id)
		}
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrNotFound):
			refused++
		default:
			t.Fatalf("run %d: %v", i, err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if n := f.count(`SELECT count(*) FROM admin_scopes WHERE server_id = ?`, id); n != 0 {
			t.Fatalf("run %d: a scope on a deleted server", i)
		}
	}
	t.Logf("accepted %d, refused %d", accepted, refused)
}

// deleteServer deletes a server as the server service does: the row and
// its scopes in one transaction.
func deleteServer(ctx context.Context, f *fixture, id string) error {
	tx, err := f.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	now := store.Now()
	if err := q.SoftDeleteServer(ctx, sqlitedb.SoftDeleteServerParams{DeletedAt: sql.NullString{String: now, Valid: true}, UpdatedAt: now, ID: id}); err != nil {
		return err
	}
	if _, err := q.DeleteServerScopes(ctx, sql.NullString{String: id, Valid: true}); err != nil {
		return err
	}
	return tx.Commit()
}
