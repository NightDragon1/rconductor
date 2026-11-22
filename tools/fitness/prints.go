// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// printsPastHandler finds, in internal/ outside the logging package and
// the harness, the ways out past the redacting log handler that the call
// graph does not show by name (SEC-012): the builtins print and println,
// fmt.Fprint* and Write on os.Stdout or os.Stderr, a logger of its own
// (slog.New with any handler but slog.DiscardHandler), and
// reflect.ValueOf of a secret, whose String gives the plain value; and
// Reveal taken as a method value or expression outside the revealers.
func printsPastHandler(funcs map[*ssa.Function]bool, mod string) []finding {
	var found []finding
	seen := map[string]bool{}
	add := func(rel, detail string) {
		if !seen[rel+detail] {
			seen[rel+detail] = true
			found = append(found, finding{"redact", rel, detail})
		}
	}
	for fn := range funcs {
		p := pkgPath(fn)
		if !strings.HasPrefix(p, mod+"/internal/") || hasPrefix(p, mod, harness) || p == mod+logPkg {
			continue
		}
		rel := strings.TrimPrefix(p, mod+"/")
		for _, b := range fn.Blocks {
			for _, in := range b.Instrs {
				// s.Reveal as a method value: a closure over the secret.
				if mc, ok := in.(*ssa.MakeClosure); ok && !hasPrefix(p, mod, revealers) {
					if f, ok := mc.Fn.(*ssa.Function); ok && f.Name() == "Reveal$bound" && len(mc.Bindings) == 1 &&
						(secretOrPointer(mc.Bindings[0].Type(), mod) || revealIface(mc.Bindings[0].Type())) {
						add(rel, "reveals a secret outside its destinations")
					}
					continue
				}
				call, ok := in.(ssa.CallInstruction)
				if !ok {
					continue
				}
				cc := call.Common()
				if bi, ok := cc.Value.(*ssa.Builtin); ok && (bi.Name() == "print" || bi.Name() == "println") {
					add(rel, "prints past the redacting log handler")
					continue
				}
				callee := cc.StaticCallee()
				if callee == nil {
					continue
				}
				cp, name := pkgPath(callee), callee.Name()
				// logging.Secret.Reveal as a method expression: a thunk
				// whose first parameter is the secret.
				if name == "Reveal$thunk" && !hasPrefix(p, mod, revealers) && len(callee.Params) > 0 &&
					secretOrPointer(callee.Params[0].Type(), mod) {
					add(rel, "reveals a secret outside its destinations")
					continue
				}
				switch {
				case cp == "fmt" && strings.HasPrefix(name, "Fprint") && len(cc.Args) > 0 && stdStream(cc.Args[0]):
					add(rel, "prints past the redacting log handler")
				case cp == "os" && (name == "Write" || name == "WriteString") && len(cc.Args) > 0 && stdStream(cc.Args[0]):
					add(rel, "prints past the redacting log handler")
				case cp == "log/slog" && name == "New" && len(cc.Args) == 1 && !isGlobal(cc.Args[0], "log/slog", "DiscardHandler"):
					add(rel, "builds a logger past the redacting log handler")
				case cp == "reflect" && name == "ValueOf" && len(cc.Args) == 1 && fromSecret(cc.Args[0], mod):
					add(rel, "reveals a secret through reflect")
				}
			}
		}
	}
	return found
}

// stdStream reports whether v is os.Stdout or os.Stderr, also behind an
// interface.
func stdStream(v ssa.Value) bool {
	return isGlobal(v, "os", "Stdout") || isGlobal(v, "os", "Stderr")
}

// isGlobal reports whether v is the value of the package variable pkg.name,
// also behind an interface.
func isGlobal(v ssa.Value, pkg, name string) bool {
	if mi, ok := v.(*ssa.MakeInterface); ok {
		v = mi.X
	}
	load, ok := v.(*ssa.UnOp)
	if !ok {
		return false
	}
	g, ok := load.X.(*ssa.Global)
	return ok && g.Pkg != nil && g.Pkg.Pkg.Path() == pkg && g.Name() == name
}

// fromSecret reports whether v is a secret, or a pointer to one, put into
// an interface.
// fromSecret tells whether the value handed to reflect may hold a secret:
// a value made into an interface here whose type holds one anywhere
// (fields, elements, pointers), or an interface value from elsewhere, whose
// content is not known here.
func fromSecret(v ssa.Value, mod string) bool {
	mi, ok := v.(*ssa.MakeInterface)
	if !ok {
		return true
	}
	return holdsSecret(mi.X.Type(), mod, map[types.Type]bool{})
}

// holdsSecret looks through pointers, structs, arrays, slices and maps.
func holdsSecret(t types.Type, mod string, seen map[types.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	if isSecret(t, mod) {
		return true
	}
	switch u := t.Underlying().(type) {
	case *types.Pointer:
		return holdsSecret(u.Elem(), mod, seen)
	case *types.Struct:
		for i := range u.NumFields() {
			if holdsSecret(u.Field(i).Type(), mod, seen) {
				return true
			}
		}
	case *types.Slice:
		return holdsSecret(u.Elem(), mod, seen)
	case *types.Array:
		return holdsSecret(u.Elem(), mod, seen)
	case *types.Map:
		return holdsSecret(u.Key(), mod, seen) || holdsSecret(u.Elem(), mod, seen)
	case *types.Interface:
		return true
	}
	return false
}

// revealIface tells whether t is an interface with a method Reveal: a
// method value of it may reveal a secret behind it.
func revealIface(t types.Type) bool {
	it, ok := t.Underlying().(*types.Interface)
	if !ok {
		return false
	}
	for i := range it.NumMethods() {
		if it.Method(i).Name() == "Reveal" {
			return true
		}
	}
	return false
}
