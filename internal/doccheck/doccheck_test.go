// SPDX-License-Identifier: AGPL-3.0-or-later

package doccheck

import (
	"testing"
	"time"
)

const sample = `# Guide

## First

| Name | Default | Allowed |
| --- | --- | --- |
| ` + "`A` / `A_FILE`" + ` | empty | 1 to 1000 |
| ` + "`B`" + ` (1.1) | ` + "`7`" + ` | 1h to 30 days, more text |

## Second

| Name | Default |
| --- | --- |
| ` + "`A`" + ` | 2 |
`

// TestSection covers DOC-001: rows are read per section and per name.
func TestSection(t *testing.T) {
	rows, err := Section(sample, "First")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows["A_FILE"]["Default"] != "empty" || Plain(rows["B"]["Default"]) != "7" {
		t.Fatalf("rows = %v", rows)
	}
	second, err := Section(sample, "Second")
	if err != nil || second["A"]["Default"] != "2" || len(second) != 1 {
		t.Fatalf("second = %v, %v", second, err)
	}
	if _, err := Section(sample, "Missing"); err == nil {
		t.Error("missing section accepted")
	}
	if _, err := Section("## X\n\n| a | b |\n| --- | --- |\n| `k` |\n", "X"); err == nil {
		t.Error("short row accepted")
	}
}

func TestRangeAndDuration(t *testing.T) {
	for cell, want := range map[string][2]string{
		"1 to 1000":                {"1", "1000"},
		"1h to 30 days, more text": {"1h", "30 days"},
		"`5s` to `1h`":             {"5s", "1h"},
	} {
		lo, hi, ok := Range(cell)
		if !ok || lo != want[0] || hi != want[1] {
			t.Errorf("Range(%q) = %q, %q, %v", cell, lo, hi, ok)
		}
	}
	for _, cell := range []string{"on, off", "IPs or CIDR ranges", "up to 253 characters"} {
		if _, _, ok := Range(cell); ok {
			t.Errorf("Range(%q) found limits", cell)
		}
	}
	for s, want := range map[string]time.Duration{"7 days": 7 * 24 * time.Hour, "250ms": 250 * time.Millisecond, "`24h`": 24 * time.Hour} {
		if got, err := Duration(s); err != nil || got != want {
			t.Errorf("Duration(%q) = %v, %v", s, got, err)
		}
	}
}
