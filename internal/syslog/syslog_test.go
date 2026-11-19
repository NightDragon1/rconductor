// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package syslog

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logmask"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

// builtin masks with the built-in list of D-088.
type builtin struct{ m *logmask.Masker }

func (b builtin) Lines(lines []string, _ [][]string) ([]string, int) {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = b.m.Line(l)
	}
	return out, 0
}

func newBuiltin(t *testing.T) builtin {
	m, err := logmask.New(nil, logmask.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	return builtin{m}
}

// TestSinkKeepsBlackedOut covers D-091: events from info up are kept with
// level and code; a secret attribute, credentials in a URL and a masked
// value never reach the table; debug is not kept; stdout gets every line.
func TestSinkKeepsBlackedOut(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	var out bytes.Buffer
	logs := logging.New(&out, "panel", config.Log{Level: slog.LevelDebug, Format: "json"})
	sink := NewSink()
	logs.Also(sink)
	log := logs.Module("test")
	log.Debug("only on stdout")
	log.Info("started", logging.Event(logging.AppStart), slog.String("password", "hunter2"))
	log.Warn("db at postgres://user:pw1@db.lan failed", slog.String("detail", "token=abc123def"))
	sink.drain(ctx, st, newBuiltin(t), log)
	rows, err := st.DB.QueryContext(ctx, `SELECT level, code, text FROM system_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var level, code, text string
		if err := rows.Scan(&level, &code, &text); err != nil {
			t.Fatal(err)
		}
		got = append(got, level+" "+code+" "+text)
	}
	if len(got) != 2 || !strings.HasPrefix(got[0], "info app.start ") || !strings.HasPrefix(got[1], "warn  ") {
		t.Fatalf("rows = %q", got)
	}
	for _, secret := range []string{"hunter2", "pw1", "abc123def", "only on stdout"} {
		if strings.Contains(strings.Join(got, "\n"), secret) {
			t.Errorf("%q kept: %q", secret, got)
		}
	}
	if !strings.Contains(out.String(), "only on stdout") {
		t.Error("stdout lost the debug line")
	}
}

// TestSinkNeverBlocks: with a full queue and with a store that cannot be
// written, the panel goes on; every line not kept is counted, and the
// failure is logged once.
func TestSinkNeverBlocks(t *testing.T) {
	sink := NewSink()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range queueSize {
				_, _ = sink.Write([]byte(`{"level":"info"}`))
			}
		})
	}
	wg.Wait()
	if got := sink.Lost(); got != 7*queueSize {
		t.Errorf("lost %d, want %d", got, 7*queueSize)
	}
	st := storetest.New(t)
	_ = st.DB.Close()
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	sink.drain(context.Background(), st, newBuiltin(t), log)
	_, _ = sink.Write([]byte(`{"level":"info"}`))
	sink.drain(context.Background(), st, newBuiltin(t), log)
	if got := sink.Lost(); got != 8*queueSize+1 {
		t.Errorf("lost %d after failed writes, want %d", got, 8*queueSize+1)
	}
	if n := strings.Count(logs.String(), "system log not written"); n != 1 {
		t.Errorf("failure logged %d times", n)
	}
}

// TestPrune covers D-091: entries older than 7 days go; above
// system_log.max_bytes the oldest go first.
func TestPrune(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	if err := users.EnsureAuditDefaults(ctx, st); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	insert := func(n int, at time.Time, size int) {
		t.Helper()
		for range n {
			if _, err := st.DB.ExecContext(ctx, `INSERT INTO system_log (ts, level, text, size) VALUES (?, 'info', 'x', ?)`,
				at.Format("2006-01-02T15:04:05.000Z"), size); err != nil {
				t.Fatal(err)
			}
		}
	}
	insert(5, now.Add(-Retention-time.Second), 1)
	insert(5, now.Add(-Retention+time.Second), 1)
	sink := NewSink()
	log := slog.New(slog.DiscardHandler)
	sink.prune(ctx, st, log, now)
	count := func() (n int) {
		_ = st.DB.QueryRow(`SELECT count(*) FROM system_log`).Scan(&n)
		return n
	}
	if n := count(); n != 5 {
		t.Errorf("after the retention %d entries, want 5", n)
	}
	if _, err := st.DB.ExecContext(ctx, `UPDATE settings SET value = ? WHERE key = ?`, fmt.Sprint(users.MinSystemLogMaxBytes), users.SettingSystemLogMaxBytes); err != nil {
		t.Fatal(err)
	}
	insert(3000, now, 1000)
	sink.prune(ctx, st, log, now)
	var oldest int
	_ = st.DB.QueryRow(`SELECT min(id) FROM system_log`).Scan(&oldest)
	// 3,000,005 bytes in 3005 entries are over 1 MiB; the oldest go in
	// steps of 500 until 1005 entries (1,000,005 bytes) are left.
	if n := count(); n != 1005 || oldest != 2006 {
		t.Errorf("above the size %d entries from id %d, want the newest 1005 from 2006", n, oldest)
	}
}
