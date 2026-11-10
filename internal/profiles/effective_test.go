// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func shipped(t *testing.T, file string) *Profile {
	t.Helper()
	data, err := os.ReadFile("../../profiles/" + file)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(data)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return p
}

// TestEffectiveGTNHOverMinecraft covers D-074 with the shipped profiles:
// GTNH (specific) sets its syntax values itself, so above Minecraft
// (generic) they are GTNH's; the wrappers of Minecraft hold although GTNH
// lists none.
func TestEffectiveGTNHOverMinecraft(t *testing.T) {
	gtnh, mc := shipped(t, "minecraft-gtnh.yaml"), shipped(t, "minecraft-java.yaml")
	for _, f := range []string{FieldCaseSensitive, FieldStripPrefix, FieldMaxLength, FieldPlayerPattern} {
		if !gtnh.Sets(f) {
			t.Errorf("GTNH does not set %s itself", f)
		}
	}
	eff := Effective([]*Profile{gtnh, mc})
	if *eff.Syntax.StripPrefix != *gtnh.Syntax.StripPrefix || eff.Syntax.MaxLength != gtnh.Syntax.MaxLength ||
		*eff.Syntax.CaseSensitive != *gtnh.Syntax.CaseSensitive || eff.Syntax.PlayerPattern != gtnh.Syntax.PlayerPattern {
		t.Errorf("syntax %+v, want GTNH's", eff.Syntax)
	}
	for _, w := range mc.Syntax.Wrappers {
		if !slices.Contains(eff.Syntax.Wrappers, w) {
			t.Errorf("wrapper %q of Minecraft lifted", w)
		}
	}
	if eff.ID != gtnh.ID || eff.Version == nil || eff.Version.Pattern != gtnh.Version.Pattern {
		t.Errorf("top profile %q, version %+v", eff.ID, eff.Version)
	}
}

// TestEffectiveSpecificWithoutPrefix covers D-074: a specific profile
// without its own prefix rule above Minecraft takes Minecraft's.
func TestEffectiveSpecificWithoutPrefix(t *testing.T) {
	mc := shipped(t, "minecraft-java.yaml")
	pack, err := Parse([]byte("schema: 1\nid: pack\nname: Pack\nkind: specific\nprotocol: { type: source-rcon, default_port: 25575 }\nsyntax:\n  strip_prefix: \"\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	bare, err := Parse([]byte("schema: 1\nid: bare\nname: Bare\nkind: specific\nprotocol: { type: source-rcon, default_port: 25575 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := *Effective([]*Profile{bare, pack}).Syntax.StripPrefix; got != "" {
		t.Errorf("prefix %q, want the lower profile's empty one", got)
	}
	// The same after the rules went through the database as JSON.
	var stored Profile
	b, _ := json.Marshal(bare)
	if err := json.Unmarshal(b, &stored); err != nil || stored.Set == nil {
		t.Fatalf("stored set = %v %v, want an empty list", stored.Set, err)
	}
	if got := *Effective([]*Profile{&stored, pack}).Syntax.StripPrefix; got != "" {
		t.Errorf("stored: prefix %q, want the lower profile's empty one", got)
	}
	// Rules stored before the fields were recorded count as setting all.
	legacy := *pack
	legacy.Set = nil
	if got := *Effective([]*Profile{&legacy, mc}).Syntax.StripPrefix; got != "" {
		t.Errorf("legacy: prefix %q, want its own", got)
	}
	if got := Effective([]*Profile{bare, mc}); *got.Syntax.StripPrefix != *mc.Syntax.StripPrefix || got.Syntax.MaxLength != mc.Syntax.MaxLength {
		t.Errorf("prefix %q length %d, want Minecraft's", *got.Syntax.StripPrefix, got.Syntax.MaxLength)
	}
}

// TestEffectiveFieldByField covers D-074 with made-up profiles: each field
// comes from the topmost profile that sets it, else the default; the
// wrappers of all profiles hold, none lifts one.
func TestEffectiveFieldByField(t *testing.T) {
	top, err := Parse([]byte("schema: 1\nid: top\nname: Top\nkind: specific\nprotocol: { type: source-rcon, default_port: 1 }\nsyntax:\n  case_sensitive: true\n  wrappers: [run]\n"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := Parse([]byte("schema: 1\nid: base\nname: Base\nprotocol: { type: source-rcon, default_port: 2 }\nports:\n  game: [{ port: 5, proto: udp, name: game }]\nsyntax:\n  case_sensitive: false\n  strip_prefix: \"!\"\n  separators: [\";\"]\n  wrappers: [execute]\nstop: { command: quit }\n"))
	if err != nil {
		t.Fatal(err)
	}
	eff := Effective([]*Profile{top, base})
	if !*eff.Syntax.CaseSensitive {
		t.Error("case sensitivity of the top profile lost")
	}
	if *eff.Syntax.StripPrefix != "!" || !slices.Equal(eff.Syntax.Separators, []string{";"}) {
		t.Errorf("prefix %q separators %v, want the base profile's", *eff.Syntax.StripPrefix, eff.Syntax.Separators)
	}
	if eff.Syntax.MaxLength != DefaultMaxLength {
		t.Errorf("length %d, want the default", eff.Syntax.MaxLength)
	}
	if len(eff.Ports.Game) != 1 || eff.Ports.Game[0].Port != 5 || eff.Stop == nil || eff.Stop.Command != "quit" {
		t.Errorf("ports %v stop %+v, want the base profile's", eff.Ports.Game, eff.Stop)
	}
	if !slices.Equal(eff.Syntax.Wrappers, []string{"run", "execute"}) {
		t.Errorf("wrappers %v, want both", eff.Syntax.Wrappers)
	}
	if eff.Protocol.DefaultPort != 1 || eff.ID != "top" {
		t.Errorf("protocol and id from the top: %+v %q", eff.Protocol, eff.ID)
	}
	// Order matters: the other way round the base profile wins.
	if rev := Effective([]*Profile{base, top}); *rev.Syntax.CaseSensitive {
		t.Error("case sensitivity of the lower profile won")
	}
	if Effective(nil) != nil {
		t.Error("no profiles, but rules")
	}
}

// TestMergeCatalogs covers D-074: the catalog is the union; with the same
// path the upper profile's entry wins.
func TestMergeCatalogs(t *testing.T) {
	upper := []Command{{Path: "list", Description: "upper"}, {Path: "quests  reload", Description: "mod"}}
	lower := []Command{{Path: "list", Description: "lower"}, {Path: "say", Description: "say"}}
	got := MergeCatalogs([][]Command{upper, lower})
	if len(got) != 3 || got[0].Description != "upper" || got[2].Path != "say" {
		t.Errorf("catalog = %+v", got)
	}
}
