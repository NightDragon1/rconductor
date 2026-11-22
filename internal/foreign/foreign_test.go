// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package foreign

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestText(t *testing.T) {
	for name, tc := range map[string]struct {
		in, want string
		max      int
		cut      bool
	}{
		"html stays text":   {"<b>hi</b><script>alert(1)</script>", "<b>hi</b><script>alert(1)</script>", 100, false},
		"color codes stay":  {"§cRed §zodd", "§cRed §zodd", 100, false},
		"line ends":         {"a\r\nb\rc\n\td", "a\nb\nc\n\td", 100, false},
		"controls dropped":  {"a\x00b\x1b[31mc\x7fd\u0085e", "ab[31mcde", 100, false},
		"bidi kept":         {"x\u202Ey", "x\u202Ey", 100, false},
		"invalid utf-8":     {"a\xffb", "a\uFFFDb", 100, false},
		"cut at boundary":   {"aä€", "aä", 4, true},
		"exactly the limit": {"abcd", "abcd", 4, false},
	} {
		got, cut := Text(tc.in, tc.max)
		if got != tc.want || cut != tc.cut {
			t.Errorf("%s: Text(%q) = %q, %v; want %q, %v", name, tc.in, got, cut, tc.want, tc.cut)
		}
		if len(got) > tc.max || !utf8.ValidString(got) {
			t.Errorf("%s: %q over the limit or not UTF-8", name, got)
		}
	}
}

func TestName(t *testing.T) {
	for name, tc := range map[string]struct {
		in, want string
		max      int
	}{
		"script stays text": {"<script>alert(1)</script>", "<script>alert(1)</script>", 64},
		"bidi override":     {"evil\u202Egnp.exe", "evilgnp.exe", 64},
		"zero width":        {"Ste\u200Bve\uFEFF", "Steve", 64},
		"controls":          {" \tA\x00l\x1bex\n ", "Alex", 64},
		"invalid utf-8":     {"a\xffb", "a\uFFFDb", 64},
		"long":              {strings.Repeat("ä", 70), strings.Repeat("ä", 64), 64},
	} {
		if got := Name(tc.in, tc.max); got != tc.want {
			t.Errorf("%s: Name(%q) = %q, want %q", name, tc.in, got, tc.want)
		}
	}
}

// TestBlank: space, control, format and other invisible characters alone
// are no text; any visible character is one (SEC-006).
func TestBlank(t *testing.T) {
	for _, s := range []string{"", " \n\t\r", "\u200B\u200C\u200D\u2060\uFEFF", "\u202E\u2066\u2069\u061C",
		"\uFE0F\U000E0100", "\u034F\u115F\u1160\u17B4\u17B5\u2800\u3164\uFFA0", "\u00AD", "\x00\x1b"} {
		if !Blank(s) {
			t.Errorf("Blank(%q) = false", s)
		}
	}
	for _, s := range []string{"a", "\u200Bx", " . ", "\u00e9", "\U0001F600", "\u3000\u4e00"} {
		if Blank(s) {
			t.Errorf("Blank(%q) = true", s)
		}
	}
}
