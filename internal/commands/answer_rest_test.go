// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logmask"
	"github.com/NightDragon1/rconductor/internal/syslog"
)

// timingOut answers part of a command, then its time is up.
type timingOut struct{ answer string }

func (f timingOut) Execute(context.Context, string, string) (string, error) {
	return f.answer, context.DeadlineExceeded
}

// listMasker masks with the built-in list, as the panel without profiles.
type listMasker struct{}

func (listMasker) Lines(lines []string, _ [][]string) ([]string, int) {
	return logmask.ListLines(lines), 0
}

// answerOf is the field answer of the line with the event, "" without.
func answerOf(t *testing.T, logs, event string) string {
	t.Helper()
	for l := range strings.Lines(logs) {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil && m["event"] == event {
			a, _ := m["answer"].(string)
			return a
		}
	}
	return ""
}

// TestC324TimeoutWithAnswer (D-091): a command that times out after part
// of an answer logs that part for the system log only.
func TestC324TimeoutWithAnswer(t *testing.T) {
	w := newWorld(t)
	var out, sys lockedLog
	logs := logging.New(&out, "panel", config.Log{Format: "json"})
	logs.Also(&sys)
	svc := NewService(w.st, w.c, timingOut{answer: "half an answer"}, logs.Module("cmd"))
	if _, err := svc.Send(context.Background(), w.as("admin"), w.server, "slow"); err == nil {
		t.Fatal("no timeout")
	}
	if got := answerOf(t, sys.String(), "cmd.timeout"); got != "half an answer" {
		t.Errorf("system log answer %q", got)
	}
	if strings.Contains(out.String(), "half an answer") {
		t.Errorf("stdout: %s", out.String())
	}
}

// TestC324AnswerCutExactly (D-091): the logged answer is cut to exactly
// MaxLoggedAnswer bytes on a character boundary.
func TestC324AnswerCutExactly(t *testing.T) {
	w := newWorld(t)
	var out, sys lockedLog
	logs := logging.New(&out, "panel", config.Log{Format: "json"})
	logs.Also(&sys)
	long := "Unknown command " + strings.Repeat("ä", MaxLoggedAnswer)
	svc := NewService(w.st, w.c, failing{answer: long}, logs.Module("cmd"))
	if _, err := svc.Send(context.Background(), w.as("admin"), w.server, "nope"); err != nil {
		t.Fatal(err)
	}
	got := answerOf(t, sys.String(), "cmd.response")
	// "Unknown command " is 16 bytes, ä is 2: an even rest fits exactly.
	if len(got) != MaxLoggedAnswer || !strings.HasPrefix(long, got) {
		t.Errorf("answer of %d bytes, want %d as a prefix", len(got), MaxLoggedAnswer)
	}
}

// TestC324AnswerMaskedInTheSink (D-091, D-088): the answer of a failed
// command reaches the real system log sink masked.
func TestC324AnswerMaskedInTheSink(t *testing.T) {
	w := newWorld(t)
	var out lockedLog
	sink := syslog.NewSink()
	logs := logging.New(&out, "panel", config.Log{Format: "json"})
	logs.Also(sink)
	svc := NewService(w.st, w.c, failing{answer: `Bad: password="hunter two" token=abc123`}, logs.Module("cmd"))
	if _, err := svc.Send(context.Background(), w.as("admin"), w.server, "nope"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Run writes what is queued, then returns
	sink.Run(ctx, w.st, listMasker{}, logs.Module("syslog"), time.Now)
	var text string
	if err := w.st.DB.QueryRow(`SELECT text FROM system_log WHERE code = 'cmd.response'`).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "hunter") || strings.Contains(text, "abc123") || !strings.Contains(text, `password=\"•••\"`) {
		t.Errorf("stored: %s", text)
	}
}
