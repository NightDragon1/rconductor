// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build eventcheck

package logging

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

// TestEveryEventCode covers NFR-016: after a run with the tag eventlog each
// code of Design 8.3 occurred at least once, as a valid JSON line with the
// fields of Design 8.2 that every line carries.
func TestEveryEventCode(t *testing.T) {
	f, err := os.Open(os.Getenv("RCD_EVENT_WITNESS"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	seen := map[string]int{}
	lines := bufio.NewScanner(f)
	lines.Buffer(make([]byte, 1<<20), 1<<24)
	for lines.Scan() {
		var m map[string]any
		if err := json.Unmarshal(lines.Bytes(), &m); err != nil {
			t.Errorf("not JSON: %s", lines.Text())
			continue
		}
		ev, ok := m[KeyEvent].(string)
		if !ok {
			continue
		}
		for _, k := range []string{KeyTS, "level", KeyComp} {
			if m[k] == nil {
				t.Errorf("%s without %s: %s", ev, k, lines.Text())
			}
		}
		seen[ev]++
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
	for _, c := range AllCodes {
		if seen[string(c)] == 0 {
			t.Errorf("%s did not occur", c)
		}
	}
	t.Logf("%d codes, %d lines", len(seen), func() (n int) {
		for _, c := range seen {
			n += c
		}
		return n
	}())
}
