// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"strings"
	"testing"
)

// TestC245SettingReadsLogged (D-109, F-93): invitation lifetime and the
// login protection of the profile fall back to their defaults on a broken
// stored value, with a line setting.read_failed, never the value.
func TestC245SettingReadsLogged(t *testing.T) {
	pf := newProfiles(t)
	iv := newInvites(t, pf.fixture)
	keys := []string{SettingInviteLifetime, "login.max_failures", "login.lockout_duration"}
	for _, key := range keys {
		if _, err := pf.st.DB.Exec(`INSERT INTO settings (id, key, value, updated_at) VALUES (?, ?, '{"broken":4711}', 't')
			ON CONFLICT DO UPDATE SET value = excluded.value`, "c245-"+key, key); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if d := iv.lifetime(ctx); d != DefaultInviteLifetime {
		t.Errorf("invite lifetime = %v", d)
	}
	if n := pf.p.maxTries(ctx); n != 5 {
		t.Errorf("max tries = %d", n)
	}
	pf.p.triesWindow(ctx)
	logged := pf.logs.String()
	for _, key := range keys {
		if !strings.Contains(logged, `"event":"setting.read_failed"`) || !strings.Contains(logged, key) {
			t.Errorf("no setting.read_failed for %s", key)
		}
	}
	if strings.Contains(logged, "4711") {
		t.Error("the broken value is in the log")
	}
}
