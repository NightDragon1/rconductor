// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/profiles"
)

// TestRenderTellrawGolden: the fixed form of minecraft-tellraw (D-081,
// D-083). The root is an object with empty text and the parts in extra,
// each part only text; quotes, backslashes and control characters are
// escaped, § as \u00a7, so text that looks like JSON stays one string and
// the result stays one command.
func TestRenderTellrawGolden(t *testing.T) {
	for name, tc := range map[string]struct{ prefix, sender, text, want string }{
		"plain": {"[R]", "NightDragon", "hello",
			`tellraw @a {"text":"","extra":[{"text":"[R] "},{"text":"NightDragon: "},{"text":"hello"}]}`},
		"no prefix": {"", "NightDragon", "hello",
			`tellraw @a {"text":"","extra":[{"text":"NightDragon: "},{"text":"hello"}]}`},
		"quotes and JSON": {"[R]", "a", `"]},{"text":"x","clickEvent":{"action":"run_command","value":"/op a"}}`,
			`tellraw @a {"text":"","extra":[{"text":"[R] "},{"text":"a: "},{"text":"\"]},{\"text\":\"x\",\"clickEvent\":{\"action\":\"run_command\",\"value\":\"/op a\"}}"}]}`},
		"backslash": {"", "a", `C:\temp\`, `tellraw @a {"text":"","extra":[{"text":"a: "},{"text":"C:\\temp\\"}]}`},
		"control":   {"", "a", "x\ny\tz\x01", `tellraw @a {"text":"","extra":[{"text":"a: "},{"text":"x\ny\tz\u0001"}]}`},
		"section":   {"", "a", "§cred", `tellraw @a {"text":"","extra":[{"text":"a: "},{"text":"\u00a7cred"}]}`},
		"markup":    {"", "a", "<b>&</b>", `tellraw @a {"text":"","extra":[{"text":"a: "},{"text":"<b>&</b>"}]}`},
		"separator": {"", "a", "hi\u2028there", `tellraw @a {"text":"","extra":[{"text":"a: "},{"text":"hi\u2028there"}]}`},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := renderMessage(profiles.RenderMinecraftTellraw, tc.prefix, tc.sender, tc.text)
			if !ok || got != tc.want {
				t.Fatalf("render =\n%s\nwant\n%s", got, tc.want)
			}
			// One command: no line break, and the JSON holds exactly the
			// three kinds of parts with text only.
			if strings.ContainsAny(got, "\n\r") {
				t.Errorf("line break in %q", got)
			}
			var root struct {
				Text  string                       `json:"text"`
				Extra []map[string]json.RawMessage `json:"extra"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(got, "tellraw @a ")), &root); err != nil || root.Text != "" {
				t.Fatalf("root: %+v %v", root, err)
			}
			for _, part := range root.Extra {
				if _, ok := part["text"]; !ok || len(part) != 1 {
					t.Errorf("part %v: text only (D-083)", part)
				}
			}
			var last string
			_ = json.Unmarshal(root.Extra[len(root.Extra)-1]["text"], &last)
			if last != tc.text {
				t.Errorf("text came back as %q, want %q", last, tc.text)
			}
		})
	}
	if _, ok := renderMessage("other", "", "a", "b"); ok {
		t.Error("unknown render accepted")
	}
}

// TestSenderName: the sender is the pseudonym without §, control, bidi and
// zero-width characters, at most 32 characters (D-081).
func TestSenderName(t *testing.T) {
	for in, want := range map[string]string{
		"NightDragon":                    "NightDragon",
		"Night§cDragon":                  "NightcDragon",
		"evil\u202enoom":                 "evilnoom",
		"a\u2066b\u2069c\u200bd\ufeffe":  "abcde",
		"tab\there\x00":                  "tabhere",
		strings.Repeat("x", 40):          strings.Repeat("x", 32),
		"  spaced  ":                     "spaced",
		"\u00e4\u00f6\u00fc\u4e2d\u6587": "\u00e4\u00f6\u00fc\u4e2d\u6587",
	} {
		if got := senderName(in); got != want {
			t.Errorf("senderName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMessageText: only the message command with a text is rewritten.
func TestMessageText(t *testing.T) {
	m := &profiles.MessageCommand{Path: "say", Render: profiles.RenderMinecraftTellraw}
	for _, tc := range []struct {
		cmd, want string
		ok        bool
	}{
		{"say hello world", "hello world", true},
		{"SAY hi", "hi", true},
		{"say", "", false},
		{"say   ", "", false},
		{"sayhello", "", false},
		{"tell a b", "", false},
	} {
		got, ok := messageText(tc.cmd, m, false)
		if ok != tc.ok || got != tc.want {
			t.Errorf("messageText(%q) = %q %v, want %q %v", tc.cmd, got, ok, tc.want, tc.ok)
		}
	}
	if _, ok := messageText("SAY hi", m, true); ok {
		t.Error("case-sensitive profile matched SAY")
	}
	if _, ok := messageText("say hi", nil, false); ok {
		t.Error("matched without message_command")
	}
	// The path as the profile check normalizes it (space around it).
	if got, ok := messageText("say hi", &profiles.MessageCommand{Path: " say ", Render: profiles.RenderMinecraftTellraw}, false); !ok || got != "hi" {
		t.Errorf("path with space around it: %q %v", got, ok)
	}
}
