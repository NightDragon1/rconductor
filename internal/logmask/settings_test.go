// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"context"
	"errors"
	"testing"

	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

func settingsRequest(t *testing.T, s *Settings, role perm.Role) users.Request {
	t.Helper()
	stored := role
	if role != perm.RCONAdmin && role != perm.ServerAdmin && role != perm.Moderator && role != perm.Supporter {
		stored = perm.Supporter // a session whose role is not known, on a stored user
	}
	id := storetest.CreateUser(t, s.st, storetest.User{Pseudonym: "u" + string(role), Role: string(stored)})
	return users.Request{Actor: perm.Actor{ID: id, Role: role}, Pseudonym: string(role)}
}

// TestSettingsRoles: only the RCON admin reads or changes the masking
// (UI-022); every other role is refused before the input is looked at,
// and each refusal is in the audit.
func TestSettingsRoles(t *testing.T) {
	s := NewSettings(storetest.New(t), nil)
	ctx := context.Background()
	bad := SettingsInput{SetTexts: true, Texts: []Text{{Value: "x"}}}
	if err := s.Get(ctx, settingsRequest(t, s, perm.RCONAdmin)); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("admin get: %v", err)
	}
	for _, role := range []perm.Role{perm.ServerAdmin, perm.Moderator, perm.Supporter, "", "unknown"} {
		req := settingsRequest(t, s, role)
		if err := s.Get(ctx, req); !errors.Is(err, users.ErrForbidden) {
			t.Errorf("%q get: %v", role, err)
		}
		if err := s.Update(ctx, req, bad); !errors.Is(err, users.ErrForbidden) {
			t.Errorf("%q update: %v", role, err)
		}
	}
	for action, want := range map[string]int{"log_masking.read": 5, "log_masking.updated": 5} {
		var n int
		if err := s.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = ? AND result = 'denied' AND target_id = 'log_masking'`, action).Scan(&n); err != nil || n != want {
			t.Errorf("%s refusals = %d (%v), want %d", action, n, err, want)
		}
	}
}
