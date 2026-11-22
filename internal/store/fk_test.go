// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package store_test

import (
	"errors"
	"testing"

	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// TestC288IsForeignKey: a row that refers to a missing one is told by the
// error code of SQLite, not by its text; other errors are not.
func TestC288IsForeignKey(t *testing.T) {
	st := storetest.New(t)
	_, err := st.DB.Exec(`INSERT INTO message_comments (id, message_id, author_pseudonym, text, created_at) VALUES ('c', 'missing', 'a', 'x', 't')`)
	if err == nil || !store.IsForeignKey(err) {
		t.Fatalf("missing message: %v, foreign key %t", err, store.IsForeignKey(err))
	}
	_, err = st.DB.Exec(`INSERT INTO no_such_table VALUES (1)`)
	if err == nil || store.IsForeignKey(err) || store.IsForeignKey(errors.New("FOREIGN KEY constraint failed")) {
		t.Errorf("other errors count as foreign key: %v", err)
	}
}
