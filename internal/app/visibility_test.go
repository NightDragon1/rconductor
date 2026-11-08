// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// consoleOf opens a WebSocket of role and subscribes the console of the
// fixture's server; nil if the subscription is refused.
func consoleOf(t *testing.T, f *rightsFixture, role, server string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.base, "http")+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {f.base}, "Cookie": {f.c[role].cookie.String()}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	topic := "server." + server + ".console"
	b, _ := json.Marshal(map[string]string{"type": "subscribe", "topic": topic})
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
	for {
		var m struct{ Type, Topic string }
		if err := readJSON(c, &m); err != nil {
			t.Fatal(err)
		}
		switch {
		case m.Type == "subscribed" && m.Topic == topic:
			return c
		case m.Type == "error" && m.Topic == topic:
			return nil
		}
	}
}

// consoleUntil reads console events until the marker command and returns
// the commands seen before it; events arrive in order, so the marker
// closes the list.
func consoleUntil(t *testing.T, c *websocket.Conn, marker string) []string {
	t.Helper()
	var seen []string
	for {
		var m struct {
			Type, Topic string
			Data        struct{ Command string }
		}
		if err := readJSON(c, &m); err != nil {
			t.Fatalf("no marker: %v (seen %v)", err, seen)
		}
		if m.Type != "event" || !strings.Contains(m.Topic, ".console") {
			continue
		}
		if m.Data.Command == marker {
			slices.Sort(seen)
			return seen
		}
		seen = append(seen, m.Data.Command)
	}
}

// TestHistoryAndConsoleVisibility covers ROLE-009 (D-067) over REST and
// WebSocket: viewer role × author role on a server in scope; outside the
// scope nothing; the filter own of the history; the live console checks
// every message.
func TestHistoryAndConsoleVisibility(t *testing.T) {
	f := newRightsFixture(t)
	authors := []string{rA, rSI, rMR, rSU}
	viewers := []string{rA, rSI, rMR, rMN, rSU}
	conns := map[string]*websocket.Conn{}
	for _, v := range viewers {
		conns[v] = consoleOf(t, f, v, f.server)
		if conns[v] == nil {
			t.Fatalf("%s may not follow the console", v)
		}
	}
	if consoleOf(t, f, rSO, f.server) != nil {
		t.Error("a server admin outside the scope follows the console")
	}
	for _, a := range authors {
		if status, _, body := f.send(a, "say by "+a); status != http.StatusOK {
			t.Fatalf("%s: %d %v", a, status, body)
		}
	}
	// The marker comes from a moderator, whom every viewer sees.
	if status, _, _ := f.send(rMN, "say marker"); status != http.StatusOK {
		t.Fatal("marker not sent")
	}
	want := map[string][]string{
		rA:  {"say by " + rA, "say by " + rMR, "say by " + rSU, "say by " + rSI},
		rSI: {"say by " + rMR, "say by " + rSU, "say by " + rSI},
		rMR: {"say by " + rMR, "say by " + rSU},
		rMN: {"say by " + rMR, "say by " + rSU},
		rSU: {"say by " + rMR, "say by " + rSU},
	}
	for _, v := range viewers {
		w := slices.Clone(want[v])
		slices.Sort(w)
		if got := consoleUntil(t, conns[v], "say marker"); !slices.Equal(got, w) {
			t.Errorf("console of %s = %v, want %v", v, got, w)
		}
	}

	history := func(role string, own bool) []string {
		var page struct{ Items []struct{ Command string } }
		path := "/servers/" + f.server + "/history?limit=200"
		if own {
			path += "&own=true"
		}
		f.as(role).must(http.MethodGet, path, nil, &page, http.StatusOK)
		var out []string
		for _, e := range page.Items {
			if strings.HasPrefix(e.Command, "say by ") {
				out = append(out, e.Command)
			}
		}
		slices.Sort(out)
		return out
	}
	for _, v := range viewers {
		w := slices.Clone(want[v])
		slices.Sort(w)
		if got := history(v, false); !slices.Equal(got, w) {
			t.Errorf("history of %s = %v, want %v", v, got, w)
		}
		var mine []string
		if slices.Contains(authors, v) {
			mine = []string{"say by " + v}
		}
		if got := history(v, true); !slices.Equal(got, mine) {
			t.Errorf("own history of %s = %v, want %v", v, got, mine)
		}
	}
	if status, _ := f.raw(rSO, http.MethodGet, "/servers/"+f.server+"/history", nil); status != http.StatusNotFound {
		t.Errorf("history outside the scope = %d", status)
	}
	// No parameter widens what a moderator sees.
	for _, q := range []string{"?own=false", "?own=true&own=false", "?limit=1&cursor=zzzz", "?role=rcon_admin", "?actor=" + f.ids[rSI]} {
		var page struct{ Items []struct{ Command string } }
		status := f.as(rMR).call(http.MethodGet, "/servers/"+f.server+"/history"+q, nil, &page)
		for _, e := range page.Items {
			if e.Command == "say by "+rA || e.Command == "say by "+rSI {
				t.Errorf("%s (%d): moderator sees %q", q, status, e.Command)
			}
		}
	}
}
