// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/users"
)

// TestC245SettingReadsLogged (D-109, F-93): parallel HTTP requests and the
// connection tests per minute fall back to their defaults on a broken
// stored value, with a line setting.read_failed, never the value.
func TestC245SettingReadsLogged(t *testing.T) {
	f := newFixture(t)
	for _, key := range []string{users.SettingHTTPParallel, users.SettingConnectionTests} {
		if _, err := f.st.DB.Exec(`INSERT INTO settings (id, key, value, updated_at) VALUES (?, ?, '"broken-4711"', 't')
			ON CONFLICT DO UPDATE SET value = excluded.value`, "c245-"+key, key); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.svc.httpParallel(); n != users.DefaultHTTPParallel {
		t.Errorf("http parallel = %d", n)
	}
	if n := f.svc.testLimit(context.Background()); n != users.DefaultConnectionTests {
		t.Errorf("connection tests = %d", n)
	}
	logged := f.logs.String()
	for _, key := range []string{users.SettingHTTPParallel, users.SettingConnectionTests} {
		if !strings.Contains(logged, `"event":"setting.read_failed"`) || !strings.Contains(logged, key) {
			t.Errorf("no setting.read_failed for %s", key)
		}
	}
	if strings.Contains(logged, "4711") {
		t.Error("the broken value is in the log")
	}
}
