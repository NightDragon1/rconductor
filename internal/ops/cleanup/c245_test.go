// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package cleanup

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

// TestC245SettingReadsLogged (D-109, F-93): the retention of audit and
// messages and the idle timeout fall back to their defaults on a broken
// stored value, with a line setting.read_failed, never the value; nothing
// newer than the default retention is removed.
func TestC245SettingReadsLogged(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	keys := []string{users.SettingAuditRetentionDays, users.SettingMessagesRetentionDays, auth.SettingIdleTimeout}
	for _, key := range keys {
		if _, err := st.DB.ExecContext(ctx, `INSERT INTO settings (id, key, value, updated_at) VALUES (?, ?, '{"broken":4711}', 't')
			ON CONFLICT DO UPDATE SET value = excluded.value`, "c245-"+key, key); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.DB.ExecContext(ctx, `INSERT INTO audit_log (id, ts, kind, action, result) VALUES ('young', ?, 'admin', 'x', 'ok')`,
		at(now.AddDate(0, 0, -users.DefaultAuditRetentionDays+1))); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	res, err := Run(ctx, st, logging.New(&logs, "panel", config.Log{Format: "json"}).Module("audit"), now)
	if err != nil || res.Audit != 0 {
		t.Fatalf("run = %+v, %v", res, err)
	}
	logged := logs.String()
	for _, key := range keys {
		if !strings.Contains(logged, `"event":"setting.read_failed"`) || !strings.Contains(logged, key) {
			t.Errorf("no setting.read_failed for %s", key)
		}
	}
	if strings.Contains(logged, "4711") {
		t.Error("the broken value is in the log")
	}
}
