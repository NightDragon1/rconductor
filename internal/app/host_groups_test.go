// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
)

// TestHostGroupsByRole covers UI-021 and D-089 per role over HTTP and the
// WebSocket, with a real agent: only the RCON admin sets the display name
// (403 for every other role); every role sees the servers of its scope
// with the display name of their host and a key that is not the agent ID;
// host name and address reach no role but the RCON admin.
func TestHostGroupsByRole(t *testing.T) {
	f := newRightsFixture(t)
	const name, ip, display = "nas-7f3", "198.51.100.7", "Rack seven"
	others := []string{rSI, rSO, rMR, rMN, rSU, rP}
	rec := map[string]*wsRecorder{}
	for _, role := range append([]string{rA}, others...) {
		rec[role] = recordTopics(t, f, role, f.server, f.server2)
	}
	agentID := pairAgent(t, f.as(rA), f.agentAddr, f.server, &hostBackend{host: &agentproto.DockerHost{Name: name, IP: ip}})
	// The second server runs on the same host.
	if _, err := f.db().ExecContext(context.Background(), `UPDATE servers SET agent_id = ?, container_name = 'mc2' WHERE id = ?`, agentID, f.server2); err != nil {
		t.Fatal(err)
	}
	for _, role := range append(others, rX) {
		status, body := f.rawBody(role, http.MethodPatch, "/agents/"+agentID, map[string]any{"display_name": "Mine"})
		if want := map[bool]int{true: http.StatusUnauthorized, false: http.StatusForbidden}[role == rX]; status != want ||
			(role != rX && role != rP && !strings.Contains(string(body), `"forbidden"`)) {
			t.Errorf("%s sets the display name = %d %s", role, status, body)
		}
	}
	f.as(rA).must(http.MethodPatch, "/agents/"+agentID, map[string]any{"display_name": display}, nil, http.StatusOK)

	// Live data reaches every viewer in scope; a topic with the host shows it.
	since := map[string]int{}
	for role, r := range rec {
		if r != nil {
			since[role] = r.size()
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for _, role := range []string{rA, rSI, rMR, rMN, rSU} {
		for rec[role] != nil && rec[role].runtimeSince(since[role]) < 2 {
			if time.Now().After(deadline) {
				t.Fatalf("%s: no runtime event in servers.overview", role)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	type item struct {
		ID        string
		HostGroup *struct {
			Key         string
			DisplayName *string `json:"display_name"`
		} `json:"host_group"`
	}
	valid := func(it item) bool {
		g := it.HostGroup
		return g != nil && g.Key != "" && g.Key != agentID && g.DisplayName != nil && *g.DisplayName == display
	}
	scope := map[string][]string{rA: {f.server, f.server2}, rSI: {f.server}, rSO: {f.server2}, rMR: {f.server}, rMN: {f.server}, rSU: {f.server}}
	for role, want := range scope {
		var list struct{ Items []item }
		var one item
		f.as(role).must(http.MethodGet, "/servers", nil, &list, http.StatusOK)
		f.as(role).must(http.MethodGet, "/servers/"+want[0], nil, &one, http.StatusOK)
		var got, keys []string
		for _, it := range list.Items {
			got = append(got, it.ID)
			if !valid(it) {
				t.Fatalf("%s: host group of %s = %+v", role, it.ID, it.HostGroup)
			}
			keys = append(keys, it.HostGroup.Key)
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) || len(slices.Compact(keys)) != 1 || !valid(one) {
			t.Errorf("%s sees %v with keys %v, want %v; one %+v", role, got, keys, want, one.HostGroup)
		}
	}

	leaks := []string{name, ip, "host-1"}
	for _, role := range append(others, rX) {
		for _, p := range []string{"/servers", "/servers/" + f.server, "/servers/" + f.server2, "/agents", "/agents/" + agentID} {
			status, body := f.rawBody(role, http.MethodGet, p, nil)
			for _, leak := range leaks {
				if strings.Contains(string(body), leak) {
					t.Errorf("%s GET %s = %d carries %s", role, p, status, leak)
				}
			}
		}
		for _, leak := range append(leaks, agentID) {
			if r := rec[role]; r != nil && strings.Contains(r.all(), leak) {
				t.Errorf("%s gets %s over the WebSocket", role, leak)
			}
		}
	}
}
