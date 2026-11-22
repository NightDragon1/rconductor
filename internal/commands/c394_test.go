// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"strings"
	"testing"
)

// TestC394SayThroughThePipeline (CMD-005, CMD-020, D-081): a say through
// Send for admin, moderator and supporter goes out as tellraw with the
// sender; a separator the escaping makes refuses it only for those bound
// to lists; without message_command the say goes out as typed; the
// multi-server path builds it the same way.
func TestC394SayThroughThePipeline(t *testing.T) {
	w := newWorld(t)
	w.exec(`INSERT INTO command_list_entries (id, command_list_id, path, passable) VALUES ('esay', 'l', 'say', 1)`)
	w.c.Invalidate()
	last := func() string {
		t.Helper()
		got := w.gameReceived()
		if len(got) == 0 {
			t.Fatal("nothing sent")
		}
		return got[len(got)-1]
	}
	for _, who := range []string{"admin", "m", "sup"} {
		if _, err := w.send(who, "say hi"); err != nil {
			t.Fatalf("%s: %v", who, err)
		}
		if got := last(); !strings.HasPrefix(got, "tellraw @a ") || !strings.Contains(got, `"text":"`+who+`: "`) {
			t.Errorf("%s: sent %q", who, got)
		}
	}
	// The escaping of a quote makes \" in the built command.
	w.exec(`UPDATE game_profiles SET rules = json_set(rules, '$.syntax.separators', json_array('\"'))`)
	w.c.Invalidate()
	if _, err := w.send("m", `say a"b`); reason(err) != ReasonInvalid {
		t.Errorf("bound with a separator in the built command: %v", err)
	}
	if _, err := w.send("admin", `say a"b`); err != nil {
		t.Errorf("admin: %v", err)
	}
	w.exec(`UPDATE game_profiles SET rules = json_remove(json_set(rules, '$.syntax.separators', json('[]')), '$.message_command')`)
	w.c.Invalidate()
	if _, err := w.send("m", "say plain"); err != nil || last() != "say plain" {
		t.Errorf("without message_command: %q, %v", last(), err)
	}
	w.exec(`UPDATE game_profiles SET rules = json_set(rules, '$.message_command', json('{"path":"say","render":"minecraft-tellraw"}'))`)
	w.c.Invalidate()
	if _, items, err := w.svc.Multi(context.Background(), w.as("admin"), []string{w.server}, "say all"); err != nil || len(items) != 1 || items[0].Result != ResultOK {
		t.Fatalf("multi: %+v, %v", items, err)
	}
	if got := last(); !strings.HasPrefix(got, "tellraw @a ") || !strings.Contains(got, `"text":"all"`) {
		t.Errorf("multi say: %q", got)
	}
}
