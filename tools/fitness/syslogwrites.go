// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"strings"

	"golang.org/x/tools/go/ssa"
)

// systemLogWrites finds writes of the system log outside its masking sink
// (D-091): a call of InsertSystemLog of the generated queries, also as a
// method value (InsertSystemLog$bound) or a method expression
// (InsertSystemLog$thunk), from any package but internal/syslog.
func systemLogWrites(funcs map[*ssa.Function]bool, mod string) []finding {
	queries := mod + sysLogQueries
	isWrite := func(f *ssa.Function) bool {
		if f == nil {
			return false
		}
		name := strings.TrimSuffix(strings.TrimSuffix(f.Name(), "$bound"), "$thunk")
		obj := f.Object()
		return name == "InsertSystemLog" && obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == queries
	}
	seen := map[finding]bool{}
	var out []finding
	for fn := range funcs {
		p := pkgPath(fn)
		if p == "" || p == mod+sysLogSink || p == queries || !strings.HasPrefix(p, mod+"/") {
			continue
		}
		rel := strings.TrimPrefix(p, mod+"/")
		for _, b := range fn.Blocks {
			for _, in := range b.Instrs {
				var target *ssa.Function
				switch v := in.(type) {
				case *ssa.MakeClosure:
					target, _ = v.Fn.(*ssa.Function)
				case ssa.CallInstruction:
					target = v.Common().StaticCallee()
				}
				if isWrite(target) {
					f := finding{"redact", rel, "writes the system log past its masking"}
					if !seen[f] {
						seen[f] = true
						out = append(out, f)
					}
				}
			}
		}
	}
	return out
}
