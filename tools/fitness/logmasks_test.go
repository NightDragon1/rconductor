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

func TestLiteralLine(t *testing.T) {
	l := literalLine(`(?i)(adminpass\s*=\s*)\S+`)
	if len(l) != lineLen || !strings.HasPrefix(l, "adminpass=x adminpass") {
		t.Errorf("line starts %q, %d bytes", l[:30], len(l))
	}
}
