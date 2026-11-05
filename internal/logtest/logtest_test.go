// SPDX-License-Identifier: AGPL-3.0-or-later

package logtest

import (
	"strings"
	"sync"
	"testing"
	"time"
)

type buffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *buffer) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.b.WriteString(s)
}

func (l *buffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestWaitSeesLateLines(t *testing.T) {
	logs := &buffer{}
	logs.add("one\n")
	go func() {
		time.Sleep(50 * time.Millisecond)
		logs.add("two\ntwo\n")
	}()
	Wait(t, logs, "one", "two")
	if n := WaitCount(t, logs, "two", 2); n != 2 {
		t.Errorf("count = %d", n)
	}
}

func TestWaitLineNeedsOneLine(t *testing.T) {
	logs := &buffer{}
	logs.add("a\nb\n")
	go func() {
		time.Sleep(50 * time.Millisecond)
		logs.add("a b\n")
	}()
	WaitLine(t, logs, "a", "b")
	if got := logs.String(); !strings.HasSuffix(got, "a b\n") {
		t.Errorf("returned before the line came: %q", got)
	}
}

func TestWaitCountReportsMore(t *testing.T) {
	logs := &buffer{}
	logs.add("x x x")
	if n := WaitCount(t, logs, "x", 1); n != 3 {
		t.Errorf("count = %d, the caller needs the real number", n)
	}
}
