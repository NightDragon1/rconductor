// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/profiles"
)

var minecraft = Syntax{StripPrefix: "/", MaxLength: 1000, Wrappers: []string{"execute"}}

// TestTokenPathMatching covers CMD-004 word for word.
func TestTokenPathMatching(t *testing.T) {
	list := []string{"whitelist add", "op"}
	for cmd, want := range map[string]bool{
		"whitelist add x":    true,
		"whitelist add x y":  true,
		"whitelist remove x": false,
		"op":                 true,
		"op x":               true,
		"opfoo":              false,
		"whitelist":          false,
		"whitelistadd x":     false,
	} {
		if got := AllowedByList(Tokens(cmd), list, false); got != want {
			t.Errorf("%q allowed = %v, want %v", cmd, got, want)
		}
	}
	// With "op" only, "op" alone is allowed but not "whitelist add x".
	if AllowedByList(Tokens("whitelist add x"), []string{"op"}, false) {
		t.Error("entry op allows whitelist")
	}
}

// TestWrapperBlocked covers CMD-005: execute … run say hi is a wrapper,
// also when execute is in the list.
func TestWrapperBlocked(t *testing.T) {
	cmd, err := Clean("execute as @a run say hi", minecraft)
	if err != nil {
		t.Fatal(err)
	}
	if !IsWrapper(Tokens(cmd), minecraft) {
		t.Error("execute not recognized as wrapper")
	}
	if !IsWrapper(Tokens("EXECUTE run op x"), minecraft) {
		t.Error("wrapper check must follow case_sensitive=false")
	}
	if IsWrapper(Tokens("say execute"), minecraft) {
		t.Error("only the command name counts")
	}
}

// TestWrapperNamespacePrefixB1 covers CMD-005: a wrapper stays a wrapper
// with any namespace prefix in front of its name, in the case rule of the
// profile; only the command name counts.
func TestWrapperNamespacePrefixB1(t *testing.T) {
	for in, want := range map[string]bool{
		"minecraft:execute run op x":   true,
		"Essentials:EXECUTE run op x":  true,
		"a:b:execute as @a run say hi": true,
		":execute run op x":            true,
		"/minecraft:execute run op x":  true,
		"say minecraft:execute":        false,
		"minecraft:say execute":        false,
		"execute: run op x":            false,
	} {
		cmd, err := Clean(in, minecraft)
		if err != nil {
			t.Fatal(err)
		}
		if got := IsWrapper(Tokens(cmd), minecraft); got != want {
			t.Errorf("%q wrapper = %v, want %v", in, got, want)
		}
	}
	strict := minecraft
	strict.CaseSensitive = true
	if IsWrapper(Tokens("foo:Execute run op x"), strict) {
		t.Error("case_sensitive=true: foo:Execute taken for execute")
	}
	if !IsWrapper(Tokens("foo:execute run op x"), strict) {
		t.Error("case_sensitive=true: foo:execute not recognized")
	}
	// A wrapper marked with a colon in the profile is compared whole.
	ns := Syntax{Wrappers: []string{"mod:run"}}
	if !IsWrapper(Tokens("mod:run x"), ns) || IsWrapper(Tokens("run x"), ns) {
		t.Error("wrapper with colon in the profile")
	}
	// Large input: many prefixes before the name.
	long := strings.Repeat(":", 10000) + "execute"
	if !IsWrapper([]string{long}, minecraft) {
		t.Error("long namespace chain not recognized")
	}
}

// TestClean covers CMD-006 and CAT-005: control characters and line breaks
// never reach the game server; a leading / is removed; the length limit
// applies after cleaning.
func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"say a\nop x":          "say aop x",
		"say a\r\nop x":        "say aop x",
		"/say hi":              "say hi",
		"  /list  ":            "list",
		"say\ta\x00b\x1b[31mc": "say ab[31mc",
		"say a op x":           "say aop x",
		"//say":                "/say",
	} {
		got, err := Clean(in, minecraft)
		if err != nil || got != want {
			t.Errorf("Clean(%q) = %q, %v; want %q", in, got, err, want)
		}
		if strings.ContainsAny(got, "\n\r\x00") {
			t.Errorf("control characters left in %q", got)
		}
	}
	for _, in := range []string{"", "  ", "/", "\n\n", strings.Repeat("a", 1001)} {
		if _, err := Clean(in, minecraft); !errors.Is(err, ErrInvalid) {
			t.Errorf("Clean(%.10q) = %v, want ErrInvalid", in, err)
		}
	}
	if _, err := Clean(strings.Repeat("ä", 1000), minecraft); err != nil {
		t.Error("length counts characters, not bytes")
	}
}

