// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/agent"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/config"
)

// Containers of the test host. The image of survival is a bare digest, so
// its card shows the profile instead (UI-004).
const (
	imageDigest  = "sha256:4f9a0c4c1e2b7d3a5f6e8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e"
	imageCreativ = "itzg/minecraft-server:java21"
)

// Foreign data as a hostile game server or agent sends it (SEC-006):
// markup, a known and an unknown color code, and an override of the
// reading direction. It belongs to the server hostile and to a container
// no server uses.
const (
	hostileContainer = "mc-hostile"
	hostileMarkup    = `<img src=x onerror="window.__xss=1"><script>window.__xss=1</script>`
	hostileAnswer    = hostileMarkup + "\n§ahello §zworld\nfile\u202Etxt.exe"
	hostileList      = "There are 3 of a max of 20 players online: <script>window.__xss=1</script>, evil\u202Egnp.exe, §aSteve"
)

var hostileLines = []string{hostileMarkup, "§ahello §zworld", "file\u202Etxt.exe"}

// fakeHost stands in for Docker: survival and arena run, creative restarts
// three times within minutes (a restart loop), lobby is stopped.
type fakeHost struct {
	followed sync.Map // containers whose logs were followed once
	ticking  sync.Map // container → stop of its tick lines
}

func (*fakeHost) Name() string { return "docker" }
func (*fakeHost) Capabilities() []string {
	return []string{agentproto.CapContainerAction, agentproto.CapLogs, agentproto.CapMetrics, agentproto.CapRestartInfo,
		agentproto.CapVersionDetect, agentproto.CapVolumesSize}
}

