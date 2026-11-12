// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/agentproto"
)

// connectionKeys are the fields of a server that only the admins get
// (Design 2.2, D-056): moderator and supporter see none of them.
var connectionKeys = []string{"protocol", "host", "port", "credentials", "agent_id", "container_name",
	"game_ports", "game_ports_override", "tls_fingerprint", "tls_not_after"}

// portKinds returns the kinds of the container ports in a server or a
// runtime event, nil without a container part, and the private port of
// the RCON entry, 0 without one.
func portKinds(t *testing.T, runtime json.RawMessage) ([]string, int) {
	t.Helper()
	var rt struct {
		Container *struct {
			Ports []struct {
				Kind    string
				Private int
			}
		}
	}
	if len(runtime) > 0 {
		if err := json.Unmarshal(runtime, &rt); err != nil {
			t.Fatalf("runtime %s: %v", runtime, err)
		}
	}
	if rt.Container == nil {
		return nil, 0
	}
	kinds, rcon := []string{}, 0
	for _, p := range rt.Container.Ports {
		kinds = append(kinds, p.Kind)
		if p.Kind == "rcon" {
			rcon = p.Private
		}
	}
	return kinds, rcon
}

// overviewRuntime returns the first runtime event of server in
// servers.overview as role sees it.
func overviewRuntime(t *testing.T, f *rightsFixture, role, server string) json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.base, "http")+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {f.base}, "Cookie": {f.c[role].cookie.String()}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("ws as %s: %v", role, err)
	}
	defer func() { _ = c.CloseNow() }()
	subscribe(t, c, "servers.overview")
	return next(t, c, func(typ, topic string, d json.RawMessage) bool {
		var ev struct {
			Kind     string
			ServerID string `json:"server_id"`
		}
		_ = json.Unmarshal(d, &ev)
		return typ == "event" && topic == "servers.overview" && ev.Kind == "runtime" && ev.ServerID == server
	})
}

