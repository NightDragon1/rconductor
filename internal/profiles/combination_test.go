// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// TestEveryProfileCombinationKeepsProtection covers D-076 and D-078: for
// every pair of shipped profiles with the same protocol and dialect, in
// both orders, and for the whole list of each protocol forwards and
// backwards, the separators and wrappers of every profile hold, and the
// command length never goes beyond the global maximum.
func TestEveryProfileCombinationKeepsProtection(t *testing.T) {
	entries, err := os.ReadDir("../../profiles")
	if err != nil {
		t.Fatal(err)
	}
	var all []*Profile
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile("../../profiles/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		p, err := Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		all = append(all, p)
	}
	family := func(p *Profile) string {
		if p.HTTPAPI != nil {
			return p.Protocol.Type + "/" + p.HTTPAPI.Dialect
		}
		return p.Protocol.Type
	}
	check := func(list []*Profile) {
		t.Helper()
		eff := Effective(list)
		names := make([]string, len(list))
		for i, p := range list {
			names[i] = p.ID
			for _, s := range p.Syntax.Separators {
				if !slices.Contains(eff.Syntax.Separators, s) {
					t.Errorf("%v: separator %q of %s lifted", names, s, p.ID)
				}
			}
			for _, w := range p.Syntax.Wrappers {
				if !slices.ContainsFunc(eff.Syntax.Wrappers, func(x string) bool { return NormalizePath(x) == NormalizePath(w) }) {
					t.Errorf("%v: wrapper %q of %s lifted", names, w, p.ID)
				}
			}
		}
		if eff.Syntax.MaxLength < 1 || eff.Syntax.MaxLength > maxMaxLength {
			t.Errorf("%v: length %d outside 1 to %d", names, eff.Syntax.MaxLength, maxMaxLength)
		}
	}
	byFamily := map[string][]*Profile{}
	pairs := 0
	for _, a := range all {
		byFamily[family(a)] = append(byFamily[family(a)], a)
		check([]*Profile{a})
		for _, b := range all {
			if a != b && family(a) == family(b) {
				check([]*Profile{a, b})
				pairs++
			}
		}
	}
	for _, list := range byFamily {
		check(list)
		rev := slices.Clone(list)
		slices.Reverse(rev)
		check(rev)
	}
	if pairs == 0 {
		t.Fatal("no pairs checked")
	}
}
