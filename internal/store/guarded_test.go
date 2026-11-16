// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// TestC142B3GuardedSettingKeepsLastValue: a guarded setting that cannot
// be read keeps the value read last, logged with the key only; without a
// value read before it is an error, so the caller takes its default; a
// missing row is an error without a log (F-93, D-077).
func TestC142B3GuardedSettingKeepsLastValue(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	var out bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&out, nil))
	var n int
	if err := st.GuardedSetting(ctx, log, "login.max_failures", &n); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing = %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("missing row logged: %s", out.String())
	}
	if _, err := st.EnsureDefault(ctx, "login.max_failures", 3); err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefault(ctx, "login.lockout_duration", "2h"); err != nil {
		t.Fatal(err)
	}
	if err := st.GuardedSetting(ctx, log, "login.max_failures", &n); err != nil || n != 3 {
		t.Fatalf("read = %d, %v", n, err)
	}
	ended, cancel := context.WithCancel(ctx)
	cancel()
	if err := st.GuardedSetting(ended, log, "login.max_failures", &n); err != nil || n != 3 || out.Len() != 0 {
		t.Errorf("ended context = %d, %v, log %q; want the last value without a log", n, err, out.String())
	}
	if err := st.DB.Close(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			var m int
			if err := st.GuardedSetting(ctx, log, "login.max_failures", &m); err != nil || m != 3 {
				t.Errorf("unreadable = %d, %v, want the last value 3", m, err)
			}
		})
	}
	wg.Wait()
	var d string
	if err := st.GuardedSetting(ctx, log, "login.lockout_duration", &d); err == nil || errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unreadable without a value before = %q, %v, want an error", d, err)
	}
	logged := out.String()
	if strings.Count(logged, `"event":"setting.read_failed"`) != 17 || !strings.Contains(logged, `"kept":false`) {
		t.Errorf("log = %s", logged)
	}
	if strings.Contains(logged, `"2h"`) || strings.Contains(logged, `:3,`) || strings.Contains(logged, `:3}`) {
		t.Errorf("a value in the log: %s", logged)
	}
}
