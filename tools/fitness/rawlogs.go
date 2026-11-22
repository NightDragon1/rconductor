// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"go/constant"
	"go/token"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// noLogOrders are the order types whose answer holds no container log
// content; every other one (logs.tail, logs.follow, version.detect and any
// new type) is a log order (UI-022).
var noLogOrders = map[string]bool{"container.action": true, "logs.stop": true, "volumes.size": true}

// logOrder tells whether a call of RequestData may order container log
// content: its order type is not one of noLogOrders, or no constant.
func logOrder(site ssa.CallInstruction) bool {
	if site == nil {
		return true
	}
	cc := site.Common()
	i := 2 // ctx, agentID, typ
	if !cc.IsInvoke() {
		i = 3 // the receiver comes first
	}
	if i >= len(cc.Args) {
		return true
	}
	c, ok := cc.Args[i].(*ssa.Const)
	if !ok || c.Value == nil || c.Value.Kind() != constant.String {
		return true
	}
	return !noLogOrders[constant.StringVal(c.Value)]
}

// inFiles tells whether fn lies in one of files (paths below root; one
// ending in "/" stands for a directory).
func inFiles(fset *token.FileSet, fn *ssa.Function, root string, files []string) bool {
	if fn == nil || !fn.Pos().IsValid() {
		return false
	}
	// The real file, not one a //line directive names.
	rel, err := filepath.Rel(root, fset.PositionFor(fn.Pos(), false).Filename)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, f := range files {
		if rel == f || strings.HasSuffix(f, "/") && strings.HasPrefix(rel, f) {
			return true
		}
	}
	return false
}

// rawLogValues finds raw log reads taken as a method value (RequestData,
// Logs or OnLines of hub and cache as $bound), which the call graph sees
// only as a call of the wrapper, outside the files of rawLogFiles.
func rawLogValues(funcs map[*ssa.Function]bool, mod string, fset *token.FileSet, root string) []finding {
	seen := map[finding]bool{}
	var out []finding
	for fn := range funcs {
		p := pkgPath(fn)
		if p == "" || !strings.HasPrefix(p, mod+"/") || hasPrefix(p, mod, harness) || inFiles(fset, fn, root, rawLogFiles) {
			continue
		}
		for _, b := range fn.Blocks {
			for _, in := range b.Instrs {
				mc, ok := in.(*ssa.MakeClosure)
				if !ok {
					continue
				}
				f, _ := mc.Fn.(*ssa.Function)
				if f == nil || f.Object() == nil || f.Object().Pkg() == nil {
					continue
				}
				name, cp := strings.TrimSuffix(f.Name(), "$bound"), f.Object().Pkg().Path()
				if name == "RequestData" && cp == mod+"/internal/agenthub" ||
					(name == "Logs" || name == "OnLines") && cp == mod+"/internal/cache" {
					x := finding{"redact", strings.TrimPrefix(p, mod+"/"), "reads raw container logs outside the masking"}
					if !seen[x] {
						seen[x] = true
						out = append(out, x)
					}
				}
			}
		}
	}
	return out
}
