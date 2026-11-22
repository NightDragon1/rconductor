// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"testing"

	"github.com/NightDragon1/rconductor/internal/commands/sayconf"
)

// TestC230SayPrefixChoice covers CMD-020 and D-082 in the pipeline: the
// prefix of the first group by name in byte order (B before a), a deleted
// group counts no more, a quote in the prefix stays inside the JSON text,
// and a stored prefix that is no text is no prefix.
func TestC230SayPrefixChoice(t *testing.T) {
	w := newWorld(t)
	for i, g := range []struct{ id, name, prefix string }{{"ga", "a", `"[a]"`}, {"gb", "B", `"[\"B\"]"`}} {
		w.exec(`INSERT INTO server_groups (id, name, created_at, updated_at) VALUES (?, ?, 't', 't')`, g.id, g.name)
		w.exec(`INSERT INTO server_group_members (server_group_id, server_id) VALUES (?, ?)`, g.id, w.server)
		w.exec(`INSERT INTO settings (id, key, value, scope_type, scope_id, updated_at) VALUES (?, ?, ?, 'server_group', ?, 't')`,
			"p"+string(rune('0'+i)), sayconf.Setting, g.prefix, g.id)
	}
	last := func() string {
		t.Helper()
		got := w.gameReceived()
		if len(got) == 0 {
			t.Fatal("nothing sent")
		}
		return got[len(got)-1]
	}
	if _, err := w.send("admin", "say hi"); err != nil {
		t.Fatal(err)
	}
	if got := last(); got != `tellraw @a {"text":"","extra":[{"text":"[\"B\"] "},{"text":"admin: "},{"text":"hi"}]}` {
		t.Errorf("byte order and quote: %s", got)
	}
	w.exec(`UPDATE server_groups SET deleted_at = 't' WHERE id = 'gb'`)
	if _, err := w.send("admin", "say hi"); err != nil {
		t.Fatal(err)
	}
	if got := last(); got != `tellraw @a {"text":"","extra":[{"text":"[a] "},{"text":"admin: "},{"text":"hi"}]}` {
		t.Errorf("deleted group still counts: %s", got)
	}
	w.exec(`UPDATE settings SET value = '123' WHERE scope_id = 'ga'`)
	if _, err := w.send("admin", "say hi"); err != nil {
		t.Fatal(err)
	}
	if got := last(); got != `tellraw @a {"text":"","extra":[{"text":"admin: "},{"text":"hi"}]}` {
		t.Errorf("a stored prefix that is no text: %s", got)
	}
}
