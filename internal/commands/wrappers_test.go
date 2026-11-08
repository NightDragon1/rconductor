// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"errors"
	"testing"
)

// TestMinecraftWrappersOfTheProfile covers CMD-005 with the shipped
// minecraft-java profile: function, return and schedule run other
// commands and are refused for moderators like execute, also with the
// namespace prefix.
func TestMinecraftWrappersOfTheProfile(t *testing.T) {
	w := newWorld(t)
	for _, cmd := range []string{
		"execute as @a run op m", "function ns:give_all", "return run op m",
		"schedule function ns:later 1t", "minecraft:function ns:x", "MINECRAFT:RETURN run op m",
	} {
		var denied *DeniedError
		if _, err := w.svc.Send(context.Background(), w.as("m"), w.server, cmd); !errors.As(err, &denied) || denied.Reason != ReasonWrapperBlocked {
			t.Errorf("%q: %v, want wrapper_blocked", cmd, err)
		}
	}
	// Admins send them as they are.
	if _, err := w.svc.Send(context.Background(), w.as("sa"), w.server, "function ns:x"); err != nil {
		t.Errorf("server admin: %v", err)
	}
}
