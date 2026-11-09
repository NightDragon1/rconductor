// SPDX-License-Identifier: AGPL-3.0-or-later

package agentview

import (
	"context"
	"encoding/json"
	"net"
	"strconv"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Level is how much of the runtime data a role gets (UI-005, UI-006):
// players and version for all, the container for admins, all ports only
// for the RCON admin.
type Level int

// Levels of the runtime data.
const (
	LevelPlayers Level = iota
	LevelServerAdmin
	LevelRCONAdmin
)

// Kinds of a port (UI-005).
const (
	PortGame  = "game"
	PortRCON  = "rcon"
	PortOther = "other"
)

// PortView is a port of the server's container: a game port of the
// profile or the server, the RCON port, or another published port. Public
// is the port on the host, 0 when it is not published.
type PortView struct {
	Kind    string `json:"kind"`
	Name    string `json:"name,omitempty"`
	Private int    `json:"private"`
	Public  int    `json:"public,omitempty"`
	Proto   string `json:"proto"`
	// SteamURL connects a Steam client to a published game port, when the
	// profile switches it on (ports.steam_connect).
	SteamURL string `json:"steam_url,omitempty"`
}

// portsOf lists the ports of a container for the level: game ports for
// the server admin, the RCON port and every other published port as well
// for the RCON admin (UI-005).
func (v *View) portsOf(ctx context.Context, r sqlitedb.GetServerRow, snapshot []agentproto.Port, level Level) []PortView {
	game, steam := v.gamePorts(ctx, r)
	used := map[int]bool{}
	published := func(private int, proto string) (int, int) {
		for i, p := range snapshot {
			if p.Private == private && p.Proto == proto {
				return p.Public, i
			}
		}
		return 0, -1
	}
	out := []PortView{}
	for _, g := range game {
		pub, i := published(g.Port, g.Proto)
		if i >= 0 {
			used[i] = true
		}
		pv := PortView{Kind: PortGame, Name: g.Name, Private: g.Port, Public: pub, Proto: g.Proto}
		if steam && pub > 0 && r.Host != "" {
			pv.SteamURL = "steam://connect/" + net.JoinHostPort(r.Host, strconv.Itoa(pub))
		}
		out = append(out, pv)
	}
	if level < LevelRCONAdmin {
		return out
	}
	rcon := PortView{Kind: PortRCON, Private: int(r.Port), Proto: "tcp"}
	for i, p := range snapshot {
		if !used[i] && p.Proto == "tcp" && (p.Public == int(r.Port) || p.Private == int(r.Port)) {
			rcon.Private, rcon.Public, used[i] = p.Private, p.Public, true
			break
		}
	}
	out = append(out, rcon)
	for i, p := range snapshot {
		if !used[i] && p.Public > 0 {
			out = append(out, PortView{Kind: PortOther, Private: p.Private, Public: p.Public, Proto: p.Proto})
		}
	}
	return out
}

// gamePorts returns the effective game ports of a server (SRV-005) and
// whether its profile offers the Steam link.
func (v *View) gamePorts(ctx context.Context, r sqlitedb.GetServerRow) ([]profiles.GamePort, bool) {
	var rules profiles.Profile
	if eff, _, err := profiles.ForServer(ctx, v.st.Queries, r.ID); err == nil && eff != nil {
		rules = *eff
	}
	game := rules.Ports.Game
	if r.GamePorts.Valid {
		var own []profiles.GamePort
		if json.Unmarshal([]byte(r.GamePorts.String), &own) == nil {
			game = own
		}
	}
	return game, rules.Ports.SteamConnect
}
