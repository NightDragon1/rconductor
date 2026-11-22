// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"context"
	"strings"
	"testing"
)

// TestC245SettingReadsLogged (D-109, F-93): timeout and test limit of the
// mail service fall back to their defaults on a broken stored value, with
// a line setting.read_failed that names the key, never the value.
func TestC245SettingReadsLogged(t *testing.T) {
	f := newFixture(t)
	f.svc.timeout = 0 // read the setting, not the test value
	for _, key := range []string{SettingTimeout, SettingTestPerMinute} {
		if _, err := f.st.DB.Exec(`INSERT INTO settings (id, key, value, updated_at) VALUES (?, ?, '{"broken":4711}', 't')
			ON CONFLICT DO UPDATE SET value = excluded.value`, "c245-"+key, key); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if d := f.svc.duration(ctx); d != DefaultTimeout {
		t.Errorf("timeout = %v", d)
	}
	f.svc.take(ctx, "u")
	logged := f.logs.String()
	for _, key := range []string{SettingTimeout, SettingTestPerMinute} {
		if !strings.Contains(logged, `"event":"setting.read_failed"`) || !strings.Contains(logged, key) {
			t.Errorf("no setting.read_failed for %s", key)
		}
	}
	if strings.Contains(logged, "4711") {
		t.Error("the broken value is in the log")
	}
}
