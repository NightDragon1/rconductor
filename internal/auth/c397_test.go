// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"strings"
	"testing"
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
