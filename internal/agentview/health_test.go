// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package agentview

import (
	"context"
	"testing"

	"github.com/NightDragon1/rconductor/internal/agentproto"
)

// TestHealth covers UI-021 and UI-011: a running container carries the
// result of its healthcheck to the admins; without one, from an agent that
// does not send it, or with a value outside the known ones the state alone
// counts; a stopped container has no health; roles without agent data get
// no container part.
func TestHealth(t *testing.T) {
	f := newInsightFixture(t)
	ctx := context.Background()
	for _, tc := range []struct{ state, health, want string }{
		{"running", "healthy", "healthy"},
		{"running", "starting", "starting"},
		{"running", "unhealthy", "unhealthy"},
		{"running", "", ""},
		{"running", "great\u202e", ""},
		{"exited", "unhealthy", ""},
		{"paused", "healthy", ""},
	} {
		f.data.Snapshot("a1", agentproto.Snapshot{Containers: []agentproto.Container{{ID: "c", Name: "mc", State: tc.state, Health: tc.health}}})
		c := f.view.RuntimeFor(ctx, "s1", LevelServerAdmin).Container
		if c == nil || c.Health != tc.want || c.State != tc.state {
			t.Errorf("%s/%q: container = %+v, want health %q", tc.state, tc.health, c, tc.want)
		}
		if rt := f.view.RuntimeFor(ctx, "s1", LevelPlayers); rt.Container != nil {
			t.Error("health for a role without agent data")
		}
	}
	// A later event without health (an older agent) leaves no stale value.
	f.data.Event("a1", agentproto.ContainerEvent{Container: agentproto.Container{ID: "c", Name: "mc", State: "running"}})
	if c := f.view.RuntimeFor(ctx, "s1", LevelServerAdmin).Container; c.Health != "" {
		t.Errorf("health after an event without it = %q", c.Health)
	}
}