// TestCaseSensitivity covers CAT-005: with case_sensitive=false KICK is
// kick; with true it is not.
func TestCaseSensitivity(t *testing.T) {
	if !AllowedByList(Tokens("KICK steve"), []string{"kick"}, false) {
		t.Error("KICK not treated as kick")
	}
	if AllowedByList(Tokens("KICK steve"), []string{"kick"}, true) {
		t.Error("case-sensitive profile treats KICK as kick")
	}
}

func TestRateLimit(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	lim := DefaultLimits["moderator"] // 20 a minute, 5 at once
	for i := range 5 {
		if ok, _ := l.take("u", "s", lim, now); !ok {
			t.Fatalf("burst token %d refused", i+1)
		}
	}
	ok, wait := l.take("u", "s", lim, now)
	if ok || wait < 2900*time.Millisecond || wait > 3100*time.Millisecond {
		t.Errorf("sixth command: ok=%v wait=%v, want refused with about 3 s", ok, wait)
	}
	if ok, _ := l.take("u", "other", lim, now); !ok {
		t.Error("limit applies per server")
	}
	if ok, _ := l.take("u", "s", lim, now.Add(3*time.Second)); !ok {
		t.Error("token not refilled after 3 s")
	}
}

// TestCheckParams covers the rules of revision 2.4 candidate 19.
func TestCheckParams(t *testing.T) {
	lo, hi := 1, 10
	params := []profiles.Param{
		{Name: "player", Type: profiles.ParamPlayer},
		{Name: "mode", Type: profiles.ParamChoice, Values: []string{"day", "night"}},
		{Name: "count", Type: profiles.ParamNumber, Min: &lo, Max: &hi, Optional: true},
		{Name: "note", Type: profiles.ParamText, Optional: true},
	}
	mc := Syntax{Player: regexp.MustCompile(`^(?:^[A-Za-z0-9_]{1,16}$)$`)}
	cases := []struct {
		args       string
		name, prob string
	}{
		{"steve day", "", ""},
		{"steve day 5 hello there", "", ""},
		{"steve", "mode", "missing"},
		{"", "player", "missing"},
		{"steve dusk", "mode", "not one of day, night"},
		{"steve DAY", "", ""},
		{"steve day 0", "count", "out of range"},
		{"steve day 11", "count", "out of range"},
		{"steve day 2.5", "count", "not a whole number"},
		{"steve day x", "count", "not a whole number"},
		{"@a day", "player", "not a player name"},
		{"@e[type=player] day", "player", "not a player name"},
		{`"x" day`, "player", "not a player name"},
		{"Steve_The_Seventeen day", "player", "not a player name"},
	}
	for _, c := range cases {
		name, prob, ok := CheckParams(Tokens(c.args), params, mc)
		if name != c.name || prob != c.prob || ok != (c.prob == "") {
			t.Errorf("%q = %q %q %v, want %q %q", c.args, name, prob, ok, c.name, c.prob)
		}
	}
	if _, _, ok := CheckParams(Tokens("steve DAY"), params, Syntax{CaseSensitive: true, Player: mc.Player}); ok {
		t.Error("case-sensitive profile took DAY for day")
	}
	// Further arguments only after a text parameter: CMD-004 rules the
	// path, not the form of the arguments.
	player := []profiles.Param{{Name: "player", Type: profiles.ParamPlayer}}
	if name, prob, ok := CheckParams(Tokens("x y"), player, mc); ok || name != "" || prob != "unexpected argument" {
		t.Errorf("whitelist add x y = %q %q %v", name, prob, ok)
	}
	if _, prob, ok := CheckParams(Tokens("extra"), nil, mc); ok || prob != "unexpected argument" {
		t.Errorf("list extra = %q %v", prob, ok)
	}
}

// TestPlayerNameWithoutPattern: a profile without player_pattern still
// keeps selectors, quoted forms and control characters out.
func TestPlayerNameWithoutPattern(t *testing.T) {
	for name, want := range map[string]bool{
		"steve": true, "Ünal-2": true, "@a": false, "@e[type=player]": false,
		`"x"`: false, "'x'": false, "a`b": false, "a\x07b": false, "": false,
	} {
		if got := PlayerName(name, Syntax{}); got != want {
			t.Errorf("%q = %v, want %v", name, got, want)
		}
	}
}

// TestCatalogEntry: the longest catalog path that starts the command.
func TestCatalogEntry(t *testing.T) {
	catalog := []profiles.Command{{Path: "whitelist"}, {Path: "whitelist add"}, {Path: "say"}}
	if c, ok := CatalogEntry(Tokens("whitelist add steve"), catalog, false); !ok || c.Path != "whitelist add" {
		t.Errorf("entry = %+v %v", c, ok)
	}
	if _, ok := CatalogEntry(Tokens("sayhi"), catalog, false); ok {
		t.Error("sayhi matched say")
	}
}
