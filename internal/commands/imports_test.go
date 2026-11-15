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

// corePackages are the directories a core area of the class file names as
// a whole ("dir/**").
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
		case strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "":
		case !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":"):
			area = strings.TrimSuffix(line, ":")
		case slices.Contains(coreAreas, area) && strings.HasPrefix(line, "  - ") && strings.HasSuffix(line, "/**"):
			out = append(out, strings.TrimSuffix(strings.TrimPrefix(line, "  - "), "/**"))
		}
	}
	if err := sc.Err(); err != nil || len(out) == 0 {
		t.Fatalf("class file: %v %v", out, err)
	}
	return out
}

// TestCoreImports covers PRJ-005 for the core area cmd: the production code
// of internal/commands imports only the standard library, packages of the
// core and the fixed list outsideCore; what it builds commands from never
// lies outside the core.
func TestCoreImports(t *testing.T) {
	const module = "github.com/NightDragon1/rconductor/"
	core := corePackages(t)
	allowed := func(rel string) bool {
		if slices.Contains(outsideCore, rel) {
			return true
		}
		return slices.ContainsFunc(core, func(c string) bool { return rel == c || strings.HasPrefix(rel, c+"/") })
	}
	n := 0
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
			default:
				t.Errorf("%s imports %s: neither core nor on the list", path, p)
			}
		}
		return nil
	})
	if err != nil || n == 0 {
		t.Fatalf("walk: %d files, %v", n, err)
	}
}
