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
	log.Warn("db at postgres://user:pw1@db.lan failed", slog.String("detail", "token=abc123def"),
		slog.String(logging.KeyAnswer, "Unknown token=abc123ans"))
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
	if !strings.Contains(got[1], `"answer":"Unknown token=`) {
		t.Errorf("answer not kept: %s", got[1])
	}
	for _, secret := range []string{"hunter2", "pw1", "abc123def", "abc123ans", "only on stdout"} {
		if strings.Contains(strings.Join(got, "\n"), secret) {
			t.Errorf("%q kept: %q", secret, got)
		}
	}
	if !strings.Contains(out.String(), "only on stdout") {
		t.Error("stdout lost the debug line")
	}
}

// TestSinkNeverBlocks: a full queue and a store that cannot be written
// count every line not kept; the failure is logged once.
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

// TestPrune (D-091): 7 days, then above the size the oldest first.
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
	// Steps of 500 oldest leave 1005 entries (1,000,005 bytes).
	if n := count(); n != 1005 || oldest != 2006 {
		t.Errorf("above the size %d entries from id %d, want the newest 1005 from 2006", n, oldest)
	}
}

// TestRunWritesLastLines: when ctx ends, Run writes what is queued.
func TestRunWritesLastLines(t *testing.T) {
	sink := NewSink()
	_, _ = sink.Write([]byte(`{"level":"info","msg":"panel stopped"}`))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sink.Run(ctx, storetest.New(t), newBuiltin(t), slog.New(slog.DiscardHandler), time.Now)
	if len(sink.queue) != 0 || sink.Lost() != 0 {
		t.Errorf("after the end %d queued, %d lost", len(sink.queue), sink.Lost())
	}
}

// TestC174QuotedValuesMasked: a value in quotes and a JSON answer are
// masked as they were logged, not after the JSON encoding escaped their
// quotes (D-088, D-091).
func TestC174QuotedValuesMasked(t *testing.T) {
	st := storetest.New(t)
	logs := logging.New(&bytes.Buffer{}, "panel", config.Log{Format: "json"})
	sink := NewSink()
	logs.Also(sink)
	log := logs.Module("cmd")
	log.Info("command answered", slog.String(logging.KeyAnswer, `Unknown password: "hunter2" <here>`))
	log.Info("command answered", slog.String(logging.KeyAnswer, `{"error":"bad","token":"abc12345"}`))
	sink.drain(context.Background(), st, newBuiltin(t), log)
	rows, err := st.DB.Query(`SELECT text FROM system_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var all []string
	for rows.Next() {
		var text string
		_ = rows.Scan(&text)
		all = append(all, text)
	}
	got := strings.Join(all, "\n")
	if len(all) != 2 || strings.Contains(got, "hunter2") || strings.Contains(got, "abc12345") ||
		!strings.Contains(got, `Unknown password: \"`) || !strings.Contains(got, "<here>") {
		t.Errorf("stored:\n%s", got)
	}
}

// TestHeadFirstOccurrence: an attribute named ts, level or event later in
// the line does not replace the head the handler wrote first.
func TestHeadFirstOccurrence(t *testing.T) {
	h := headOf(`{"ts":"2026-11-22T00:00:00Z","level":"INFO","event":"auth.login.ok","level":"ERROR","event":"x","ts":"1999"}`)
	if h.TS != "2026-11-22T00:00:00Z" || h.Level != "INFO" || h.Event != "auth.login.ok" {
		t.Errorf("head = %+v", h)
	}
	if h := headOf(`not json`); h != (head{}) {
		t.Errorf("head of text = %+v", h)
	}
}

// TestRewriteKeys: object keys go through f as the values do, so a host
// or a secret used as a key of foreign JSON is masked too; keys that meet
// stay apart.
func TestRewriteKeys(t *testing.T) {
	got := Rewrite(`{"caps":{"rack1.example.lan":1,"other":"rack1.example.lan"},"a":"x","b":"x"}`, func(in []string) []string {
		out := make([]string, len(in))
		for i, s := range in {
			out[i] = strings.ReplaceAll(s, "rack1.example.lan", "host-1")
			if s == "a" || s == "b" {
				out[i] = "k"
			}
		}
		return out
	})
	want := `{"caps":{"host-1":1,"other":"host-1"},"k":"x","k~2":"x"}`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// TestRewriteManyKeysMeet: keys that all become the same text stay apart
// with a short counter each; the line grows linearly, not with the square.
func TestRewriteManyKeysMeet(t *testing.T) {
	var b strings.Builder
	b.WriteString("{")
	for i := range 700 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `"k%03d":1`, i)
	}
	b.WriteString("}")
	got := Rewrite(b.String(), func(in []string) []string {
		out := make([]string, len(in))
		for i, s := range in {
			out[i] = s
			if strings.HasPrefix(s, "k") {
				out[i] = "[x]"
			}
		}
		return out
	})
	if len(got) > 3*b.Len() || !strings.Contains(got, `"[x]~700":1`) {
		t.Errorf("%d bytes for %d", len(got), b.Len())
	}
}

// TestC312LostLinesTold (D-091, D-077): lines lost to a full queue are
// told with the event code once per drain, with the number since the last
// report and in all; a drain without new losses tells nothing.
func TestC312LostLinesTold(t *testing.T) {
	sink := NewSink()
	for range queueSize + 5 {
		_, _ = sink.Write([]byte(`{"level":"info"}`))
	}
	var logs syncBuf
	log := logging.New(&logs, "panel", config.Log{Format: "json"}).Module("syslog")
	st := storetest.New(t)
	sink.drain(context.Background(), st, newBuiltin(t), log)
	sink.drain(context.Background(), st, newBuiltin(t), log)
	for range queueSize + 2 {
		_, _ = sink.Write([]byte(`{"level":"info"}`))
	}
	sink.drain(context.Background(), st, newBuiltin(t), log)
	got := logs.String()
	if n := strings.Count(got, `"event":"syslog.lines_lost"`); n != 2 {
		t.Fatalf("told %d times, want 2:\n%s", n, got)
	}
	if !strings.Contains(got, `"lost":5,"lost_total":5`) || !strings.Contains(got, `"lost":2,"lost_total":7`) {
		t.Errorf("numbers: %s", got)
	}
}

// syncBuf is a log buffer safe for the writer and the test.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestC312LostWhileWritesFail (D-091, D-077): with the logger writing into
// the sink itself, as in the panel, a failing database neither keeps the
// report going nor counts it; the lost lines are told once writes work.
func TestC312LostWhileWritesFail(t *testing.T) {
	sink := NewSink()
	var out syncBuf
	logs := logging.New(&out, "panel", config.Log{Format: "json"})
	logs.Also(sink)
	log := logs.Module("syslog")
	broken := storetest.New(t)
	_ = broken.DB.Close()
	for range queueSize + 3 {
		_, _ = sink.Write([]byte(`{"level":"info"}`))
	}
	for range 5 {
		sink.drain(context.Background(), broken, newBuiltin(t), log)
	}
	if n := strings.Count(out.String(), `"event":"syslog.lines_lost"`); n != 0 {
		t.Fatalf("told %d times while writes fail", n)
	}
	// The next line the panel logs is written, then the loss is told.
	_, _ = sink.Write([]byte(`{"level":"info"}`))
	sink.drain(context.Background(), storetest.New(t), newBuiltin(t), log)
	if n := strings.Count(out.String(), `"event":"syslog.lines_lost"`); n != 1 {
		t.Errorf("told %d times after writes work, want 1:\n%s", n, out.String())
	}
}
