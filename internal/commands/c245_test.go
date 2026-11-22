// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/commands/limits"
	"github.com/NightDragon1/rconductor/internal/users"
)

// TestC245SettingReadsLogged (D-109, F-93): the command pipeline reads its
// limits with a log: a broken stored value gives the default and a line
// setting.read_failed with the key, never the value.
func TestC245SettingReadsLogged(t *testing.T) {
	w := newWorld(t)
	for _, key := range []string{users.SettingMultiParallel, users.SettingTemplatesPerServer, "ratelimit.moderator.burst"} {
		w.exec(`INSERT INTO settings (id, key, value, updated_at) VALUES (?, ?, '"broken-4711"', 't')
			ON CONFLICT DO UPDATE SET value = excluded.value`, "c245-"+key, key)
	}
	ctx := context.Background()
	if n := w.svc.multiSetting(ctx, users.SettingMultiParallel, limits.DefaultMultiParallel, limits.MinMultiParallel, limits.MaxMultiParallel); n != limits.DefaultMultiParallel {
		t.Errorf("multi parallel = %d", n)
	}
	if n := w.svc.templatesPerServer(ctx); n != limits.DefaultTemplatesPerServer {
		t.Errorf("templates per server = %d", n)
	}
	if lim := w.svc.limit(ctx, "moderator"); lim != DefaultLimits["moderator"] {
		t.Errorf("rate limit = %+v", lim)
	}
	logged := w.logs.String()
	for _, key := range []string{users.SettingMultiParallel, users.SettingTemplatesPerServer, "ratelimit.moderator.burst"} {
		if !strings.Contains(logged, `"event":"setting.read_failed"`) || !strings.Contains(logged, key) {
			t.Errorf("no setting.read_failed for %s", key)
		}
	}
	if strings.Contains(logged, "4711") {
		t.Error("the broken value is in the log")
	}
}
