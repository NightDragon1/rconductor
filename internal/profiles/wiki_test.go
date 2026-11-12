// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	shippedprofiles "github.com/NightDragon1/rconductor/profiles"
)

// rowOf is the table row of the profile with the given id, or "".
func rowOf(page, id string) string {
	for line := range strings.SplitSeq(page, "\n") {
		cells := strings.Split(line, " | ")
		if len(cells) > 1 && cells[1] == "`"+id+"`" {
			return line
		}
	}
	return ""
}

// TestSupportedGamesPageCoversShippedProfiles holds the wiki page against
// profiles/ (DOC-005, CAT-001): every shipped profile has its row with
// name, kind, protocol, researched version, status and each source, and
// the page has no row more.
func TestSupportedGamesPageCoversShippedProfiles(t *testing.T) {
	page, err := SupportedGamesPage(shippedprofiles.FS())
	if err != nil {
		t.Fatal(err)
	}
	names, _ := fs.Glob(shippedprofiles.FS(), "*.yaml")
	if len(names) == 0 {
		t.Fatal("no shipped profiles")
	}
	rows := 0
	for line := range strings.SplitSeq(page, "\n") {
		if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| Game |") && !strings.HasPrefix(line, "| --- |") {
			rows++
		}
	}
	if rows != len(names) {
		t.Errorf("page has %d rows, profiles/ has %d files", rows, len(names))
	}
	for _, name := range names {
		data, _ := fs.ReadFile(shippedprofiles.FS(), name)
		p, err := Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		row := rowOf(page, p.ID)
		if row == "" {
			t.Errorf("%s: no row for id %s", name, p.ID)
			continue
		}
		version := p.ResearchedVersion
		if version == "" {
			version = "unknown"
		}
		for _, want := range []string{
			"| " + cell(p.Name) + " |", "| " + p.Kind + " |", "`" + p.Protocol.Type + "`",
			"| " + cell(version) + " |", "| " + p.Status + " |",
		} {
			if !strings.Contains(row, want) {
				t.Errorf("%s: row lacks %q: %s", name, want, row)
			}
		}
		for _, s := range p.Sources {
			if !strings.Contains(row, strings.Fields(s)[0]) {
				t.Errorf("%s: row lacks source %s", name, s)
			}
		}
	}
}

// TestSupportedGamesPageSortedByName keeps the table in name order.
func TestSupportedGamesPageSortedByName(t *testing.T) {
	page, err := SupportedGamesPage(shippedprofiles.FS())
	if err != nil {
		t.Fatal(err)
	}
	var prev string
	for line := range strings.SplitSeq(page, "\n") {
		cells := strings.Split(line, " | ")
		if len(cells) < 2 || !strings.HasPrefix(cells[1], "`") {
			continue
		}
		name := strings.ToLower(strings.TrimPrefix(cells[0], "| "))
		if name < prev {
			t.Errorf("%q after %q", name, prev)
		}
		prev = name
	}
}

// minimalProfile is a valid profile; name and sources are placed in.
func minimalProfile(name, sources string) string {
	return "schema: 1\nid: test-game\nname: \"" + name + "\"\n" + sources +
		"match:\n  images: [\"example/test*\"]\nprotocol:\n  type: source-rcon\n  default_port: 27015\n"
}

// TestSupportedGamesPageEscapesCells keeps a pipe or line break in a value
// from breaking the table, and a missing version, status or source
// shows as such.
func TestSupportedGamesPageEscapesCells(t *testing.T) {
	fsys := fstest.MapFS{"a.yaml": {Data: []byte(minimalProfile(`A | B`, "sources:\n  - \"https://example.org/x|y\"\n"))}}
	page, err := SupportedGamesPage(fsys)
	if err != nil {
		t.Fatal(err)
	}
	row := rowOf(page, "test-game")
	if !strings.HasPrefix(row, `| A \| B | `) {
		t.Errorf("name not escaped: %s", row)
	}
	if !strings.Contains(row, `(<https://example.org/x\|y>)`) {
		t.Errorf("source not escaped: %s", row)
	}
	if !strings.Contains(row, "| unknown | untested |") {
		t.Errorf("missing version or status not shown: %s", row)
	}

	fsys = fstest.MapFS{"a.yaml": {Data: []byte(minimalProfile("A", "sources:\n  - \"https://example.org/a (a copy)\"\n  - \"profiles/b.yaml (list output)\"\n"))}}
	page, _ = SupportedGamesPage(fsys)
	row = rowOf(page, "test-game")
	if !strings.HasSuffix(row, "| [example.org: a](<https://example.org/a>) (a copy), profiles/b.yaml (list output) |") {
		t.Errorf("note or text source not kept: %s", row)
	}

	fsys = fstest.MapFS{"a.yaml": {Data: []byte(minimalProfile("A", ""))}}
	page, _ = SupportedGamesPage(fsys)
	if row := rowOf(page, "test-game"); !strings.HasSuffix(row, "| none |") {
		t.Errorf("missing sources not shown: %s", row)
	}
}

// TestSupportedGamesPageRefusesInvalidProfile makes a broken file fail
// the page instead of leaving a gap in it.
func TestSupportedGamesPageRefusesInvalidProfile(t *testing.T) {
	fsys := fstest.MapFS{"bad.yaml": {Data: []byte("schema: 1\nid: x\nunknown_field: 1\n")}}
	if _, err := SupportedGamesPage(fsys); err == nil || !strings.Contains(err.Error(), "bad.yaml") {
		t.Errorf("err = %v, want an error naming bad.yaml", err)
	}
}
