// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"bufio"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// coreAreas are the areas of the security core in .github/core-paths.yml.
var coreAreas = []string{"authz", "cmd", "render", "redact", "audit", "input"}

// outsideCore are the packages outside the core the command pipeline may
// import: storage, the user and request types, the flags, the profiles it
// reads its rules from, and logging. The list may only shrink.
var outsideCore = []string{
	"internal/store", "internal/store/sqlitedb", "internal/users", "internal/features", "internal/profiles", "internal/logging",
}

// wholeCore are the directories the core areas name as a whole today; a
// change of the class file that adds or drops one must change this list
// too, so its effect on TestCoreImports shows in the diff.
var wholeCore = []string{
	"internal/agentproto", "internal/audit", "internal/commands", "internal/connmgr", "internal/foreign",
	"internal/logmask", "internal/netlimit", "internal/pattern", "internal/perm", "internal/auth", "internal/rcon", "internal/diag", "internal/syslog", "internal/messages",
}

var (
	areaLine = regexp.MustCompile(`^[a-z][a-z0-9_-]*:$`)
	globLine = regexp.MustCompile(`^  - ("?)([^"]+)("?)$`)
)

// corePackages are the directories a core area of the class file names as
// a whole ("dir/**"); a line outside the grammar of scripts/classify.sh
// fails the test.
func corePackages(t *testing.T) []string {
	t.Helper()
	f, err := os.Open("../../.github/core-paths.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []string
	area := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "#") || line == "":
		case areaLine.MatchString(line):
			area = strings.TrimSuffix(line, ":")
		case globLine.MatchString(line):
			g := globLine.FindStringSubmatch(line)[2]
			if slices.Contains(coreAreas, area) && strings.HasSuffix(g, "/**") {
				out = append(out, strings.TrimSuffix(g, "/**"))
			}
		default:
			t.Fatalf("class file: unexpected line %q", line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	want := slices.Sorted(slices.Values(wholeCore))
	if !slices.Equal(out, want) {
		t.Fatalf("whole core directories of the class file %v, want %v (update wholeCore with the class file)", out, want)
	}
	return out
}

// TestCoreImports covers PRJ-005 for the core area cmd: the production code
// of internal/commands imports directly only the standard library, packages
// of the core and the fixed list outsideCore, and every entry of the list is
// still used, so the list only shrinks. It does not cover what those
// packages import or decide in turn.
func TestCoreImports(t *testing.T) {
	const module = "github.com/NightDragon1/rconductor/"
	core := corePackages(t)
	allowed := func(rel string) bool {
		if slices.Contains(outsideCore, rel) {
			return true
		}
		return slices.ContainsFunc(core, func(c string) bool { return rel == c || strings.HasPrefix(rel, c+"/") })
	}
	n, used := 0, map[string]bool{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		n++
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			first, _, _ := strings.Cut(p, "/")
			switch {
			case !strings.Contains(first, "."):
				// standard library
			case strings.HasPrefix(p, module) && allowed(strings.TrimPrefix(p, module)):
				used[strings.TrimPrefix(p, module)] = true
			default:
				t.Errorf("%s imports %s: neither core nor on the list", path, p)
			}
		}
		return nil
	})
	if err != nil || n == 0 {
		t.Fatalf("walk: %d files, %v", n, err)
	}
	for _, p := range outsideCore {
		if !used[p] {
			t.Errorf("%s is on the list but no longer imported: remove it", p)
		}
	}
}
