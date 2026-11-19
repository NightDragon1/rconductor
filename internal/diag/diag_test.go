// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

// TestExportSkeleton covers NFR-018 and D-090 as far as built: only the
// RCON admin, refusals audited; then the period, at most 7 days.
func TestExportSkeleton(t *testing.T) {
	st := storetest.New(t)
	s := NewService(st, nil)
	ctx := context.Background()
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	for _, role := range []perm.Role{perm.ServerAdmin, perm.Moderator, perm.Supporter, ""} {
		id := storetest.CreateUser(t, st, storetest.User{Pseudonym: "u" + string(role), Role: string(role)})
		req := users.Request{Actor: perm.Actor{ID: id, Role: role}}
		if err := s.Export(ctx, req, now.Add(time.Hour), now); !errors.Is(err, users.ErrForbidden) {
			t.Errorf("%q = %v, want forbidden before the period", role, err)
		}
	}
	var n int
	_ = st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'diagnostics.exported' AND result = 'denied'`).Scan(&n)
	if n != 4 {
		t.Errorf("audited refusals = %d", n)
	}
	admin := users.Request{Actor: perm.Actor{ID: storetest.CreateUser(t, st, storetest.User{Pseudonym: "a", Role: string(perm.RCONAdmin)}), Role: perm.RCONAdmin}}
	var inv *users.InvalidError
	for _, p := range [][2]time.Time{{now, now}, {now, now.Add(-time.Hour)}, {now, now.Add(MaxPeriod + time.Second)}} {
		if err := s.Export(ctx, admin, p[0], p[1]); !errors.As(err, &inv) {
			t.Errorf("period %v = %v, want invalid", p, err)
		}
	}
	if err := s.Export(ctx, admin, now, now.Add(MaxPeriod)); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("7 days = %v", err)
	}
}
