// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package perm

import (
	"strings"
	"testing"
)

// TestC260BrokenSupporterSwitch (D-109, F-93): a supporter switch that
// cannot be read counts as off: supporters get no rights, and the failure
// is logged with the event code.
func TestC260BrokenSupporterSwitch(t *testing.T) {
	w := newWorld(t)
	w.change(`UPDATE users SET supporters_allowed = 1 WHERE id = ?`, w.ids["m"])
	w.change(`UPDATE settings SET value = 'true' WHERE key = ?`, SettingSupportersEnabled)
	if got := w.rights("sup").ServerIDs(); len(got) == 0 {
		t.Fatal("no supporter rights with the switch on")
	}
	w.change(`UPDATE settings SET value = '"yes' WHERE key = ?`, SettingSupportersEnabled)
	if got := w.rights("sup").ServerIDs(); len(got) != 0 {
		t.Errorf("supporter rights with a broken switch: %v", got)
	}
	if !strings.Contains(w.log.String(), `"event":"setting.read_failed"`) {
		t.Errorf("broken switch not logged: %s", w.log.String())
	}
}
