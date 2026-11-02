// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"errors"
	"strings"
	"testing"
	"time"
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
