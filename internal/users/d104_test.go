// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/NightDragon1/rconductor/internal/auth"
)

// TestD104OwnSecondFactorFirst: the RCON admin cannot require the second
// factor for everyone while he has none of his own; the refusal writes
// nothing, neither the value nor the version nor an audit entry. With his
// own second factor the switch goes through; a value that is already on is
// no switch and needs none.
func TestD104OwnSecondFactorFirst(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := auth.EnsureSecondFactorDefaults(ctx, f.st); err != nil {
		t.Fatal(err)
	}
	on := map[string]json.RawMessage{auth.SettingTOTPRequired: json.RawMessage(`true`)}
	before := f.version("settings", "")
	// Order of the contract: a stale version and a bad value come first.
	stale := *before - 1
	if _, err := f.svc.UpdateSettings(ctx, f.as("admin"), on, &stale); !errors.Is(err, ErrConflict) {
		t.Errorf("stale version: %v, want ErrConflict", err)
	}
	bad := map[string]json.RawMessage{auth.SettingTOTPRequired: json.RawMessage(`true`), "audit.retention_days": json.RawMessage(`"x"`)}
	var inv *InvalidError
	if _, err := f.svc.UpdateSettings(ctx, f.as("admin"), bad, before); !errors.As(err, &inv) {
		t.Errorf("bad value: %v, want InvalidError", err)
	}
	if _, err := f.svc.UpdateSettings(ctx, f.as("admin"), on, before); !errors.Is(err, ErrOwnTOTPRequired) {
		t.Fatalf("without an own second factor: %v, want ErrOwnTOTPRequired", err)
	}
	var stored bool
	if err := f.st.GlobalSetting(ctx, auth.SettingTOTPRequired, &stored); err != nil || stored {
		t.Errorf("stored %v, %v after the refusal", stored, err)
	}
	if after := f.version("settings", ""); *after != *before {
		t.Errorf("version moved from %d to %d", *before, *after)
	}
	if got := f.auditOf("settings.update"); len(got) != 0 {
		t.Errorf("audit after the refusal: %+v", got)
	}
	if _, err := f.st.DB.Exec(`UPDATE users SET totp_secret = x'00' WHERE id = ?`, f.ids["admin"]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateSettings(ctx, f.as("admin"), on, f.version("settings", "")); err != nil {
		t.Fatalf("with an own second factor: %v", err)
	}
	// Already on: the admin without a second factor sends it again with
	// another key; that is no switch of auth.totp_required.
	if _, err := f.st.DB.Exec(`UPDATE users SET totp_secret = NULL WHERE id = ?`, f.ids["admin"]); err != nil {
		t.Fatal(err)
	}
	again := map[string]json.RawMessage{auth.SettingTOTPRequired: json.RawMessage(`true`), "audit.retention_days": json.RawMessage(`120`)}
	if _, err := f.svc.UpdateSettings(ctx, f.as("admin"), again, f.version("settings", "")); err != nil {
		t.Errorf("value already on: %v", err)
	}
}