// TestO6HostAndPortsByRole checks host, RCON port and game ports per role
// in every answer with server data (UI-005, UI-006, D-056): the RCON admin
// gets host, RCON port and all ports; the server admin in scope only the
// game ports, neither host nor RCON port; moderator and supporter neither
// host nor any port, in the server list, the server, the server groups,
// servers.overview and the audit log alike.
func TestO6HostAndPortsByRole(t *testing.T) {
	f := newRightsFixture(t)
	pairAgent(t, f.as(rA), f.agentAddr, f.server, &hostBackend{ports: []agentproto.Port{
		{Private: 25565, Public: 30000, Proto: "tcp"}, {Private: 8123, Public: 8123, Proto: "tcp"},
	}})
	var conn struct {
		Host string
		Port int
	}
	f.as(rA).must(http.MethodGet, "/servers/"+f.server, nil, &conn, http.StatusOK)
	if conn.Host == "" || conn.Port == 0 {
		t.Fatalf("RCON admin: host %q port %d", conn.Host, conn.Port)
	}

	// views returns the server as role sees it in the list and alone.
	views := func(role string) map[string]map[string]json.RawMessage {
		out := map[string]map[string]json.RawMessage{}
		var one map[string]json.RawMessage
		f.as(role).must(http.MethodGet, "/servers/"+f.server, nil, &one, http.StatusOK)
		out["server"] = one
		var list struct{ Items []map[string]json.RawMessage }
		f.as(role).must(http.MethodGet, "/servers", nil, &list, http.StatusOK)
		for _, it := range list.Items {
			var id string
			_ = json.Unmarshal(it["id"], &id)
			if id == f.server {
				out["list"] = it
			}
		}
		if out["list"] == nil {
			t.Fatalf("%s: server not in the list", role)
		}
		return out
	}

	for _, role := range []string{rA, rSI, rMR, rMN, rSU} {
		for where, s := range views(role) {
			kinds, rcon := portKinds(t, s["runtime"])
			var game []json.RawMessage
			_ = json.Unmarshal(s["game_ports"], &game)
			switch role {
			case rA:
				var host string
				var port int
				_ = json.Unmarshal(s["host"], &host)
				_ = json.Unmarshal(s["port"], &port)
				if host != conn.Host || port != conn.Port || len(game) == 0 || rcon != conn.Port ||
					!strings.Contains(strings.Join(kinds, ","), "game,rcon") {
					t.Errorf("O6 %s %s: host %q port %d game ports %d container ports %v rcon %d, want all",
						role, where, host, port, len(game), kinds, rcon)
				}
			case rSI:
				for _, k := range []string{"host", "port"} {
					if _, ok := s[k]; ok {
						t.Errorf("O6 %s %s: field %s present", role, where, k)
					}
				}
				if b, _ := json.Marshal(s); strings.Contains(string(b), conn.Host) || rcon != 0 {
					t.Errorf("O6 %s %s: host or RCON port in %s", role, where, b)
				}
				if len(game) == 0 || len(kinds) == 0 {
					t.Errorf("O6 %s %s: game ports %d container ports %v, want the game ports", role, where, len(game), kinds)
				}
				for _, k := range kinds {
					if k != "game" {
						t.Errorf("O6 %s %s: container port of kind %s, want game ports only", role, where, k)
					}
				}
			default:
				for _, k := range connectionKeys {
					if _, ok := s[k]; ok {
						t.Errorf("O6 %s %s: field %s present", role, where, k)
					}
				}
				if kinds != nil {
					t.Errorf("O6 %s %s: container part with ports %v", role, where, kinds)
				}
				b, _ := json.Marshal(s)
				if strings.Contains(string(b), conn.Host) {
					t.Errorf("O6 %s %s: host in %s", role, where, b)
				}
			}
		}
	}

	// Server groups carry IDs only; moderator and supporter may not read
	// them (Design 2.2).
	for _, role := range []string{rA, rSI, rMR, rMN, rSU} {
		for _, path := range []string{"/server-groups", "/server-groups/" + f.serverGroup} {
			status, b := f.rawBody(role, http.MethodGet, path, nil)
			switch role {
			case rA, rSI:
				if status != http.StatusOK || strings.Contains(string(b), `"host"`) || strings.Contains(string(b), conn.Host) ||
					strings.Contains(string(b), `"port"`) {
					t.Errorf("O6 %s %s: %d %s", role, path, status, b)
				}
			default:
				if status != http.StatusForbidden {
					t.Errorf("O6 %s %s: %d, want 403", role, path, status)
				}
			}
		}
	}

	// servers.overview sends the same three views.
	for _, role := range []string{rA, rSI, rMR, rMN, rSU} {
		d := overviewRuntime(t, f, role, f.server)
		kinds, rcon := portKinds(t, d)
		switch role {
		case rA:
			if rcon != conn.Port || !strings.Contains(strings.Join(kinds, ","), "game,rcon") {
				t.Errorf("O6 %s overview: ports %v rcon %d, want all", role, kinds, rcon)
			}
		case rSI:
			if len(kinds) == 0 || rcon != 0 || strings.Contains(string(d), conn.Host) {
				t.Errorf("O6 %s overview: ports %v, want the game ports only", role, kinds)
			}
			for _, k := range kinds {
				if k != "game" {
					t.Errorf("O6 %s overview: port of kind %s", role, k)
				}
			}
		default:
			if kinds != nil || strings.Contains(string(d), conn.Host) {
				t.Errorf("O6 %s overview: %s, want no container part and no host", role, d)
			}
		}
	}

	// The audit log: a change of host and port shows old and new value to
	// the RCON admin only; the server admin reads the entry without them.
	f.as(rA).must(http.MethodPatch, "/servers/"+f.server, map[string]any{"host": "localhost", "port": conn.Port + 1}, nil, http.StatusOK)
	t.Cleanup(func() {
		f.as(rA).must(http.MethodPatch, "/servers/"+f.server, map[string]any{"host": conn.Host, "port": conn.Port}, nil, http.StatusOK)
	})
	for _, role := range []string{rA, rSI} {
		var page struct {
			Items []struct {
				Action  string
				Payload map[string]json.RawMessage
			}
		}
		f.as(role).must(http.MethodGet, "/audit?server_id="+f.server+"&kind=admin", nil, &page, http.StatusOK)
		found := false
		for _, e := range page.Items {
			if e.Action != "server.update" {
				continue
			}
			_, host := e.Payload["host"]
			_, port := e.Payload["port"]
			if host || port {
				found = true
			}
			if role == rSI && (host || port) {
				t.Errorf("O6 %s audit: %s with host or port: %v", role, e.Action, e.Payload)
			}
			if role == rSI {
				found = true
			}
		}
		if !found {
			t.Errorf("O6 %s audit: no server.update entry with the change", role)
		}
	}
}

