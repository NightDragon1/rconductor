// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

// TestC255SupporterSessionsEnd (ROLE-006, AUTH-006): deleting a moderator
// ends the sessions of the supporters he passed commands on to.
func TestC255SupporterSessionsEnd(t *testing.T) {
	f := newFixture(t)
	sup := f.ids["sup"]
	f.exec(`INSERT INTO supporter_grants (id, supporter_id, moderator_id, server_id, granted_by, created_at) VALUES ('sg', ?, ?, 's1', ?, 't')`, sup, f.ids["m"], f.ids["m"])
	if err := f.svc.Delete(context.Background(), f.as("admin"), f.ids["m"]); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.revoked, sup) {
		t.Errorf("supporter sessions not ended: revoked %v", f.revoked)
	}
	if n := f.count(`SELECT count(*) FROM supporter_grants WHERE id = 'sg'`); n != 0 {
		t.Error("supporter grant left")
	}
}

// TestC253PreconditionBeforeInvalid (AUD-003): a settings write without
// version is 428 before its values are checked, as the contract orders
// 403, 428, 400.
func TestC253PreconditionBeforeInvalid(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.UpdateSettings(context.Background(), f.as("admin"), map[string]json.RawMessage{"no.such.key": json.RawMessage(`1`)}, nil)
	if !errors.Is(err, ErrPreconditionRequired) {
		t.Errorf("no version and a bad key = %v, want 428", err)
	}
}
