// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
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

// TestExplicitValuesCountAsSet covers D-074: a field the file writes counts
// as set by its key, also with false, 0, "" or []; so a specific profile
// can set steam_connect false over a generic true, and an empty formatting
// over a generic profile's.
func TestExplicitValuesCountAsSet(t *testing.T) {
	generic, err := Parse([]byte("schema: 1\nid: g\nname: G\nprotocol: { type: source-rcon, default_port: 1 }\nports:\n  steam_connect: true\nsyntax:\n  separators: [\";\"]\nformatting: minecraft\n"))
	if err != nil {
		t.Fatal(err)
	}
	specific, err := Parse([]byte("schema: 1\nid: s\nname: S\nkind: specific\nprotocol: { type: source-rcon, default_port: 1 }\nports:\n  steam_connect: false\nsyntax:\n  separators: []\n  player_pattern: \"\"\nformatting: \"\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{FieldSteamConnect, FieldSeparators, FieldPlayerPattern, FieldFormatting} {
		if !specific.Sets(f) {
			t.Errorf("explicit %s not counted as set", f)
		}
	}
	eff := Effective([]*Profile{specific, generic})
	if eff.Ports.SteamConnect || eff.Formatting != "" {
		t.Errorf("steam_connect %v formatting %q, want the specific profile's false and empty",
			eff.Ports.SteamConnect, eff.Formatting)
	}
	// Separators hold from every profile; an empty list lifts none (D-076).
	if !slices.Equal(eff.Syntax.Separators, []string{";"}) {
		t.Errorf("separators %v, want the generic profile's", eff.Syntax.Separators)
	}
	// Absent keys stay unset, and the generic values hold.
	bare, err := Parse([]byte("schema: 1\nid: b\nname: B\nkind: specific\nprotocol: { type: source-rcon, default_port: 1 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(bare.Set) != 0 {
		t.Errorf("a profile without fields sets %v", bare.Set)
	}
	if eff := Effective([]*Profile{bare, generic}); !eff.Ports.SteamConnect || len(eff.Syntax.Separators) != 1 || eff.Formatting != "minecraft" {
		t.Errorf("generic values lost: %+v %v %q", eff.Ports, eff.Syntax.Separators, eff.Formatting)
	}
}

// TestPresenceThroughAnchors covers D-074 for own profiles that use
// anchors, aliases or merge keys: the keys they bring count as written.
func TestPresenceThroughAnchors(t *testing.T) {
	text := "schema: 1\nid: a\nname: A\nprotocol: { type: source-rcon, default_port: 1 }\n" +
		"stop: &s { command: quit }\n" +
		"ports: &p\n  steam_connect: false\n" +
		"syntax:\n  <<: { case_sensitive: false }\n  strip_prefix: \"\"\n"
	p, err := Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{FieldStop, FieldSteamConnect, FieldCaseSensitive, FieldStripPrefix} {
		if !p.Sets(f) {
			t.Errorf("%s not counted as set: %v", f, p.Set)
		}
	}
	if p.Sets(FieldMaxLength) || p.Sets(FieldGamePorts) {
		t.Errorf("absent fields counted as set: %v", p.Set)
	}
}

// TestNullRefused covers D-074: null, also a key without a value, is
// refused with its line; an empty value is written as [] or "".
func TestNullRefused(t *testing.T) {
	base := "schema: 1\nid: n\nname: N\nprotocol: { type: source-rcon, default_port: 1 }\n"
	for name, add := range map[string]string{
		"null":        "stop: null\n",
		"tilde":       "formatting: ~\n",
		"empty value": "syntax:\n  separators:\n",
		"in a list":   "syntax:\n  wrappers: [null]\n",
		"nested":      "ports:\n  steam_connect: null\n",
	} {
		_, err := Parse([]byte(base + add))
		if err == nil || !strings.Contains(err.Error(), "null not allowed") {
			t.Errorf("%s: %v, want null not allowed", name, err)
			continue
		}
		f := Check([]byte(base + add))
		if len(f) == 0 || f[0].Line < 5 {
			t.Errorf("%s: findings %+v, want the line of the null", name, f)
		}
	}
	if _, err := Parse([]byte(base + "syntax:\n  separators: []\n  player_pattern: \"\"\n")); err != nil {
		t.Errorf("explicit empty values: %v", err)
	}
}

// TestShippedProfilesWithoutNull covers D-074: no shipped profile holds a
// null.
func TestShippedProfilesWithoutNull(t *testing.T) {
	entries, err := os.ReadDir("../../profiles")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile("../../profiles/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if f := Check(data); len(f) > 0 {
			t.Errorf("%s: %+v", e.Name(), f)
		}
	}
}

// TestSeparatorsOfAllProfilesC20 covers D-076: command separators of every
// profile of a server hold; an upper profile with separators: [] lifts
// none.
func TestSeparatorsOfAllProfilesC20(t *testing.T) {
	gtnh, src := shipped(t, "minecraft-gtnh.yaml"), shipped(t, "source-engine.yaml")
	eff := Effective([]*Profile{gtnh, src})
	for _, sep := range src.Syntax.Separators {
		if !slices.Contains(eff.Syntax.Separators, sep) {
			t.Errorf("separator %q of source-engine lifted: %v", sep, eff.Syntax.Separators)
		}
	}
}

// TestKeyFormsCountAsSetC21 covers D-074: a key counts as written in every
// YAML form that decodes to it: quoted, tagged, anchored or explicit.
func TestKeyFormsCountAsSetC21(t *testing.T) {
	head := "schema: 1\nid: k\nname: K\nprotocol: { type: source-rcon, default_port: 1 }\n"
	for name, body := range map[string]string{
		"double quoted": "syntax:\n  \"separators\": [\";\"]\n",
		"single quoted": "syntax:\n  'separators': [\";\"]\n",
		"quoted parent": "\"syntax\":\n  separators: [\";\"]\n",
		"tagged":        "syntax:\n  !!str separators: [\";\"]\n",
		"anchored":      "syntax:\n  &k separators: [\";\"]\n",
		"explicit":      "syntax:\n  ? separators\n  : [\";\"]\n",
		"json":          "{\"schema\": 1, \"id\": \"k\", \"name\": \"K\", \"protocol\": {\"type\": \"source-rcon\", \"default_port\": 1}, \"syntax\": {\"separators\": [\";\"]}}\n",
	} {
		text := head + body
		if name == "json" {
			text = body
		}
		p, err := Parse([]byte(text))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !p.Sets(FieldSeparators) {
			t.Errorf("%s: separators not counted as set: %v", name, p.Set)
		}
	}
}

// TestLegacyRulesDoNotWinWithDefaultsC22 covers D-074: rules stored before
// the fields were recorded win a field only with a value of their own,
// never with a default.
func TestLegacyRulesDoNotWinWithDefaultsC22(t *testing.T) {
	legacy := shipped(t, "minecraft-gtnh.yaml")
	legacy.Set = nil
	lower, err := Parse([]byte("schema: 1\nid: l\nname: L\nprotocol: { type: source-rcon, default_port: 1 }\nsyntax:\n  strip_prefix: \"!\"\n  max_length: 200\n"))
	if err != nil {
		t.Fatal(err)
	}
	eff := Effective([]*Profile{legacy, lower})
	if *eff.Syntax.StripPrefix != "!" || eff.Syntax.MaxLength != 200 {
		t.Errorf("prefix %q length %d, want the lower profile's", *eff.Syntax.StripPrefix, eff.Syntax.MaxLength)
	}
	if eff.Syntax.PlayerPattern != legacy.Syntax.PlayerPattern {
		t.Errorf("player pattern %q, want the legacy rules' own", eff.Syntax.PlayerPattern)
	}
}

// TestCommandsPrintableC24 covers CAT-005: the players and stop commands
// of a profile hold no control characters.
func TestCommandsPrintableC24(t *testing.T) {
	head := "schema: 1\nid: c\nname: C\nprotocol: { type: source-rcon, default_port: 1 }\n"
	for name, body := range map[string]string{
		"players": "players:\n  command: \"list\\nop me\"\n  pattern: \"(.*)\"\n",
		"stop":    "stop:\n  command: \"stop\\u0000\"\n",
	} {
		if _, err := Parse([]byte(head + body)); err == nil || !strings.Contains(err.Error(), "printable") {
			t.Errorf("%s: %v, want refused", name, err)
		}
	}
}
