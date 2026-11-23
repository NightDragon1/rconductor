// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// TestC397TOTPSwitchFaultEvent (D-109, F-93): a second-factor switch that
// is neither true nor false counts as required and is logged with the
// event code, never with its value.
func TestC397TOTPSwitchFaultEvent(t *testing.T) {
	f := newSFFixture(t)
	f.set(t, SettingTOTPRequired, `"maybe-7654"`)
	if !f.svc.TOTPRequired(context.Background()) {
		t.Error("an invalid switch does not count as required")
	}
	logged := f.logs.String()
	if !strings.Contains(logged, `"event":"setting.read_failed"`) || strings.Contains(logged, "7654") {
		t.Errorf("log = %s", logged)
	}
}

// TestC444TOTPSwitchFaultLoggedOnce (D-109): a broken second-factor switch
// is logged once while it stays broken, and again after a good read.
func TestC444TOTPSwitchFaultLoggedOnce(t *testing.T) {
	f := newSFFixture(t)
	ctx := context.Background()
	count := func() int { return strings.Count(f.logs.String(), "second factor setting not readable") }
	f.set(t, SettingTOTPRequired, `"maybe"`)
	for range 3 {
		if !f.svc.TOTPRequired(ctx) {
			t.Fatal("an invalid switch does not count as required")
		}
	}
	if count() != 1 {
		t.Fatalf("logged %d times, want once", count())
	}
	f.set(t, SettingTOTPRequired, `false`)
	if f.svc.TOTPRequired(ctx) {
		t.Fatal("false counts as required")
	}
	f.set(t, SettingTOTPRequired, `"maybe"`)
	f.svc.TOTPRequired(ctx)
	if count() != 2 {
		t.Errorf("logged %d times in all, want 2 after a good read", count())
	}
}

// TestC411DutyCheckedInTheTransaction (AUTH-008): switching the own second
// factor off is refused inside the writing transaction when the duty is
// on, also if it was switched on after the check before.
func TestC411DutyCheckedInTheTransaction(t *testing.T) {
	f := newSFFixture(t)
	ctx := context.Background()
	id := f.user(t, storetest.User{Pseudonym: "kiwi"})
	f.enable(t, "kiwi")
	f.set(t, SettingTOTPRequired, `true`)
	if err := f.svc.clearTOTP(ctx, id, "kiwi", id, "rcon_admin", "", "r1", "self", true); !errors.Is(err, ErrTOTPRequired) {
		t.Fatalf("clear under duty = %v", err)
	}
	var secret []byte
	_ = f.st.DB.QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, id).Scan(&secret)
	if len(secret) == 0 {
		t.Error("the second factor was switched off under the duty")
	}
	// An administrator's reset does not depend on the duty.
	if err := f.svc.clearTOTP(ctx, id, "kiwi", id, "rcon_admin", "", "r2", "admin_reset", false); err != nil {
		t.Errorf("admin reset: %v", err)
	}
}
