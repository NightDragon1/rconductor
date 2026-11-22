// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package perm

import (
	"context"
	"strings"
	"testing"
)

// TestC397BrokenSupporterSwitch (D-109, F-93): a supporter switch that
// cannot be read counts as off: supporters get no rights, and the failure
// is logged with the event code.
func TestC397BrokenSupporterSwitch(t *testing.T) {
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
	m := Actor{ID: w.ids["m"], Role: Moderator}
	if may, err := w.c.MayManageUsers(context.Background(), m); err != nil || may {
		t.Errorf("moderator manages supporters with a broken switch: %t, %v", may, err)
	}
	if managed, err := w.c.Managed(context.Background(), m); err != nil || managed[w.ids["sup"]] {
		t.Errorf("moderator manages the supporter with a broken switch: %v, %v", managed, err)
	}
	if !strings.Contains(w.log.String(), `"event":"setting.read_failed"`) {
		t.Errorf("broken switch not logged: %s", w.log.String())
	}
}
