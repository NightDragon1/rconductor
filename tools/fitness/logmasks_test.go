// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	if len(lines) < 4 || !strings.HasPrefix(lines[0], "adminpass=! adminpass=! ") || !strings.HasPrefix(lines[1], "adminpass=x ") {
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
	for _, p := range []string{`(pw=)\S+(?: .*END)?`, `(pw\s+)\d+(?: .*END)?`, `(pw=)\d+(?:;.*END)?`} {
		dir := maskDir(t, map[string]string{"q": `['` + p + `']`})
		found, err := checkLogMasks(dir, budget{patternSpend, time.Hour})
		if err != nil || len(found) == 0 || found[0].symbol != "q "+p {
			t.Errorf("%s: got %v %v", p, found, err)
		}
	}
}

// TestShortestMatches: the line of the shortest match is matched by its
// pattern, also with classes of blanks, digits and negated classes.
func TestShortestMatches(t *testing.T) {
	for _, p := range []string{
		`(pw\s+)\d+(?: .*END)?`, `(?i)(adminpass\s*=\s*)\S+`, `(k=)[^=]+`, `(k[\t ]+)\w+`,
		`(\bBearer\s+)[A-Za-z0-9._~+/=-]+`, `(k=)a{3,5}`, `(k=)(?:x|yy)+`, `(^k=)\S+`,
	} {
		if l := patternLines(p)[0]; !regexp.MustCompile(p).MatchString(l) {
			t.Errorf("%s does not match %q", p, l[:20])
		}
	}
}

// TestStackSum: the stack sums the 16 costliest profiles only.
func TestStackSum(t *testing.T) {
	per := map[string]time.Duration{}
	for i := range maxStack + 1 {
		per[fmt.Sprintf("p%02d", i)] = time.Duration(i+1) * time.Millisecond
	}
	// 2 + 3 + … + 17 ms: the cheapest profile is left out.
	if got, want := stackSum(per), 152*time.Millisecond; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if got := stackSum(map[string]time.Duration{"a": time.Millisecond}); got != time.Millisecond {
		t.Errorf("one profile: %s", got)
	}
}

// TestLogMasksErrors: a broken profile file and a missing profiles
// directory are errors, so the rule never passes on nothing.
func TestLogMasksErrors(t *testing.T) {
	dir := maskDir(t, map[string]string{"a": `['(k=)\S+']`})
	if err := os.WriteFile(filepath.Join(dir, "profiles", "bad.yaml"), []byte("log_masks: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := checkLogMasks(dir, budget{time.Hour, time.Hour}); err == nil {
		t.Error("broken profile file passes")
	}
	if _, err := checkLogMasks(t.TempDir(), budget{time.Hour, time.Hour}); err == nil {
		t.Error("no profiles directory passes")
	}
}
