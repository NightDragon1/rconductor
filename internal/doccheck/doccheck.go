// SPDX-License-Identifier: AGPL-3.0-or-later

// Package doccheck reads the tables of the configuration guide, so that
// tests can hold its defaults and limits against the code (DOC-001).
package doccheck

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Row is one table row: the cells after the first, by column name.
type Row map[string]string

var names = regexp.MustCompile("`([^`]+)`")

// Section returns the rows of the first table below the heading
// "## heading", keyed by every name in backticks in the first cell, so
// that "`A` / `A_FILE`" gives two keys for one row.
func Section(doc, heading string) (map[string]Row, error) {
	lines := strings.Split(doc, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "## "+heading {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil, fmt.Errorf("no section %q", heading)
	}
	var header []string
	rows := map[string]Row{}
	for _, l := range lines[start:] {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "## ") {
			break
		}
		if !strings.HasPrefix(l, "|") {
			if header != nil {
				break
			}
			continue
		}
		cells := split(l)
		switch {
		case header == nil:
			header = cells
		case strings.HasPrefix(cells[0], "---"):
		default:
			if len(cells) != len(header) {
				return nil, fmt.Errorf("%s: row %q has %d cells, want %d", heading, l, len(cells), len(header))
			}
			row := Row{}
			for i := 1; i < len(cells); i++ {
				row[header[i]] = cells[i]
			}
			for _, m := range names.FindAllStringSubmatch(cells[0], -1) {
				rows[m[1]] = row
			}
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s: no table", heading)
	}
	return rows, nil
}

func split(line string) []string {
	line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// Plain is a cell without backticks; "empty" is the empty string.
func Plain(cell string) string {
	cell = strings.TrimSpace(strings.ReplaceAll(cell, "`", ""))
	if cell == "empty" {
		return ""
	}
	return cell
}

// Duration reads a duration as the guide writes it: "250ms", "30s",
// "15m", "24h" or "7 days".
func Duration(s string) (time.Duration, error) {
	s = Plain(s)
	if n, ok := strings.CutSuffix(s, " days"); ok {
		d, err := strconv.Atoi(n)
		return time.Duration(d) * 24 * time.Hour, err
	}
	return time.ParseDuration(s)
}

// Range reads the limits "lo to hi" at the start of a cell, such as
// "1 to 1000" or "1h to 30 days"; text after the upper limit is ignored.
func Range(cell string) (lo, hi string, ok bool) {
	lo, rest, ok := strings.Cut(Plain(cell), " to ")
	if !ok || lo == "" || lo[0] < '0' || lo[0] > '9' || strings.Contains(lo, " ") {
		return "", "", false
	}
	f := strings.Fields(rest)
	if len(f) == 0 {
		return "", "", false
	}
	hi = strings.TrimRight(f[0], ",;")
	if len(f) > 1 && strings.TrimRight(f[1], ",;") == "days" {
		hi += " days"
	}
	return lo, hi, true
}