func (*fakeHost) Run(ctx context.Context, out agent.Sender) error {
	now := time.Now().UTC()
	creative := func(ago time.Duration, n int) agentproto.Container {
		return agentproto.Container{ID: "c2", Name: "mc-creative", Image: imageCreativ, State: "running", StartedAt: now.Add(-ago), RestartCount: n}
	}
	snap := agentproto.Snapshot{Containers: []agentproto.Container{
		{ID: "c1", Name: "mc-survival", Image: imageDigest, State: "running", StartedAt: now.Add(-50 * time.Hour), Volumes: []string{"mc-survival-data"}},
		creative(3*time.Minute, 1),
		{ID: "c3", Name: "mc-lobby", Image: imageCreativ, State: "exited"},
		{ID: "c4", Name: "mc-arena", Image: imageCreativ, State: "running", StartedAt: now.Add(-26 * time.Hour)},
		{ID: "c5", Name: hostileContainer, Image: "§a<b>evil</b>\u202E:tag", State: "running", StartedAt: now.Add(-time.Hour)},
		{ID: "c6", Name: "<script>window.__xss=1</script>\u202Exe", Image: imageCreativ, State: "exited"},
	}}
	if err := out.Send(ctx, agentproto.TypeSnapshot, "", snap); err != nil {
		return err
	}
	for i, ago := range []time.Duration{2 * time.Minute, time.Minute} {
		if err := out.Send(ctx, agentproto.TypeContainerEvent, "", agentproto.ContainerEvent{Container: creative(ago, i+2)}); err != nil {
			return err
		}
	}
	const gib = 1 << 30
	metrics := agentproto.Metrics{Containers: []agentproto.Metric{
		{Name: "mc-survival", CPUPercent: 38, MemBytes: 5 * gib, MemLimit: 8 * gib},
		{Name: "mc-creative", CPUPercent: 92, MemBytes: 3 * gib, MemLimit: 6 * gib},
		{Name: "mc-arena", CPUPercent: 31, MemBytes: 11 * gib, MemLimit: 16 * gib},
	}}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		if err := out.Send(ctx, agentproto.TypeMetrics, "", metrics); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// Lines of the test logs: earlier ones only over logs.tail, then the two
// of the first follow.
var (
	earlyLines = []string{"[Server thread/INFO]: Starting minecraft server version 26.3", "[Server thread/INFO]: Preparing level \"world\""}
	firstLines = []string{"[Server thread/INFO]: Steve joined the game", "[Server thread/INFO]: §6Done§r (1.2s)!"}
)

// Handle answers every order. The first follow of a container's logs
// brings two lines; like Docker, a later follow starts at the present and
// the panel's ring holds the earlier lines. While followed, a tick line
// comes every three seconds; logs.tail gives the earlier lines and the
// first two (UI-009, CMD-014, revision 2.4 candidate 20).
func (h *fakeHost) Handle(ctx context.Context, e agentproto.Envelope, out agent.Sender) {
	switch e.Type {
	case agentproto.TypeLogsFollow:
		f, err := agentproto.Payload[agentproto.LogsFollow](e)
		if err != nil {
			break
		}
		if _, again := h.followed.LoadOrStore(f.Container, true); !again {
			lines := firstLines
			if f.Container == hostileContainer {
				lines = hostileLines
			}
			_ = out.Send(ctx, agentproto.TypeLogLines, e.ID, agentproto.LogLines{Container: f.Container, Lines: lines})
		}
		tctx, stop := context.WithCancel(context.WithoutCancel(ctx))
		if _, running := h.ticking.LoadOrStore(f.Container, stop); running {
			stop()
			break
		}
		go h.tick(tctx, f.Container, e.ID, out)
	case agentproto.TypeLogsStop:
		if f, err := agentproto.Payload[agentproto.LogsFollow](e); err == nil {
			if stop, ok := h.ticking.LoadAndDelete(f.Container); ok {
				stop.(context.CancelFunc)()
			}
		}
	case agentproto.TypeLogsTail:
		if t, err := agentproto.Payload[agentproto.LogsTail](e); err == nil {
			lines := append(append([]string{}, earlyLines...), firstLines...)
			if len(lines) > t.Lines {
				lines = lines[len(lines)-t.Lines:]
			}
			_ = out.Send(ctx, agentproto.TypeLogLines, e.ID, agentproto.LogLines{Container: t.Container, Lines: lines})
		}
	case agentproto.TypeVersionDetect:
		// survival logged its version at the start (AGT-006).
		if v, err := agentproto.Payload[agentproto.VersionDetect](e); err == nil && v.Container == "mc-survival" {
			_ = out.Send(ctx, agentproto.TypeVersionFound, e.ID, agentproto.VersionFound{Container: v.Container, Version: "26.3"})
		}
	case agentproto.TypeVolumesSize:
		_ = out.Send(ctx, agentproto.TypeVolumeSizes, e.ID, agentproto.VolumeSizes{Volumes: []agentproto.VolumeSize{
			{Name: "mc-survival-data", Bytes: 7 << 30},
		}})
	}
	_ = out.Send(ctx, agentproto.TypeResult, e.ID, agentproto.Result{Code: agentproto.CodeOK})
}

func (h *fakeHost) tick(ctx context.Context, container, ref string, out agent.Sender) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for n := 1; ; n++ {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		line := fmt.Sprintf("[Server thread/INFO]: tick %d", n)
		if out.Send(ctx, agentproto.TypeLogLines, ref, agentproto.LogLines{Container: container, Lines: []string{line}}) != nil {
			h.ticking.Delete(container)
			return
		}
	}
}

// idleHost has no containers; its agent leaves again at once.
type idleHost struct{ *fakeHost }

