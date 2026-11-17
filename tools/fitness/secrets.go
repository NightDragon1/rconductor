// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bufio"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// readDestinations reads the packages that may convert a logging.Secret
// to its plain value besides the revealers: one package path below the
// module and # the reason per line.
func readDestinations(path string) (map[string]string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line, reason, _ := strings.Cut(sc.Text(), "#")
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0 && strings.HasPrefix(strings.TrimSpace(sc.Text()), "#"):
		case len(fields) == 1 && strings.TrimSpace(reason) != "":
			out[fields[0]] = strings.TrimSpace(reason)
		case len(fields) != 0 || strings.TrimSpace(sc.Text()) != "":
			return nil, fmt.Errorf("destination line %q: want a package and # reason", sc.Text())
		}
	}
	return out, sc.Err()
}

// secretConversions finds conversions of logging.Secret, or a pointer to
// one, to another type (string(s), []byte(s), (*string)(&s)) outside the revealers and the destinations; a
// destination without a conversion is a finding too, so the list stays
// true.
func secretConversions(funcs map[*ssa.Function]bool, mod string, dests map[string]string) []finding {
	used := map[string]bool{}
	var found []finding
	seen := map[string]bool{}
	for fn := range funcs {
		p := pkgPath(fn)
		if p == "" || hasPrefix(p, mod, revealers) {
			continue
		}
		rel := strings.TrimPrefix(p, mod+"/")
		for _, b := range fn.Blocks {
			for _, in := range b.Instrs {
				var x ssa.Value
				switch v := in.(type) {
				case *ssa.ChangeType:
					x = v.X
				case *ssa.Convert:
					x = v.X
				default:
					continue
				}
				if !secretOrPointer(x.Type(), mod) || secretOrPointer(in.(ssa.Value).Type(), mod) {
					continue
				}
				if _, ok := dests[rel]; ok {
					used[rel] = true
				} else if !seen[rel] {
					seen[rel] = true
					found = append(found, finding{"redact", rel, "converts a secret to its plain value outside its destinations"})
				}
			}
		}
	}
	for rel := range dests {
		if !used[rel] {
			found = append(found, finding{"redact", rel, "listed as a secret destination but converts no secret"})
		}
	}
	return found
}

// secretOrPointer is a Secret or a pointer to one at any depth, so that
// (*string)(&s) and unsafe.Pointer(&s) or unsafe.Pointer(&p) count too.
func secretOrPointer(t types.Type, mod string) bool {
	for p, ok := t.(*types.Pointer); ok; p, ok = t.(*types.Pointer) {
		t = p.Elem()
	}
	return isSecret(t, mod)
}

func isSecret(t types.Type, mod string) bool {
	n, ok := t.(*types.Named)
	return ok && n.Obj().Name() == "Secret" && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == mod+logPkg
}
