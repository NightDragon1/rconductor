// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// design74 is the example of Design 7.4, word for word.
const design74 = `schema: 1
id: minecraft-java
name: Minecraft (Java Edition)
match:
  images: ["itzg/minecraft-server*"]
protocol:
  type: source-rcon
  default_port: 25575
ports:
  game:
    - { port: 25565, proto: tcp, name: game }
syntax:
  case_sensitive: false
  strip_prefix: "/"
  max_length: 1000
  wrappers: [execute]
players:
  command: list
  pattern: "players online: (.*)$"
  separator: ", "
stop:
  command: stop
version:
  pattern: "Starting minecraft server version (\\S+)"
formatting: minecraft
catalog:
  - path: list
    description: Show players online
  - path: whitelist add
    description: Add a player to the whitelist
    params:
      - { name: player, type: player }
  - path: kick
    description: Kick a player from the server
    params:
      - { name: player, type: player }
      - { name: reason, type: text, optional: true }
  - path: time set
    description: Set the time of day
    params:
      - { name: time, type: choice, values: [day, night] }
  - path: say
    description: Message to all players
    params:
      - { name: text, type: text }
`

// TestDesignExample covers the first part of CAT-001: the Minecraft profile
// of Design 7.4 is imported without errors.
func TestDesignExample(t *testing.T) {
	p, err := Parse([]byte(design74))
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "minecraft-java" || p.Protocol.DefaultPort != 25575 || len(p.Catalog) != 5 {
		t.Errorf("profile = %+v", p)
	}
	if *p.Syntax.CaseSensitive || *p.Syntax.StripPrefix != "/" || p.Syntax.MaxLength != 1000 || p.Syntax.Wrappers[0] != "execute" {
		t.Errorf("syntax = %+v", p.Syntax)
	}
	if k := p.Catalog[2]; k.Path != "kick" || len(k.Params) != 2 || !k.Params[1].Optional {
		t.Errorf("kick = %+v", k)
	}
}

func TestShippedProfiles(t *testing.T) {
	entries, err := os.ReadDir("../../profiles")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !isProfileFile(e.Name()) {
			continue
		}
		data, err := os.ReadFile("../../profiles/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		p, err := Parse(data)
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		if p.ID+".yaml" != e.Name() {
			t.Errorf("%s has id %s", e.Name(), p.ID)
		}
		// Every shipped profile names its kind (CAT-007).
		if !strings.Contains(string(data), "\nkind: ") {
			t.Errorf("%s names no kind", e.Name())
		}
	}
}

// TestShippedProfilesAreListed keeps the list of shipped profiles in the
// user documentation complete (DOC-005).
func TestShippedProfilesAreListed(t *testing.T) {
	doc, err := os.ReadFile("../../docs/user/technical.md")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir("../../profiles")
	for _, e := range entries {
		if isProfileFile(e.Name()) && !strings.Contains(string(doc), "| `"+strings.TrimSuffix(e.Name(), ".yaml")+"` |") {
			t.Errorf("%s is not listed in docs/user/technical.md", e.Name())
		}
	}
}

// TestRejected covers the second part of CAT-001: a profile with a missing
// required field is refused with a message naming the field.
func TestRejected(t *testing.T) {
	base := design74
	cases := map[string]struct{ from, to, want string }{
		"missing id":            {"id: minecraft-java\n", "", "id is required"},
		"missing protocol":      {"  type: source-rcon\n", "", "protocol.type"},
		"missing default port":  {"  default_port: 25575\n", "", "default_port"},
		"wrong schema":          {"schema: 1", "schema: 2", "schema must be 1"},
		"missing schema":        {"schema: 1\n", "", "schema must be 1"},
		"missing name":          {"name: Minecraft (Java Edition)\n", "", "name"},
		"missing description":   {"    description: Kick a player from the server\n", "", "description is required"},
		"unknown field":         {"formatting: minecraft", "formating: minecraft", "formating"},
		"bad pattern":           {`"players online: (.*)$"`, `"players online: (?=x)"`, "players.pattern"},
		"text not last":         {"      - { name: reason, type: text, optional: true }", "      - { name: reason, type: text, optional: true }\n      - { name: extra, type: player, optional: true }", "text is allowed only as last"},
		"choice without values": {"type: choice, values: [day, night]", "type: choice", "choice needs values"},
		"duplicate path":        {"  - path: say", "  - path: list", "listed twice"},
		"unknown param type":    {"{ name: text, type: text }", "{ name: text, type: color }", "type must be"},
		"wrapper with space":    {"wrappers: [execute]", "wrappers: [\"execute run\"]", "single command name"},
		"bad player pattern":    {"wrappers: [execute]", "wrappers: [execute]\n  player_pattern: \"(?=x)\"", "syntax.player_pattern"},
		"unknown kind":          {"name: Minecraft (Java Edition)\n", "name: Minecraft (Java Edition)\nkind: special\n", "kind must be generic or specific"},
		"kind in upper case":    {"name: Minecraft (Java Edition)\n", "name: Minecraft (Java Edition)\nkind: Generic\n", "kind must be generic or specific"},
		"kind as a list":        {"name: Minecraft (Java Edition)\n", "name: Minecraft (Java Edition)\nkind: [generic]\n", "Profile.Kind"},
		"kind too long":         {"name: Minecraft (Java Edition)\n", "name: Minecraft (Java Edition)\nkind: " + strings.Repeat("g", 300) + "\n", "kind must be generic or specific"},
	}
	for name, tc := range cases {
		if !strings.Contains(base, tc.from) {
			t.Fatalf("%s: test text not found", name)
		}
		_, err := Parse([]byte(strings.Replace(base, tc.from, tc.to, 1)))
		var pe *Error
		if !errors.As(err, &pe) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want message with %q", name, err, tc.want)
		}
	}
	if _, err := Parse([]byte(strings.Repeat("#", MaxFileSize+1))); err == nil {
		t.Error("oversized file accepted")
	}
}

