// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"io/fs"
	"slices"
	"strings"
	"testing"

	shippedprofiles "github.com/NightDragon1/rconductor/profiles"
)

// TestMinecraftBaseProfilesSay covers D-083: every Minecraft profile that
// can stand alone or be the lowest profile of a server sets message_command
// { path: say, render: minecraft-tellraw }; layers and add-ons do not. A
// base profile is one whose catalog has say; a new one must be added here
// on purpose.
func TestMinecraftBaseProfilesSay(t *testing.T) {
	names, err := fs.Glob(shippedprofiles.FS(), "minecraft-*.yaml")
	if err != nil || len(names) == 0 {
		t.Fatalf("profiles: %v %v", names, err)
	}
	var bases []string
	for _, name := range names {
		data, err := fs.ReadFile(shippedprofiles.FS(), name)
		if err != nil {
			t.Fatal(err)
		}
		p, err := Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		base := slices.ContainsFunc(p.Catalog, func(c Command) bool { return NormalizePath(c.Path) == "say" })
		m := p.MessageCommand
		switch {
		case base:
			bases = append(bases, strings.TrimSuffix(name, ".yaml"))
			if m == nil || m.Path != "say" || m.Render != RenderMinecraftTellraw || !p.Sets(FieldMessageCommand) {
				t.Errorf("%s: base profile without message_command { path: say, render: minecraft-tellraw }: %+v", name, m)
			}
		case m != nil:
			t.Errorf("%s: layer or add-on sets message_command", name)
		}
	}
	slices.Sort(bases)
	if want := []string{"minecraft-gtnh", "minecraft-java", "minecraft-java-legacy"}; !slices.Equal(bases, want) {
		t.Errorf("base profiles %v, want %v (D-083)", bases, want)
	}
}

// TestMessageCommandChecked: message_command takes one command word and a
// render of the list (D-081).
func TestMessageCommandChecked(t *testing.T) {
	base := "schema: 1\nid: t\nname: T\nprotocol: { type: source-rcon, default_port: 25575 }\n"
	for name, tc := range map[string]struct {
		yaml string
		ok   bool
	}{
		"valid":          {"message_command: { path: say, render: minecraft-tellraw }\n", true},
		"unknown render": {"message_command: { path: say, render: html }\n", false},
		"two words":      {"message_command: { path: say all, render: minecraft-tellraw }\n", false},
		"empty path":     {"message_command: { path: \"\", render: minecraft-tellraw }\n", false},
		"long path":      {"message_command: { path: " + strings.Repeat("s", 33) + ", render: minecraft-tellraw }\n", false},
		"slash":          {"message_command: { path: /say, render: minecraft-tellraw }\n", false},
		"format char":    {"message_command: { path: \"s\\u200Bay\", render: minecraft-tellraw }\n", false},
	} {
		_, err := Parse([]byte(base + tc.yaml))
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok %v", name, err, tc.ok)
		}
	}
}

// TestMessageCommandStoredNormalized: the path is stored as the rights
// check normalizes it (CMD-020).
func TestMessageCommandStoredNormalized(t *testing.T) {
	p, err := Parse([]byte("schema: 1\nid: t\nname: T\nprotocol: { type: source-rcon, default_port: 25575 }\nmessage_command: { path: \"  say  \", render: minecraft-tellraw }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.MessageCommand == nil || p.MessageCommand.Path != "say" {
		t.Errorf("path = %+v, want say", p.MessageCommand)
	}
}

// TestC221RenderNeedsItsProtocol (D-081): a render builds its command for
// one protocol; a profile of another protocol cannot name it.
func TestC221RenderNeedsItsProtocol(t *testing.T) {
	_, err := Parse([]byte("schema: 1\nid: t\nname: T\nprotocol: { type: http-api, default_port: 8080 }\nmessage_command: { path: say, render: minecraft-tellraw }\n"))
	if err == nil || !strings.Contains(err.Error(), "needs protocol.type source-rcon") {
		t.Errorf("tellraw over another protocol: %v", err)
	}
}
