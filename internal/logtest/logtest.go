// SPDX-License-Identifier: AGPL-3.0-or-later

// Package logtest lets tests wait for log lines. Code often changes a
// status or answers before it writes the line, so a test that reads the
// log only once fails now and then. Checks that a line is absent read it
// directly: there is nothing to wait for.
package logtest

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	timeout = 10 * time.Second
	poll    = 5 * time.Millisecond
)

// Wait returns once the log holds every wanted text and fails the test
// after the timeout.
func Wait(t testing.TB, logs fmt.Stringer, want ...string) {
	t.Helper()
	for _, w := range want {
		WaitCount(t, logs, w, 1)
	}
}

// WaitLine returns once a single line holds every wanted text and fails
// the test after the timeout.
func WaitLine(t testing.TB, logs fmt.Stringer, want ...string) {
	t.Helper()
	for start := time.Now(); ; time.Sleep(poll) {
		s := logs.String()
		for line := range strings.Lines(s) {
			if holdsAll(line, want) {
				return
			}
		}
		if time.Since(start) > timeout {
			t.Fatalf("no line with %q within %v:\n%s", want, timeout, s)
		}
	}
}

func holdsAll(line string, want []string) bool {
	for _, w := range want {
		if !strings.Contains(line, w) {
			return false
		}
	}
	return true
}

// WaitCount returns how often the log holds the text, once that is at
// least n, and fails the test after the timeout. The caller compares the
// result if n is exact.
func WaitCount(t testing.TB, logs fmt.Stringer, want string, n int) int {
	t.Helper()
	for start := time.Now(); ; time.Sleep(poll) {
		s := logs.String()
		got := strings.Count(s, want)
		if got >= n {
			return got
		}
		if time.Since(start) > timeout {
			t.Fatalf("%q %d times in the log, want %d within %v:\n%s", want, got, n, timeout, s)
		}
	}
}
