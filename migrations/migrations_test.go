// SPDX-License-Identifier: AGPL-3.0-or-later

package migrations

import (
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

// TestNumbersUnique covers NFR-013: the SQL files and the Go migrations
// share one sequence; no number is used twice, every SQL file starts with
// its number.
func TestNumbersUnique(t *testing.T) {
	seen := map[int64]string{}
	entries, err := fs.ReadDir(SQLite(), ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		num, _, ok := strings.Cut(e.Name(), "_")
		n, err := strconv.ParseInt(num, 10, 64)
		if !ok || err != nil || !strings.HasSuffix(e.Name(), ".sql") {
			t.Errorf("%s: not <number>_<name>.sql", e.Name())
			continue
		}
		if other, dup := seen[n]; dup {
			t.Errorf("number %d twice: %s and %s", n, other, e.Name())
		}
		seen[n] = e.Name()
	}
	for _, m := range SQLiteGo() {
		if other, dup := seen[m.Version]; dup {
			t.Errorf("number %d twice: %s and the Go migration", m.Version, other)
		}
		seen[m.Version] = "Go migration"
	}
	if len(seen) == 0 {
		t.Fatal("no migrations")
	}
}
