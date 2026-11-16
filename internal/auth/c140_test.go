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

// TestC140N1RequiredFailsClosed covers AUTH-008: while the setting
// auth.totp_required cannot be read, the second factor counts as required:
// a session without one only sets it up, also on the WebSocket check, the
// factor cannot be switched off, and the fault is logged. A missing setting
// means off.
func TestC140N1RequiredFailsClosed(t *testing.T) {
	f := newSFFixture(t)
	f.user(t, storetest.User{Pseudonym: "kiwi"})
	ctx := context.Background()
	f.set(t, SettingTOTPRequired, "not json")
	_, p, err := f.svc.Login(ctx, "kiwi", testPassword, client)
	if err != nil || !p.TOTPSetupRequired {
		t.Fatalf("login with an unreadable setting = %+v %v", p, err)
	}
	if f.svc.SessionValid(ctx, p.SessionHash) {
		t.Error("session valid for the WebSocket while the setting is unreadable")
	}
	if !strings.Contains(f.logs.String(), "second factor setting not readable") {
		t.Error("fault not logged")
	}
	// A user with the factor on cannot switch it off meanwhile.
	f.user(t, storetest.User{Pseudonym: "lime"})
	f.set(t, SettingTOTPRequired, "false")
	q, _, err := f.login(t, "lime", client)
	if err != nil {
		t.Fatal(err)
	}
	key := f.enable(t, "lime")
	f.set(t, SettingTOTPRequired, "not json")
	if err := f.svc.DisableTOTP(ctx, q, client, testPassword, f.code(key)); !errors.Is(err, ErrTOTPRequired) {
		t.Errorf("switch off while the setting is unreadable: %v", err)
	}
	if _, err := f.st.DB.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, SettingTOTPRequired); err != nil {
		t.Fatal(err)
	}
	if f.svc.TOTPRequired(ctx) {
		t.Error("a missing setting counts as required")
	}
}