func (idleHost) Run(ctx context.Context, out agent.Sender) error {
	if err := out.Send(ctx, agentproto.TypeSnapshot, "", agentproto.Snapshot{Containers: []agentproto.Container{}}); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

// legacyHost is an older agent of the same major version: it reports
// only restart_info and runs one container mc-legacy (AGT-013). It still
// sends metrics, which the panel drops.
type legacyHost struct{ *fakeHost }

func (legacyHost) Capabilities() []string { return []string{agentproto.CapRestartInfo} }

func (legacyHost) Run(ctx context.Context, out agent.Sender) error {
	snap := agentproto.Snapshot{Containers: []agentproto.Container{
		{ID: "l1", Name: "mc-legacy", Image: imageCreativ, State: "running", StartedAt: time.Now().UTC().Add(-time.Hour)},
	}}
	if err := out.Send(ctx, agentproto.TypeSnapshot, "", snap); err != nil {
		return err
	}
	metrics := agentproto.Metrics{Containers: []agentproto.Metric{{Name: "mc-legacy", CPUPercent: 77, MemBytes: 1 << 30, MemLimit: 2 << 30}}}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		if err := out.Send(ctx, agentproto.TypeMetrics, "", metrics); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// startAgent pairs an agent with a fresh token and waits until the panel
// lists it as connected; it returns the agent ID.
func startAgent(ctx context.Context, admin *client, agentAddr, name, root string, backend agent.Backend) (string, error) {
	var tok struct{ Token string }
	if err := admin.call(http.MethodPost, "/agents/tokens", nil, &tok); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(root, "agent-")
	if err != nil {
		return "", err
	}
	cfg := config.Agent{PairingToken: tok.Token, PanelAddr: agentAddr, SecretsDir: dir, Name: name,
		LabelKey: "rconductor.manage", LabelValue: "true", Log: config.Log{Format: "json"}}
	a, err := agent.New(cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)), backend)
	if err != nil {
		return "", err
	}
	go func() { _ = a.Run(ctx) }()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var list struct {
			Items []struct {
				ID, Name  string
				Connected bool
			}
		}
		if err := admin.call(http.MethodGet, "/agents", nil, &list); err != nil {
			return "", err
		}
		for _, it := range list.Items {
			if it.Name == name && it.Connected {
				return it.ID, nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return "", fmt.Errorf("agent %s not connected", name)
}

// waitDisconnected waits until the panel lists the agent as gone.
func waitDisconnected(admin *client, id string) error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var a struct{ Connected bool }
		if err := admin.call(http.MethodGet, "/agents/"+id, nil, &a); err != nil {
			return err
		}
		if !a.Connected {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("agent still connected")
}

// closedPort returns a local port nothing listens on.
func closedPort() (int, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	return strconv.Atoi(port)
}

// seedHosts adds the agents, three more servers on the containers of the
// test host, two server groups and a locked user: everything the overview
// shows (UI-002 to UI-004, UI-006).
func seedHosts(ctx context.Context, admin *client, panel, o, agentAddr, root, gameAddr, hostileAddr, profile string, st *State) error {
	hostID, err := startAgent(ctx, admin, agentAddr, "host-1", root, &fakeHost{})
	if err != nil {
		return err
	}
	gone, cancel := context.WithCancel(ctx)
	spareID, err := startAgent(gone, admin, agentAddr, "host-2", root, idleHost{&fakeHost{}})
	if err != nil {
		cancel()
		return err
	}
	cancel()
	if err := waitDisconnected(admin, spareID); err != nil {
		return err
	}

	if err := pendingAgent(ctx, admin, agentAddr, root); err != nil {
		return err
	}
	// host-legacy is older; a test ties a server of its own to it (AGT-013).
	if st.LegacyAgentID, err = startAgent(ctx, admin, agentAddr, "host-legacy", root, legacyHost{&fakeHost{}}); err != nil {
		return err
	}

	host, port, _ := net.SplitHostPort(gameAddr)
	p, _ := strconv.Atoi(port)
	closed, err := closedPort()
	if err != nil {
		return err
	}
	if err := admin.call(http.MethodPatch, "/servers/"+st.ServerID, map[string]string{"agent_id": hostID, "container_name": "mc-survival"}, nil); err != nil {
		return err
	}
	ids := map[string]string{"survival": st.ServerID}
	for _, s := range []struct {
		name     string
		port     int
		password string
	}{
		{"creative", p, rconPassword},
		{"lobby", closed, rconPassword},
		{"arena", p, "wrong-password"},
	} {
		var srv struct{ ID string }
		if err := admin.call(http.MethodPost, "/servers", map[string]any{
			"name": s.name, "profile_id": profile, "host": host, "port": s.port, "password": s.password,
			"agent_id": hostID, "container_name": "mc-" + s.name,
		}, &srv); err != nil {
			return err
		}
		ids[s.name] = srv.ID
	}
	// outpost has no agent: only the restart over RCON (revision 2.4
	// candidate 16).
	var outpost struct{ ID string }
	if err := admin.call(http.MethodPost, "/servers", map[string]any{
		"name": "outpost", "profile_id": profile, "host": host, "port": p, "password": rconPassword,
	}, &outpost); err != nil {
		return err
	}
	ids["outpost"] = outpost.ID
	// hostile answers with foreign data that tries to do harm (SEC-006).
	hostileHost, hostilePort, _ := net.SplitHostPort(hostileAddr)
	hp, _ := strconv.Atoi(hostilePort)
	var hostile struct{ ID string }
	if err := admin.call(http.MethodPost, "/servers", map[string]any{
		"name": "hostile", "profile_id": profile, "host": hostileHost, "port": hp, "password": rconPassword,
		"agent_id": hostID, "container_name": hostileContainer,
	}, &hostile); err != nil {
		return err
	}
	ids["hostile"] = hostile.ID
	// hmod moderates only hostile, for the commands page (SEC-006).
	hmodID, err := invite(admin, panel, o, "moderator", "hmod")
	if err != nil {
		return err
	}
	if err := admin.call(http.MethodPut, "/users/"+hmodID+"/assignments", map[string]any{
		"groups": []string{st.moderators}, "grants": []map[string]string{{"group_id": st.moderators, "server_id": hostile.ID}},
	}, nil); err != nil {
		return err
	}
	st.Users["hmod"] = userPassword
	st.Servers = ids
	for _, g := range []struct {
		name    string
		servers []string
	}{
		{"minecraft", []string{ids["survival"], ids["creative"], ids["lobby"]}},
		{"events", []string{ids["arena"]}},
	} {
		if err := admin.call(http.MethodPost, "/server-groups", map[string]any{"name": g.name, "server_ids": g.servers}, nil); err != nil {
			return err
		}
	}
	lockedID, err := invite(admin, panel, o, "supporter", "locked")
	if err != nil {
		return err
	}
	st.LockedUserID = lockedID
	if err := admin.call(http.MethodPatch, "/users/"+lockedID, map[string]bool{"disabled": true}, nil); err != nil {
		return err
	}
	return seedHistory(admin, panel, o, ids["survival"])
}

// seedHistory sends commands of the admin and of the moderator to
// survival, for the history tab; one answer carries color codes.
func seedHistory(admin *client, panel, o, server string) error {
	if err := waitConnected(admin, server); err != nil {
		return err
	}
	for _, c := range []string{"list", "say §ahello §lworld"} {
		if err := admin.call(http.MethodPost, "/servers/"+server+"/commands", map[string]string{"command": c}, nil); err != nil {
			return err
		}
	}
	mod := &client{base: panel, origin: o}
	if err := mod.call(http.MethodPost, "/auth/login", map[string]string{"pseudonym": "mod", "password": userPassword}, nil); err != nil {
		return err
	}
	return mod.call(http.MethodPost, "/servers/"+server+"/commands", map[string]string{"command": "say hi from mod"}, nil)
}

// waitConnected waits until the panel reaches the server over RCON.
func waitConnected(admin *client, id string) error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var s struct{ Status string }
		if err := admin.call(http.MethodGet, "/servers/"+id, nil, &s); err != nil {
			return err
		}
		if s.Status == "connected" {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("server not connected")
}

// pendingAgent leaves an agent waiting for approval by fingerprint (way
// B, AGT-005): pairing by fingerprint is on only until it waits, then off
// again, as by default. The agent keeps trying and connects once approved.
func pendingAgent(ctx context.Context, admin *client, agentAddr, root string) error {
	if err := admin.call(http.MethodPatch, "/settings", map[string]bool{"agents.fingerprint_pairing": true}, nil); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(root, "agent-")
	if err != nil {
		return err
	}
	cfg := config.Agent{PanelAddr: agentAddr, SecretsDir: dir, Name: "host-3",
		LabelKey: "rconductor.manage", LabelValue: "true", Log: config.Log{Format: "json"}}
	a, err := agent.New(cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)), idleHost{&fakeHost{}})
	if err != nil {
		return err
	}
	go func() { _ = a.Run(ctx) }()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var list struct {
			Items []struct{ Name, Status string }
		}
		if err := admin.call(http.MethodGet, "/agents", nil, &list); err != nil {
			return err
		}
		for _, it := range list.Items {
			if it.Name == "host-3" && it.Status == "pending" {
				return admin.call(http.MethodPatch, "/settings", map[string]bool{"agents.fingerprint_pairing": false}, nil)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("agent host-3 not pending")
}
