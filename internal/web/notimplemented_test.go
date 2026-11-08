// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// stubs lists the operations whose handler calls notImplemented, read from
// the source of this package.
func stubs(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "notimplemented.go" {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "notImplemented" {
				op := "?"
				if len(call.Args) == 1 {
					if lit, ok := call.Args[0].(*ast.BasicLit); ok {
						op, _ = strconv.Unquote(lit.Value)
					}
				}
				out = append(out, op+" ("+fset.Position(call.Pos()).String()+")")
			}
			return true
		})
	}
	slices.Sort(out)
	return out
}

// TestNotImplementedHandlers lists the stub handlers of contract first. It
// fails only with RCD_REQUIRE_NO_STUBS=1, the check of the gate review
// (make stubs): then the list must be empty.
func TestNotImplementedHandlers(t *testing.T) {
	list := stubs(t)
	for _, s := range list {
		t.Logf("stub: %s", s)
	}
	t.Logf("%d stub handlers", len(list))
	if os.Getenv("RCD_REQUIRE_NO_STUBS") == "1" && len(list) > 0 {
		t.Errorf("%d handlers still answer 501: %v", len(list), list)
	}
}

func TestNotImplementedAnswers501(t *testing.T) {
	var p *problem
	if !errors.As(notImplemented("getThing"), &p) || p.status != 501 || !strings.Contains(p.detail, "getThing") {
		t.Errorf("notImplemented = %+v", p)
	}
}
