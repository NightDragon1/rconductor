// SPDX-License-Identifier: AGPL-3.0-or-later

// Command fitness checks that code outside the security core does not
// bypass it (PRJ-005), on the call graph of the panel module:
//
//	authz  every operation of api/openapi.yaml that needs a session reaches
//	       the caller's principal (internal/web.principal) or internal/perm
//	audit  every operation that is not a read reaches internal/audit.Write
//	cmd    only the command pipeline calls Execute of the RCON packages
//
// Excepted findings pass (rule symbol # reason); a stale exception fails.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// Package paths below the module path.
const webPkg, permPkg, auditPkg = "/internal/web", "/internal/perm", "/internal/audit"

// Execute of cmdSinks sends a command to a game server; only cmdCore calls it.
var (
	cmdSinks = []string{"/internal/rcon", "/internal/connmgr"}
	cmdCore  = []string{"/internal/rcon", "/internal/connmgr", "/internal/commands"}
)

type finding struct{ rule, symbol, detail string }

func (f finding) key() string { return f.rule + " " + f.symbol }

func main() {
	dir := flag.String("dir", "..", "root of the module to check")
	exc := flag.String("exceptions", "fitness/exceptions.txt", "exception file")
	flag.Parse()
	found, err := check(*dir)
	var allowed map[string]bool
	if err == nil {
		allowed, err = readExceptions(*exc)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fitness:", err)
		os.Exit(2)
	}
	if report(found, allowed, os.Stdout) > 0 {
		os.Exit(1)
	}
	fmt.Println("fitness ok")
}

// report prints and counts unexcepted findings and stale exceptions.
func report(found []finding, allowed map[string]bool, w *os.File) int {
	bad, seen := 0, map[string]bool{}
	for _, f := range found {
		seen[f.key()] = true
		if !allowed[f.key()] {
			_, _ = fmt.Fprintf(w, "%s: %s\n", f.key(), f.detail)
			bad++
		}
	}
	for k := range allowed {
		if !seen[k] {
			_, _ = fmt.Fprintf(w, "%s: exception without finding, remove it\n", k)
			bad++
		}
	}
	return bad
}

func readExceptions(path string) (map[string]bool, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	out := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		if fields := strings.Fields(line); len(fields) == 2 {
			out[fields[0]+" "+fields[1]] = true
		} else if len(fields) != 0 {
			return nil, fmt.Errorf("exception line %q: want rule, symbol and # reason", sc.Text())
		}
	}
	return out, sc.Err()
}

type operation struct{ public, write bool }

// operations reads the operation IDs of api/openapi.yaml.
func operations(dir string) (map[string]operation, error) {
	b, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "api", "openapi.yaml")))
	if err != nil {
		return nil, err
	}
	var doc struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	out := map[string]operation{}
	for _, methods := range doc.Paths {
		for m, node := range methods {
			var op struct {
				OperationID string            `yaml:"operationId"`
				Security    *[]map[string]any `yaml:"security"`
			}
			if !slices.Contains([]string{"get", "head", "post", "put", "patch", "delete"}, m) {
				continue
			}
			if err := node.Decode(&op); err != nil || op.OperationID == "" {
				return nil, fmt.Errorf("operation %s without operationId", m)
			}
			name := strings.ToUpper(op.OperationID[:1]) + op.OperationID[1:]
			out[name] = operation{public: op.Security != nil && len(*op.Security) == 0, write: m != "get" && m != "head"}
		}
	}
	return out, nil
}

func check(dir string) ([]finding, error) {
	ops, err := operations(dir)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{Dir: dir, Mode: packages.LoadAllSyntax | packages.NeedModule}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}
	if packages.PrintErrors(pkgs) > 0 || len(pkgs) == 0 || pkgs[0].Module == nil {
		return nil, fmt.Errorf("the module in %s does not load", dir)
	}
	mod := pkgs[0].Module.Path
	var found []finding
	prog, _ := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()
	web := prog.ImportedPackage(mod + webPkg)
	if web == nil || web.Type("apiServer") == nil {
		return nil, fmt.Errorf("type apiServer missing in %s", mod+webPkg)
	}
	srv := types.NewPointer(web.Type("apiServer").Type())
	funcs := ssautil.AllFunctions(prog)
	handlers := map[string]*ssa.Function{}
	for name := range ops {
		if fn := prog.LookupMethod(srv, web.Pkg, name); fn != nil {
			handlers[name], funcs[fn] = fn, true
		}
	}
	cg := vta.CallGraph(funcs, cha.CallGraph(prog))
	cmdCallers := map[string]bool{}
	for fn, n := range cg.Nodes {
		if fn == nil || pkgPath(fn) == "" || hasPrefix(pkgPath(fn), mod, cmdCore) {
			continue
		}
		for _, e := range n.Out {
			if c := e.Callee.Func; c.Name() == "Execute" && hasPrefix(pkgPath(c), mod, cmdSinks) {
				cmdCallers[strings.TrimPrefix(pkgPath(fn), mod+"/")] = true
			}
		}
	}
	for _, p := range slices.Sorted(maps.Keys(cmdCallers)) {
		found = append(found, finding{"cmd", p, "sends a command to a game server outside the command pipeline"})
	}
	authz := func(f *ssa.Function) bool {
		p := pkgPath(f)
		return (p == mod+webPkg && f.Name() == "principal") || p == mod+permPkg || strings.HasPrefix(p, mod+permPkg+"/")
	}
	audit := func(f *ssa.Function) bool { return pkgPath(f) == mod+auditPkg && f.Name() == "Write" }
	names := make([]string, 0, len(ops))
	for n := range ops {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, name := range names {
		op := ops[name]
		fn := handlers[name]
		if fn == nil {
			found = append(found, finding{"authz", name, "no handler"})
			continue
		}
		if !op.public && !reaches(cg, fn, authz) {
			found = append(found, finding{"authz", name, "does not reach the principal or internal/perm"})
		}
		if op.write && !reaches(cg, fn, audit) {
			found = append(found, finding{"audit", name, "does not reach audit.Write"})
		}
	}
	return found, nil
}

func pkgPath(f *ssa.Function) string {
	if f.Pkg == nil {
		if o := f.Origin(); o != nil && o.Pkg != nil {
			return o.Pkg.Pkg.Path()
		}
		return ""
	}
	return f.Pkg.Pkg.Path()
}

func hasPrefix(path, mod string, list []string) bool {
	for _, l := range list {
		if path == mod+l || strings.HasPrefix(path, mod+l+"/") {
			return true
		}
	}
	return false
}

// reaches reports whether a function satisfying want is reachable from fn.
func reaches(cg *callgraph.Graph, fn *ssa.Function, want func(*ssa.Function) bool) bool {
	start := cg.Nodes[fn]
	if start == nil {
		return false
	}
	seen := map[*callgraph.Node]bool{start: true}
	queue := []*callgraph.Node{start}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if want(n.Func) {
			return true
		}
		for _, e := range n.Out {
			if !seen[e.Callee] {
				seen[e.Callee] = true
				queue = append(queue, e.Callee)
			}
		}
	}
	return false
}
