// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"encoding/json"
	"testing"

	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/perm"
)

// TestC75BoolSettingsTakeOnlyTrueOrFalse: every on/off setting takes
// exactly true or false; null, strings, numbers and objects are refused,
// so none is stored as false.
func TestC75BoolSettingsTakeOnlyTrueOrFalse(t *testing.T) {
	keys := []string{auth.SettingTOTPRequired, perm.SettingSupportersEnabled, SettingFingerprintPairing, SettingShortcuts}
	for _, key := range keys {
		rule := settingRules[key]
		if rule == nil {
			t.Fatalf("%s has no rule", key)
		}
		for raw, want := range map[string]any{"true": true, "false": false} {
			if v, err := rule(json.RawMessage(raw)); err != nil || v != want {
				t.Errorf("%s %s = %v, %v", key, raw, v, err)
			}
		}
		for _, raw := range []string{"null", `"true"`, `"false"`, "0", "1", "{}", "[]", ""} {
			if v, err := rule(json.RawMessage(raw)); err == nil {
				t.Errorf("%s %s = %v, want refused", key, raw, v)
			}
		}
	}
}
