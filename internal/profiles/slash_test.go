// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"strings"
	"testing"
)

// TestO9FactorioSlashCommands covers CAT-005 and CAT-007 for Factorio,
// which takes input without a leading "/" as chat: the profile keeps the
// slash (strip_prefix ""), so every catalog path, the stop command and
// every wrapper carry it, also in the effective rules of the server.
// The shipped profile had these properties before; the test guards them.
func TestO9FactorioSlashCommands(t *testing.T) {
	f := shipped(t, "factorio.yaml")
	if !f.Sets(FieldStripPrefix) || f.Syntax.StripPrefix == nil || *f.Syntax.StripPrefix != "" {
		t.Fatalf("strip_prefix %v, want \"\" set by the profile", f.Syntax.StripPrefix)
	}
	eff := Effective([]*Profile{f})
	if *eff.Syntax.StripPrefix != "" {
		t.Errorf("effective strip_prefix %q, want \"\"", *eff.Syntax.StripPrefix)
	}
	catalog := MergeCatalogs([][]Command{f.Catalog})
	if len(catalog) == 0 {
		t.Fatal("empty catalog")
	}
	for _, c := range catalog {
		if !strings.HasPrefix(c.Path, "/") {
			t.Errorf("catalog path %q without leading /", c.Path)
		}
	}
	if eff.Stop == nil || !strings.HasPrefix(eff.Stop.Command, "/") {
		t.Errorf("stop command %+v without leading /", eff.Stop)
	}
	for _, w := range eff.Syntax.Wrappers {
		if !strings.HasPrefix(w, "/") {
			t.Errorf("wrapper %q without leading /", w)
		}
	}
}
