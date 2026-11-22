// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package store_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// TestC397EndedContextNoLog (D-109): a request that ended is no fault of
// the database: neither LoggedSetting nor GuardedSetting on a broken row
// logs it.
func TestC397EndedContextNoLog(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	var out bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&out, nil))
	if _, err := st.EnsureDefault(ctx, "supporters.enabled", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefault(ctx, "login.max_failures", 3); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`UPDATE settings SET value = '"broken' WHERE key = 'login.max_failures'`); err != nil {
		t.Fatal(err)
	}
	ended, cancel := context.WithCancel(ctx)
	cancel()
	var on bool
	var n int
	_ = st.LoggedSetting(ended, log, "supporters.enabled", &on)
	_ = st.GuardedSetting(ended, log, "login.max_failures", &n)
	if out.Len() != 0 {
		t.Errorf("logged for an ended request: %s", out.String())
	}
}
