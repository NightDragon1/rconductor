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

// TestD109BrokenRowsAndSwitches covers D-109: a broken row of a number is a
// failed read too, logged, with the last value read before; a switch read
// through LoggedSetting logs a broken row and a failed database and keeps
// no value read before, so the caller keeps the protection on.
func TestD109BrokenRowsAndSwitches(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	var out bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&out, nil))
	if _, err := st.EnsureDefault(ctx, "login.max_failures", 3); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.GuardedSetting(ctx, log, "login.max_failures", &n); err != nil || n != 3 {
		t.Fatalf("first read %d %v", n, err)
	}
	if _, err := st.DB.Exec(`UPDATE settings SET value = '"broken"' WHERE key = 'login.max_failures'`); err != nil {
		t.Fatal(err)
	}
	n = 0
	if err := st.GuardedSetting(ctx, log, "login.max_failures", &n); err != nil || n != 3 {
		t.Errorf("broken row: %d %v, want the last value 3", n, err)
	}
	if !strings.Contains(out.String(), `"event":"setting.read_failed"`) || strings.Contains(out.String(), "broken") {
		t.Errorf("broken row not logged by key alone: %s", out.String())
	}
	out.Reset()
	if _, err := st.EnsureDefault(ctx, "supporters.enabled", true); err != nil {
		t.Fatal(err)
	}
	var on bool
	if err := st.LoggedSetting(ctx, log, "supporters.enabled", &on); err != nil || !on {
		t.Fatalf("switch %v %v", on, err)
	}
	if err := st.LoggedSetting(ctx, log, "no.such.switch", &on); !errors.Is(err, sql.ErrNoRows) || out.Len() != 0 {
		t.Errorf("missing switch %v, log %s", err, out.String())
	}
	if _, err := st.DB.Exec(`UPDATE settings SET value = '7' WHERE key = 'supporters.enabled'`); err != nil {
		t.Fatal(err)
	}
	if err := st.LoggedSetting(ctx, log, "supporters.enabled", &on); err == nil || !strings.Contains(out.String(), `"setting":"supporters.enabled"`) {
		t.Errorf("broken switch: %v, log %s", err, out.String())
	}
	out.Reset()
	if err := st.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := st.LoggedSetting(ctx, log, "supporters.enabled", &on); err == nil || !strings.Contains(out.String(), `"event":"setting.read_failed"`) {
		t.Errorf("closed database: %v, log %s", err, out.String())
	}
}
