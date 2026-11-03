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

// fakeHost stands in for Docker: survival and arena run, creative restarts
// three times within minutes (a restart loop), lobby is stopped.
type fakeHost struct{}

func (fakeHost) Name() string { return "docker" }
func (fakeHost) Capabilities() []string {
	return []string{agentproto.CapContainerAction, agentproto.CapLogs, agentproto.CapMetrics, agentproto.CapRestartInfo}
}

func (fakeHost) Run(ctx context.Context, out agent.Sender) error {
	now := time.Now().UTC()
	creative := func(ago time.Duration, n int) agentproto.Container {
		return agentproto.Container{ID: "c2", Name: "mc-creative", Image: imageCreativ, State: "running", StartedAt: now.Add(-ago), RestartCount: n}
	}
	snap := agentproto.Snapshot{Containers: []agentproto.Container{
		{ID: "c1", Name: "mc-survival", Image: imageDigest, State: "running", StartedAt: now.Add(-50 * time.Hour)},
		creative(3*time.Minute, 1),
		{ID: "c3", Name: "mc-lobby", Image: imageCreativ, State: "exited"},
		{ID: "c4", Name: "mc-arena", Image: imageCreativ, State: "running", StartedAt: now.Add(-26 * time.Hour)},
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

func (fakeHost) Handle(ctx context.Context, e agentproto.Envelope, out agent.Sender) {
	_ = out.Send(ctx, agentproto.TypeResult, e.ID, agentproto.Result{Code: agentproto.CodeOK})
}

// idleHost has no containers; its agent leaves again at once.
type idleHost struct{ fakeHost }

func (idleHost) Run(ctx context.Context, out agent.Sender) error {
	if err := out.Send(ctx, agentproto.TypeSnapshot, "", agentproto.Snapshot{Containers: []agentproto.Container{}}); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
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
		LabelKey: "rcon.manage", LabelValue: "true", Log: config.Log{Format: "json"}}
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
func seedHosts(ctx context.Context, admin *client, panel, o, agentAddr, root, gameAddr, profile string, st *State) error {
	hostID, err := startAgent(ctx, admin, agentAddr, "host-1", root, fakeHost{})
	if err != nil {
		return err
	}
	gone, cancel := context.WithCancel(ctx)
	spareID, err := startAgent(gone, admin, agentAddr, "host-2", root, idleHost{})
	if err != nil {
		cancel()
		return err
	}
	cancel()
	if err := waitDisconnected(admin, spareID); err != nil {
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
	return admin.call(http.MethodPatch, "/users/"+lockedID, map[string]bool{"disabled": true}, nil)
}
