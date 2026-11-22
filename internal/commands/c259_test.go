// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/rcon/source"
)

// TestC259SeparatorInBuiltCommand (CMD-005, CMD-020): for those bound to
// lists, a separator the escaping put into the built command refuses the
// say; others get it as built.
func TestC259SeparatorInBuiltCommand(t *testing.T) {
	w := newWorld(t)
	syn := Syntax{Message: &profiles.MessageCommand{Path: "say", Render: profiles.RenderMinecraftTellraw}, Separators: []string{`\"`}}
	ctx := context.Background()
	var denied *DeniedError
	if _, err := w.svc.message(ctx, w.as("m"), w.server, `say a"b`, syn, true); !errors.As(err, &denied) || denied.Reason != ReasonInvalid {
		t.Errorf("bound: %v, want invalid", err)
	}
	if built, err := w.svc.message(ctx, w.as("admin"), w.server, `say a"b`, syn, false); err != nil || !strings.Contains(built, `a\"b`) {
		t.Errorf("not bound: %q, %v", built, err)
	}
}

// TestC259PrefixReadFails (CMD-020, D-082): a prefix that cannot be read
// gives no command to send: the say is refused.
func TestC259PrefixReadFails(t *testing.T) {
	w := newWorld(t)
	w.exec(`INSERT INTO server_groups (id, name, created_at, updated_at) VALUES ('ga', 'a', 't', 't')`)
	w.exec(`INSERT INTO server_group_members (server_group_id, server_id) VALUES ('ga', ?)`, w.server)
	syn := Syntax{Message: &profiles.MessageCommand{Path: "say", Render: profiles.RenderMinecraftTellraw}}
	w.exec(`ALTER TABLE settings RENAME TO settings_gone`)
	if built, err := w.svc.message(context.Background(), w.as("admin"), w.server, "say hi", syn, false); err == nil {
		t.Errorf("say with an unreadable prefix = %q", built)
	}
}

// TestC262ProtocolBound (D-081): without max_length the bound of the
// protocol caps the built command, to the byte.
func TestC262ProtocolBound(t *testing.T) {
	if tooLong(strings.Repeat("x", source.MaxCommand), Syntax{}) || !tooLong(strings.Repeat("x", source.MaxCommand+1), Syntax{}) {
		t.Error("protocol bound not exact")
	}
	// A max_length above the protocol does not lift its bound.
	if !tooLong(strings.Repeat("x", source.MaxCommand+1), Syntax{MaxLength: 10000}) {
		t.Error("max_length above the protocol lifts its bound")
	}
	if !tooLong("12345", Syntax{MaxLength: 4}) || tooLong("1234", Syntax{MaxLength: 4}) {
		t.Error("max_length not exact")
	}
}
