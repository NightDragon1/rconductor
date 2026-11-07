// SPDX-License-Identifier: AGPL-3.0-or-later

package pattern

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestNoCatastrophicBacktracking covers SEC-003: a pattern that takes
// exponential time with backtracking engines ends in well under 1 s.
func TestNoCatastrophicBacktracking(t *testing.T) {
	p, err := Compile(`^(a+)+$`, 1)
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Repeat("a", MaxLineLen-1) + "!"
	start := time.Now()
	if _, ok, err := p.FirstMatch(context.Background(), input); ok || err != nil {
		t.Errorf("match = %v, %v", ok, err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}
}

func TestLimits(t *testing.T) {
	p, _ := Compile(`found (\d+)`, 1)
	// Line limit: a match after MaxLines lines is not seen.
	text := strings.Repeat("noise\n", MaxLines) + "found 7\n"
	if _, ok, _ := p.FirstMatch(context.Background(), text); ok {
		t.Error("match beyond the line limit")
	}
	if m, ok, _ := p.FirstMatch(context.Background(), "x\nfound 42\nfound 1"); !ok || m[0] != "42" {
		t.Errorf("first match = %v %v", m, ok)
	}
	// Line length: the end of an overlong line is cut off.
	long := strings.Repeat("x", MaxLineLen) + "found 5"
	if _, ok, _ := p.FirstMatch(context.Background(), long); ok {
		t.Error("match beyond the line length")
	}
	// Timeout.
	slow := p.WithTimeout(-time.Second)
	if _, _, err := slow.FirstMatch(context.Background(), "found 1"); !errors.Is(err, ErrTimeout) {
		t.Errorf("timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := p.FirstMatch(ctx, "found 1"); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
}

// TestFirstMatchNext: the line after the match comes along, also at the
// end of the search window, and nothing after a match on the last line.
func TestFirstMatchNext(t *testing.T) {
	p, err := Compile(`^a(.*)$`, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if m, next, ok, err := p.FirstMatchNext(ctx, "x\na1\r\nb\nc\n"); err != nil || !ok || m[0] != "1" || next != "b" {
		t.Errorf("match %v next %q ok %v err %v", m, next, ok, err)
	}
	if m, next, ok, err := p.FirstMatchNext(ctx, "a2"); err != nil || !ok || m[0] != "2" || next != "" {
		t.Errorf("match %v next %q ok %v err %v", m, next, ok, err)
	}
	if _, _, ok, err := p.FirstMatchNext(ctx, "x\ny\n"); err != nil || ok {
		t.Errorf("ok %v err %v", ok, err)
	}
	long := strings.Repeat("x\n", MaxLines-1) + "a3\n" + strings.Repeat("b", MaxLineLen+1)
	if _, next, ok, err := p.FirstMatchNext(ctx, long); err != nil || !ok || len(next) != MaxLineLen {
		t.Errorf("next %d ok %v err %v", len(next), ok, err)
	}
}

func TestCompileRules(t *testing.T) {
	for expr, groups := range map[string]int{
		"":                                0,
		strings.Repeat("a", MaxExprLen+1): 0,
		`(a`:                              0,
		`a(?=b)`:                          0, // lookahead is not RE2
		`no group`:                        1,
	} {
		if _, err := Compile(expr, groups); err == nil {
			t.Errorf("Compile(%.20q, %d) accepted", expr, groups)
		}
	}
}
