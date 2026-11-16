// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestC142B3StricterSettingsKeptWhenUnreadable: session and login limits
// set stricter than the default keep their last value when the database
// cannot be read; the capacity of the failure table stays at its default
// (F-93, D-077).
func TestC142B3StricterSettingsKeptWhenUnreadable(t *testing.T) {
	f := newSessionFixture(t, false)
	ctx := context.Background()
	for key, v := range map[string]string{
		SettingIdleTimeout: `"30m0s"`, SettingMaxLifetime: `"2h0m0s"`, SettingMaxFailures: "2",
		SettingMaxClientFailures: "7", SettingLockout: `"2h0m0s"`, SettingMaxTracked: "5000",
		SettingSecondFactorWindow: `"2m0s"`, SettingRecoveryPerHour: "1",
	} {
		if _, err := f.st.EnsureDefault(ctx, key, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := f.st.DB.Exec(`UPDATE settings SET value = ? WHERE key = ?`, v, key); err != nil {
			t.Fatal(err)
		}
	}
	check := func(when string, wantTracked int) {
		t.Helper()
		idle, life := f.svc.policy(ctx)
		lim := f.svc.loginPolicy(ctx)
		window := f.svc.durationSetting(ctx, SettingSecondFactorWindow, DefaultSecondFactorWindow, MinSecondFactorWindow)
		if idle != 30*time.Minute || life != 2*time.Hour || lim.maxFailures != 2 || lim.maxClientFailures != 7 ||
			lim.lockout != 2*time.Hour || window != 2*time.Minute || f.svc.recoveryLimit(ctx) != 1 {
			t.Errorf("%s: idle %s, life %s, %+v, window %s, recovery %d", when, idle, life, lim, window, f.svc.recoveryLimit(ctx))
		}
		if lim.maxTracked != wantTracked {
			t.Errorf("%s: max tracked %d, want %d", when, lim.maxTracked, wantTracked)
		}
	}
	check("readable", 5000)
	if err := f.st.DB.Close(); err != nil {
		t.Fatal(err)
	}
	check("unreadable", DefaultMaxTracked)
	if !strings.Contains(f.logs.String(), `"event":"setting.read_failed"`) {
		t.Error("no setting.read_failed in the log")
	}
}
