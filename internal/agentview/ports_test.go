// SPDX-License-Identifier: AGPL-3.0-or-later

package agentview

import (
	"context"
	"testing"

	"github.com/NightDragon1/rconductor/internal/agentproto"
)

// TestPortsByLevel covers UI-005: the server admin gets the game ports,
// published or not, with the Steam link when the profile offers it; the
// RCON admin also the RCON port and every other published port; the other
// roles no container part. A port that is not published has no link.
func TestPortsByLevel(t *testing.T) {
	f := newInsightFixture(t)
	ctx := context.Background()
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE game_profiles SET rules = ? WHERE id = 'p1'`,
		`{"ports":{"game":[{"port":25565,"proto":"tcp","name":"game"},{"port":24454,"proto":"udp","name":"voice"}],"steam_connect":true}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE servers SET host = 'mc.example', port = 25575 WHERE id = 's1'`); err != nil {
		t.Fatal(err)
	}
	f.data.Snapshot("a1", agentproto.Snapshot{Containers: []agentproto.Container{{ID: "c", Name: "mc", State: "running", Ports: []agentproto.Port{
		{Private: 25565, Public: 30000, Proto: "tcp"},
		{Private: 25575, Public: 25575, Proto: "tcp"},
		{Private: 8123, Public: 8123, Proto: "tcp"},
		{Private: 9999, Proto: "tcp"}, // not published
	}}}})

	admin := f.view.RuntimeFor(ctx, "s1", LevelServerAdmin).Container.Ports
	want := []PortView{
		{Kind: PortGame, Name: "game", Private: 25565, Public: 30000, Proto: "tcp", SteamURL: "steam://connect/mc.example:30000"},
		{Kind: PortGame, Name: "voice", Private: 24454, Proto: "udp"},
	}
	if len(admin) != len(want) || admin[0] != want[0] || admin[1] != want[1] {
		t.Errorf("server admin ports = %+v, want %+v", admin, want)
	}
	rcon := f.view.RuntimeFor(ctx, "s1", LevelRCONAdmin).Container.Ports
	want = append(want,
		PortView{Kind: PortRCON, Private: 25575, Public: 25575, Proto: "tcp"},
		PortView{Kind: PortOther, Private: 8123, Public: 8123, Proto: "tcp"},
	)
	if len(rcon) != len(want) {
		t.Fatalf("RCON admin ports = %+v, want %+v", rcon, want)
	}
	for i := range want {
		if rcon[i] != want[i] {
			t.Errorf("RCON admin port %d = %+v, want %+v", i, rcon[i], want[i])
		}
	}
	if rt := f.view.RuntimeFor(ctx, "s1", LevelPlayers); rt.Container != nil {
		t.Error("ports for a role without agent data")
	}

	// Without the switch in the profile no link; an IPv6 host is bracketed.
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE servers SET host = '2001:db8::1' WHERE id = 's1'`); err != nil {
		t.Fatal(err)
	}
	if got := f.view.RuntimeFor(ctx, "s1", LevelServerAdmin).Container.Ports[0].SteamURL; got != "steam://connect/[2001:db8::1]:30000" {
		t.Errorf("IPv6 link = %q", got)
	}
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE game_profiles SET rules = '{"ports":{"game":[{"port":25565,"proto":"tcp","name":"game"}]}}' WHERE id = 'p1'`); err != nil {
		t.Fatal(err)
	}
	if got := f.view.RuntimeFor(ctx, "s1", LevelServerAdmin).Container.Ports[0].SteamURL; got != "" {
		t.Errorf("link without steam_connect = %q", got)
	}
}
