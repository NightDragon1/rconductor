// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/users"
)

func sayPrefixWorld(t *testing.T) (*fixture, string) {
	t.Helper()
	f := newFixture(t)
	a, b := f.create("a"), f.create("b")
	g, err := f.svc.CreateGroup(context.Background(), f.as("admin"), "mc", []string{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO admin_scopes (id, user_id, server_group_id, assigned_by, created_at) VALUES ('c222', ?, ?, ?, 't')`, f.ids["sa"], g.ID, f.ids["admin"])
	f.c.Invalidate()
	return f, g.ID
}

func groupVersion(t *testing.T, f *fixture, id string) *int64 {
	t.Helper()
	var v int64
	if err := f.st.DB.QueryRow(`SELECT version FROM server_groups WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return &v
}

// TestC222ScopeGoneBeforeTheWrite: a scope removed after the first check
// and before the write refuses the prefix inside the transaction; the
// refusal is audited once and does not wait on the lock of the transaction
// (CMD-020, ROLE-003).
func TestC222ScopeGoneBeforeTheWrite(t *testing.T) {
	f, id := sayPrefixWorld(t)
	ctx := context.Background()
	if _, err := f.svc.SayPrefix(ctx, f.as("sa"), id); err != nil {
		t.Fatal(err) // the rights of sa are in the cache now
	}
	f.svc.beforeWrite = func() { f.exec(`DELETE FROM admin_scopes WHERE id = 'c222'`) }
	start := time.Now()
	_, err := f.svc.SetSayPrefix(ctx, f.as("sa"), id, "[S]", true, groupVersion(t, f, id))
	if !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("scope gone: %v, want not found", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("refusal took %s", d)
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE action = ? AND result = 'denied' AND reason_code = 'not_found'`, actionSayPrefixUpdate); n != 1 {
		t.Errorf("audited refusals = %d, want 1", n)
	}
	if p, _ := f.svc.SayPrefix(ctx, f.as("admin"), id); p != "" {
		t.Errorf("prefix written: %q", p)
	}
}

// TestC222ParallelWritesNoStarvation: many parallel writes of the prefix
// finish quickly, each a success or a conflict of the version; the check
// inside the transaction needs no second connection.
func TestC222ParallelWritesNoStarvation(t *testing.T) {
	f, id := sayPrefixWorld(t)
	ctx := context.Background()
	v := groupVersion(t, f, id)
	start := time.Now()
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() {
			_, err := f.svc.SetSayPrefix(ctx, f.as("sa"), id, fmt.Sprintf("[%d]", i), true, v)
			if err != nil && !errors.Is(err, users.ErrConflict) {
				t.Errorf("write %d: %v", i, err)
			}
		})
	}
	wg.Wait()
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("40 parallel writes took %s", d)
	}
}
