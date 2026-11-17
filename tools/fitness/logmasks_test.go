// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func maskDir(t *testing.T, profiles map[string]string) string {
	dir := t.TempDir()
	_ = os.Mkdir(filepath.Join(dir, "profiles"), 0o700)
	for id, masks := range profiles {
		body := "id: " + id + "\nlog_masks: " + masks + "\n"
		if err := os.WriteFile(filepath.Join(dir, "profiles", id+".yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestLogMasksBudget: a pattern over its budget, a stack over its budget
// and a pattern that does not compile are findings; within the budgets
// there are none. Budgets of zero and of an hour stand for slow and fast
// patterns, so the test does not depend on the speed of the machine.
func TestLogMasksBudget(t *testing.T) {
	dir := maskDir(t, map[string]string{
		"a": `['(?i)(adminpass\s*=\s*)\S+']`,
		"b": `['(-rcon\s+)\S+', '(a']`,
	})
	found, err := checkLogMasks(dir, budget{time.Hour, time.Hour})
	if err != nil || len(found) != 1 || found[0].key() != "logmask b (a" {
		t.Fatalf("got %v %v", found, err)
	}
	found, _ = checkLogMasks(dir, budget{0, time.Hour})
	if len(found) < 2 || !strings.Contains(found[1].detail, "budget") {
		t.Errorf("pattern over budget: %v", found)
	}
	found, _ = checkLogMasks(dir, budget{time.Hour, 0})
	stack := 0
	for _, f := range found {
		if f.symbol == "stack" {
			stack++
		}
	}
	if stack == 0 {
		t.Errorf("stack over budget: %v", found)
	}
}

func TestPatternLines(t *testing.T) {
	lines := patternLines(`(?i)(adminpass\s*=\s*)\S+`)
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "adminpass=! adminpass=! ") || !strings.HasPrefix(lines[1], "adminpass=x ") {
		t.Errorf("lines start %q", []string{lines[0][:30], lines[1][:30]})
	}
	for _, l := range lines {
		if len(l) != lineLen {
			t.Errorf("%d bytes", len(l))
		}
	}
}

// TestLogMasksRestartingSearch: a pattern whose short matches each start a
// search that runs to the end of the line takes time square in the length
// of the line; its own shortest match over and over finds that within the
// budget of the CI (about 130 ms against 10 ms).
func TestLogMasksRestartingSearch(t *testing.T) {
	dir := maskDir(t, map[string]string{"q": `['(pw=)\S+(?: .*END)?']`})
	found, err := checkLogMasks(dir, budget{patternSpend, time.Hour})
	if err != nil || len(found) == 0 || found[0].symbol != `q (pw=)\S+(?: .*END)?` {
		t.Errorf("got %v %v", found, err)
	}
}
