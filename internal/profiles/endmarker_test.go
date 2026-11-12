// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"os"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/rcon/source"
)

const markerBase = "schema: 1\nid: g\nname: G\nprotocol: { type: source-rcon, default_port: 1 }\n"

// TestO8EndMarkerCheck covers PROT-001 and CAT-006: source_rcon.end_marker
// takes the known end markers only, is required inside the block and is
// only for protocol source-rcon; the profile check reports it with its
// line.
func TestO8EndMarkerCheck(t *testing.T) {
	for _, m := range source.EndMarkers() {
		p, err := Parse([]byte(markerBase + "source_rcon:\n  end_marker: " + m + "\n"))
		if err != nil || p.SourceRCON == nil || p.SourceRCON.EndMarker != m || !p.Sets(FieldEndMarker) {
			t.Errorf("%s: %+v, %v", m, p, err)
		}
	}
	for name, tc := range map[string]struct{ file, want string }{
		"unknown":   {markerBase + "source_rcon:\n  end_marker: never\n", "source_rcon.end_marker must be one of empty_response, none"},
		"empty":     {markerBase + "source_rcon:\n  end_marker: \"\"\n", "source_rcon.end_marker must be one of"},
		"missing":   {markerBase + "source_rcon: {}\n", "source_rcon.end_marker must be one of"},
		"typo":      {markerBase + "source_rcon:\n  endmarker: none\n", "unknown field"},
		"long":      {markerBase + "source_rcon:\n  end_marker: " + strings.Repeat("n", 5000) + "\n", "source_rcon.end_marker must be one of"},
		"http-api":  {"schema: 1\nid: g\nname: G\nprotocol: { type: http-api, default_port: 1 }\nhttp_api: { dialect: satisfactory }\nsource_rcon: { end_marker: none }\n", "source_rcon is only for protocol.type source-rcon"},
		"null":      {markerBase + "source_rcon:\n  end_marker:\n", ""},
		"not a map": {markerBase + "source_rcon: none\n", ""},
	} {
		_, err := Parse([]byte(tc.file))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
		if f := Check([]byte(tc.file)); len(f) == 0 {
			t.Errorf("%s: profile check passes", name)
		}
	}
	if f := Check([]byte(markerBase + "source_rcon:\n  end_marker: never\n")); len(f) != 1 || f[0].Line != 6 {
		t.Errorf("profile check findings %+v, want line 6", f)
	}
	p, err := Parse([]byte(markerBase))
	if err != nil || p.SourceRCON != nil || p.Sets(FieldEndMarker) {
		t.Errorf("without the field: %+v, %v", p.SourceRCON, err)
	}
}

// TestO8EndMarkerEffective: the end marker comes from the topmost profile
// that sets it (D-074), so a specific profile without it keeps that of its
// game, and no profile setting it leaves the default (nil).
func TestO8EndMarkerEffective(t *testing.T) {
	game, err := Parse([]byte(markerBase + "source_rcon:\n  end_marker: none\n"))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := Parse([]byte("schema: 1\nid: m\nname: M\nkind: specific\nprotocol: { type: source-rcon, default_port: 1 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if eff := Effective([]*Profile{mod, game}); eff.SourceRCON == nil || eff.SourceRCON.EndMarker != source.EndMarkerNone {
		t.Errorf("end marker %+v, want the game's none", eff.SourceRCON)
	}
	if eff := Effective([]*Profile{mod}); eff.SourceRCON != nil {
		t.Errorf("end marker %+v, want the default", eff.SourceRCON)
	}
	legacy := *game
	legacy.Set = nil
	if eff := Effective([]*Profile{mod, &legacy}); eff.SourceRCON == nil || eff.SourceRCON.EndMarker != source.EndMarkerNone {
		t.Errorf("legacy rules: end marker %+v, want none", eff.SourceRCON)
	}
}

// TestO8FactorioProfile: the shipped Factorio profile sets the end marker
// none, because Factorio refuses packets of type 0.
func TestO8FactorioProfile(t *testing.T) {
	data, err := os.ReadFile("../../profiles/factorio.yaml")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.SourceRCON == nil || p.SourceRCON.EndMarker != source.EndMarkerNone {
		t.Errorf("factorio end marker %+v, want none", p.SourceRCON)
	}
}
