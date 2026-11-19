// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/features"
)

// TestHostGroups covers UI-021 and D-089: the RCON admin sets the display
// name of a host, foreign data with at most agents.display_name_max
// characters and no control characters (400); null or "" removes it. The
// servers of one answer carry their host with a key that is the same per
// host and not the agent ID; a server without an agent has none. With the
// flag off the answers are as before and the change is 404.
func TestHostGroups(t *testing.T) {
	flags, err := features.New(features.Flag{Name: "host_groups", Operations: []string{"UpdateAgent"}})
	if err != nil {
		t.Fatal(err)
	}
	f := newAPIFixtureFeatures(t, Proxies{}, flags)
	admin := f.login(t, "Boss")
	a, b := "00000000-0000-4000-8000-0000000000a1", "00000000-0000-4000-8000-0000000000b2"
	for _, q := range []string{
		`INSERT INTO agents (id, name, public_key, fingerprint, status, created_at) VALUES ('` + a + `', 'nas-a', x'00', 'fa', 'active', 't'), ('` + b + `', 'nas-b', x'00', 'fb', 'active', 't')`,
		`INSERT INTO servers (id, name, agent_id, created_at, updated_at) VALUES ('00000000-0000-4000-8000-000000000001', 'a1', '` + a + `', 't', 't'),
			('00000000-0000-4000-8000-000000000002', 'a2', '` + a + `', 't', 't'), ('00000000-0000-4000-8000-000000000003', 'b', '` + b + `', 't', 't'),
			('00000000-0000-4000-8000-000000000004', 'none', NULL, 't', 't')`,
	} {
		if _, err := f.st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	req := func(path, body string) int {
		return f.do(t, call{method: http.MethodPatch, path: "/api/v1/" + path, body: body, origin: "http://panel.lan", cookies: []*http.Cookie{admin}}).Code
	}
	set, patch := func(id, body string) int { return req("agents/"+id, body) }, func(body string) int { return req("settings", body) }
	groups := func() map[string]*api.HostGroup {
		rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/servers", cookies: []*http.Cookie{admin}})
		var list api.ServerList
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		out := map[string]*api.HostGroup{}
		for _, s := range list.Items {
			out[s.Name] = s.HostGroup
		}
		return out
	}
	if groups()["a1"] != nil || patch(`{"features.host_groups":true}`) != http.StatusOK {
		t.Fatal("flag off: host group shown, or flag not switched on")
	}

	for body, want := range map[string]int{
		`{"display_name":"Rack\u0007"}`: 400, `{"display_name":"Rack\n1"}`: 400, `{"display_name":"\u001b[31mRack"}`: 400,
		`{"display_name":"` + strings.Repeat("x", 65) + `"}`: 400, `{"display_name":"` + strings.Repeat("x", 1<<20) + `"}`: 400,
		`{"display_name":"` + strings.Repeat("ä", 64) + `"}`: 200,
	} {
		if got := set(b, body); got != want && (want != 400 || got != http.StatusRequestEntityTooLarge) {
			t.Errorf("%.40s = %d, want %d", body, got, want)
		}
	}
	// The length is a setting with a validated minimum.
	if patch(`{"agents.display_name_max":15}`) != http.StatusUnprocessableEntity || patch(`{"agents.display_name_max":16}`) != http.StatusOK ||
		set(b, `{"display_name":"`+strings.Repeat("x", 17)+`"}`) != http.StatusBadRequest {
		t.Error("agents.display_name_max not applied")
	}
	for _, body := range []string{`{"display_name":null}`, `{"display_name":""}`} {
		if set(b, body) != http.StatusOK || groups()["b"].DisplayName != nil {
			t.Errorf("%s does not remove the display name", body)
		}
	}
	// Parallel changes: each is written and audited, one of them stays.
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if got := set(a, fmt.Sprintf(`{"display_name":"Rack %d"}`, i)); got != http.StatusOK {
				t.Errorf("parallel %d = %d", i, got)
			}
		})
	}
	wg.Wait()
	var ok int
	if err := f.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'agent.display_name_set' AND result = 'ok'`).Scan(&ok); err != nil || ok != 23 {
		t.Errorf("audited changes = %d (%v), want 23", ok, err)
	}

	g := groups()
	switch {
	case g["a1"] == nil || g["a2"] == nil || g["b"] == nil || g["none"] != nil:
		t.Fatalf("host groups %v", g)
	case g["a1"].Key != g["a2"].Key || g["a1"].Key == g["b"].Key || g["a1"].Key == a || g["b"].Key == b:
		t.Errorf("keys %q %q %q", g["a1"].Key, g["a2"].Key, g["b"].Key)
	case g["a1"].DisplayName == nil || !strings.HasPrefix(*g["a1"].DisplayName, "Rack ") || g["b"].DisplayName != nil:
		t.Errorf("display names %v %v", g["a1"].DisplayName, g["b"].DisplayName)
	}
	agent := func() string {
		return f.do(t, call{method: http.MethodGet, path: "/api/v1/agents/" + a, cookies: []*http.Cookie{admin}}).Body.String()
	}
	if !strings.Contains(agent(), `"display_name":"Rack `) {
		t.Error("agent without its display name")
	}
	if patch(`{"features.host_groups":false}`) != http.StatusOK || groups()["a1"] != nil || strings.Contains(agent(), "display_name") {
		t.Error("flag off again: host group or display name shown")
	}
}