// steamURLs returns the Steam links among the container ports of a
// server or a runtime event.
func steamURLs(t *testing.T, runtime json.RawMessage) []string {
	t.Helper()
	var rt struct {
		Container *struct {
			Ports []struct {
				SteamURL string `json:"steam_url"`
			}
		}
	}
	if err := json.Unmarshal(runtime, &rt); err != nil {
		t.Fatalf("runtime %s: %v", runtime, err)
	}
	var out []string
	if rt.Container != nil {
		for _, p := range rt.Container.Ports {
			if p.SteamURL != "" {
				out = append(out, p.SteamURL)
			}
		}
	}
	return out
}

// TestO6SteamLinkByRole checks the Steam connect link (UI-005, ROLE-003):
// it carries the server's host, so only the RCON admin gets it, over REST
// and in servers.overview; the server admin gets the game port without it
// (D-056), moderator and supporter no ports at all.
func TestO6SteamLinkByRole(t *testing.T) {
	f := newRightsFixture(t)
	pairAgent(t, f.as(rA), f.agentAddr, f.server, &hostBackend{ports: []agentproto.Port{
		{Private: 25565, Public: 30000, Proto: "tcp"},
	}})
	// A custom profile above the Minecraft one switches the link on.
	dir := filepath.Join(f.data, "profiles")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	profile := "schema: 1\nid: o6-steam\nname: O6 Steam\nkind: specific\nprotocol: { type: source-rcon, default_port: 25575 }\nports:\n  steam_connect: true\n"
	if err := os.WriteFile(filepath.Join(dir, "o6-steam.yaml"), []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	steam := ""
	for deadline := time.Now().Add(10 * time.Second); steam == ""; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("custom profile not imported")
		}
		var p struct{ Items []struct{ ID, Slug string } }
		f.as(rA).must(http.MethodGet, "/profiles", nil, &p, http.StatusOK)
		for _, x := range p.Items {
			if x.Slug == "o6-steam" {
				steam = x.ID
			}
		}
	}
	f.as(rA).must(http.MethodPatch, "/servers/"+f.server, map[string]any{"profile_ids": []string{steam, f.profile()}}, nil, http.StatusOK)
	var conn struct{ Host string }
	f.as(rA).must(http.MethodGet, "/servers/"+f.server, nil, &conn, http.StatusOK)
	want := "steam://connect/" + net.JoinHostPort(conn.Host, "30000")

	for _, role := range []string{rA, rSI, rMR, rMN, rSU} {
		var one struct{ Runtime json.RawMessage }
		f.as(role).must(http.MethodGet, "/servers/"+f.server, nil, &one, http.StatusOK)
		var list struct {
			Items []struct {
				ID      string
				Runtime json.RawMessage
			}
		}
		f.as(role).must(http.MethodGet, "/servers", nil, &list, http.StatusOK)
		views := map[string]json.RawMessage{"server": one.Runtime, "overview": overviewRuntime(t, f, role, f.server)}
		for _, it := range list.Items {
			if it.ID == f.server {
				views["list"] = it.Runtime
			}
		}
		for where, rt := range views {
			got := steamURLs(t, rt)
			kinds, _ := portKinds(t, rt)
			switch role {
			case rA:
				if len(got) != 1 || got[0] != want {
					t.Errorf("O6 %s %s: Steam links %v, want [%s]", role, where, got, want)
				}
			case rSI:
				if len(got) != 0 || len(kinds) == 0 {
					t.Errorf("O6 %s %s: Steam links %v, ports %v, want the game port without link", role, where, got, kinds)
				}
			default:
				if len(got) != 0 || kinds != nil {
					t.Errorf("O6 %s %s: Steam links %v, ports %v, want none", role, where, got, kinds)
				}
			}
		}
	}
}