// TestKind covers the schema part of CAT-007: generic or specific, without
// kind generic; the shipped profiles name it.
func TestKind(t *testing.T) {
	p, err := Parse([]byte(design74))
	if err != nil || p.Kind != KindGeneric || KindOf(p) != KindGeneric {
		t.Errorf("without kind: %q %v", p.Kind, err)
	}
	p, err = Parse([]byte(strings.Replace(design74, "name: Minecraft (Java Edition)\n", "name: Minecraft (Java Edition)\nkind: specific\n", 1)))
	if err != nil || p.Kind != KindSpecific {
		t.Errorf("specific: %+v %v", p, err)
	}
	if KindOf(&Profile{}) != KindGeneric || KindOf(nil) != KindGeneric {
		t.Error("a stored profile without kind is not generic")
	}
	for file, want := range map[string]string{"minecraft-java.yaml": KindGeneric, "minecraft-gtnh.yaml": KindSpecific} {
		data, err := os.ReadFile("../../profiles/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "\nkind: "+want+"\n") {
			t.Errorf("%s does not name kind %s", file, want)
		}
	}
}

// TestHTTPAPIDialect covers D-071 in the profile: protocol http-api names
// a known dialect; http_api is only for that protocol; the profile
// describes no requests of its own.
func TestHTTPAPIDialect(t *testing.T) {
	ok := "schema: 1\nid: sf\nname: SF\nprotocol: { type: http-api, default_port: 7777 }\nhttp_api: { dialect: satisfactory }\n"
	p, err := Parse([]byte(ok))
	if err != nil || p.HTTPAPI == nil || p.HTTPAPI.Dialect != "satisfactory" {
		t.Fatalf("known dialect: %+v %v", p, err)
	}
	for name, tc := range map[string]struct{ text, want string }{
		"unknown dialect": {strings.Replace(ok, "satisfactory", "doom", 1), "http_api.dialect must be one of satisfactory"},
		"no dialect":      {strings.Replace(ok, "http_api: { dialect: satisfactory }\n", "", 1), "http_api.dialect must be one of"},
		"rcon with it":    {strings.Replace(ok, "http-api", "source-rcon", 1), "http_api is only for protocol.type http-api"},
		"own request":     {strings.Replace(ok, "dialect: satisfactory }", "dialect: satisfactory, path: /x }", 1), "path"},
	} {
		if _, err := Parse([]byte(tc.text)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	// profile check names the line.
	f := Check([]byte(strings.Replace(ok, "satisfactory", "doom", 1)))
	if len(f) == 0 || f[0].Line == 0 {
		t.Errorf("findings without a line: %+v", f)
	}
}

// TestSyntaxDefaults covers the defaults of CAT-005.
func TestSyntaxDefaults(t *testing.T) {
	p, err := Parse([]byte("schema: 1\nid: tiny\nname: Tiny\nprotocol: { type: source-rcon, default_port: 27015 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if *p.Syntax.CaseSensitive || *p.Syntax.StripPrefix != "/" || p.Syntax.MaxLength != DefaultMaxLength {
		t.Errorf("defaults = %+v", p.Syntax)
	}
	if NormalizePath("  whitelist   add ") != "whitelist add" {
		t.Error("path not normalized")
	}
}

// TestRequiredFieldsAreChecked comes from an exploratory check without a
// finding, so it has no state before a fix to fail on: a profile without a
// required field is refused, and the separator of the player list is optional.
func TestRequiredFieldsAreChecked(t *testing.T) {
	for name, missing := range map[string]string{
		"description": "    description: Show players online\n",
		"name":        "name: Minecraft (Java Edition)\n",
		"schema":      "schema: 1\n",
		"protocol":    "protocol:\n  type: source-rcon\n  default_port: 25575\n",
		"pattern":     "  pattern: \"players online: (.*)$\"\n",
	} {
		if !strings.Contains(design74, missing) {
			t.Fatalf("%s is not in the example", name)
		}
		if _, err := Parse([]byte(strings.Replace(design74, missing, "", 1))); err == nil {
			t.Errorf("profile without %s accepted", name)
		}
	}
	if _, err := Parse(nil); err == nil {
		t.Error("empty profile accepted")
	}
	if _, err := Parse([]byte(strings.Replace(design74, "  separator: \", \"\n", "", 1))); err != nil {
		t.Errorf("profile without a separator: %v", err)
	}
}

// TestSteamConnect covers the switch ports.steam_connect of UI-005: off
// by default, on when set, and only a boolean.
func TestSteamConnect(t *testing.T) {
	p, err := Parse([]byte(design74))
	if err != nil || p.Ports.SteamConnect {
		t.Fatalf("default: %v %v", p.Ports.SteamConnect, err)
	}
	on := strings.Replace(design74, "ports:\n  game:", "ports:\n  steam_connect: true\n  game:", 1)
	if p, err = Parse([]byte(on)); err != nil || !p.Ports.SteamConnect {
		t.Errorf("on: %v %v", err, p)
	}
	bad := strings.Replace(design74, "ports:\n  game:", "ports:\n  steam_connect: \"yes please\"\n  game:", 1)
	if _, err := Parse([]byte(bad)); err == nil {
		t.Error("a string as steam_connect accepted")
	}
}

// TestOrigin covers the origin fields of D-072: researched_version,
// sources and status (tested or untested, default untested).
func TestOrigin(t *testing.T) {
	p, err := Parse([]byte(design74))
	if err != nil || p.Status != StatusUntested || p.ResearchedVersion != "" || p.Sources == nil || len(p.Sources) != 0 {
		t.Fatalf("without origin: %+v %v", p, err)
	}
	with := strings.Replace(design74, "name: Minecraft (Java Edition)\n",
		"name: Minecraft (Java Edition)\nresearched_version: \"26.3\"\nsources:\n  - \"https://example.org/a\"\n  - \"profiles/x.yaml (list output)\"\nstatus: tested\n", 1)
	p, err = Parse([]byte(with))
	if err != nil || p.Status != StatusTested || p.ResearchedVersion != "26.3" || len(p.Sources) != 2 {
		t.Fatalf("with origin: %+v %v", p, err)
	}
	many := "sources:\n" + strings.Repeat("  - x\n", maxSources+1)
	for name, tc := range map[string]struct{ add, want string }{
		"unknown status":     {"status: works\n", "status must be tested or untested"},
		"status upper case":  {"status: Tested\n", "status must be tested or untested"},
		"status as list":     {"status: [tested]\n", "Profile.Status"},
		"long version":       {"researched_version: " + strings.Repeat("1", maxVersionLen+1) + "\n", "researched_version must be"},
		"control in version": {"researched_version: \"1.0\\n2\"\n", "researched_version must be"},
		"too many sources":   {many, "at most 20 sources"},
		"empty source":       {"sources: [\"  \"]\n", "sources[0] must be"},
		"long source":        {"sources: [" + strings.Repeat("s", maxSourceLen+1) + "]\n", "sources[0] must be"},
		"control in source":  {"sources: [\"a\\tb\"]\n", "sources[0] must be"},
		"sources as text":    {"sources: https://example.org\n", "sequence is expected"},
	} {
		text := strings.Replace(design74, "name: Minecraft (Java Edition)\n", "name: Minecraft (Java Edition)\n"+tc.add, 1)
		if _, err := Parse([]byte(text)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	// profile check names the line of the field.
	f := Check([]byte(strings.Replace(design74, "name: Minecraft (Java Edition)\n", "name: Minecraft (Java Edition)\nstatus: works\n", 1)))
	if len(f) == 0 || f[0].Line == 0 {
		t.Errorf("findings without a line: %+v", f)
	}
}

// TestShippedOrigin covers D-072 for the shipped profiles: each names its
// status, sources are fields and no longer comments, and the profiles run
// against a real server are tested.
func TestShippedOrigin(t *testing.T) {
	tested := map[string]bool{"minecraft-java": true, "minecraft-gtnh": true, "satisfactory": true}
	files, err := filepath.Glob("../../profiles/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no profiles: %v", err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "# source:") {
			t.Errorf("%s keeps source comments", file)
		}
		if !strings.Contains(string(data), "\nstatus: ") {
			t.Errorf("%s names no status", file)
		}
		p, err := Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if want := map[bool]string{true: StatusTested, false: StatusUntested}[tested[p.ID]]; p.Status != want {
			t.Errorf("%s: status %s, want %s", p.ID, p.Status, want)
		}
	}
}
