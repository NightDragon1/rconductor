// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package agenthub

import (
	"testing"

	"github.com/NightDragon1/rconductor/internal/agentproto"
)

// TestC72DisconnectBeforeNextSession: the end of an agent's last session is
// told while no new session of it can register, so the receiver never
// marks the data of a newer session stale (AGT-003).
func TestC72DisconnectBeforeNextSession(t *testing.T) {
	h := &Hub{sessions: map[string]*Session{}, caps: map[string][]string{}, hosts: map[string]agentproto.DockerHost{}}
	old := &Session{AgentID: "a1"}
	h.register(old)
	calls := 0
	h.OnDisconnect(func(id string) {
		calls++
		if l := h.lifeOf(id); l.TryLock() {
			l.Unlock()
			t.Error("a new session could register while the end of the old one is told")
		}
	})
	h.unregister(old)
	if calls != 1 {
		t.Fatalf("onDisconnect calls = %d, want 1", calls)
	}
	// Another agent is not held up meanwhile.
	h.OnDisconnect(func(string) {
		calls++
		if l := h.lifeOf("a2"); !l.TryLock() {
			t.Error("another agent waits for the end of a1")
		} else {
			l.Unlock()
		}
	})
	h.register(old)
	h.unregister(old)
	if calls != 2 {
		t.Fatalf("onDisconnect calls = %d, want 2", calls)
	}
	// A session that is no longer the last one tells nothing.
	h.register(&Session{AgentID: "a1"})
	h.unregister(old)
	if calls != 2 {
		t.Errorf("onDisconnect calls = %d after a stale end, want 2", calls)
	}
}
