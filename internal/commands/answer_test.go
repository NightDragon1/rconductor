// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon"
)

// failing answers every command as a failure the game reports.
type failing struct{ answer string }

func (f failing) Execute(context.Context, string, string) (string, error) {
	return f.answer, rcon.ErrCommandFailed
}

// TestFailedAnswerOnlyInSystemLog covers D-091: the answer of a failed
// command goes to the system log, cut to MaxLoggedAnswer, and never to
// stdout; a command that works logs no answer.
func TestFailedAnswerOnlyInSystemLog(t *testing.T) {
	w := newWorld(t)
	var out, sys lockedLog
	logs := logging.New(&out, "panel", config.Log{Format: "json"})
	logs.Also(&sys)
	svc := NewService(w.st, w.c, failing{answer: "Unknown command " + strings.Repeat("x", 3*MaxLoggedAnswer)}, logs.Module("cmd"))
	if _, err := svc.Send(context.Background(), w.as("admin"), w.server, "nope"); err != nil {
		t.Fatal(err)
	}
	line := sys.String()
	if !strings.Contains(line, `"answer":"Unknown command x`) || strings.Count(line, "x") > MaxLoggedAnswer+64 {
		t.Errorf("system log: %d bytes", len(line))
	}
	if strings.Contains(out.String(), "Unknown command") || !strings.Contains(out.String(), `"event":"cmd.response"`) {
		t.Errorf("stdout: %s", out.String())
	}
	if _, err := w.svc.Send(context.Background(), w.as("admin"), w.server, "list"); err != nil || strings.Contains(w.logs.String(), `"answer"`) {
		t.Errorf("a working command: %v", err)
	}
}
