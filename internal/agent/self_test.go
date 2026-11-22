// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/agentproto"
)

// TestC319OwnContainerFromHostname: only the mount of /etc/hostname names
// the own container; a mount of a foreign container before it does not
// (D-091).
func TestC319OwnContainerFromHostname(t *testing.T) {
	dir := t.TempDir()
	own, foreign := strings.Repeat("ab12", 16), strings.Repeat("cd34", 16)
	p := filepath.Join(dir, "mountinfo")
	_ = os.WriteFile(p, []byte("1 2 0:1 / / rw - overlay overlay rw\n"+
		"2 1 0:5 /var/lib/docker/containers/"+foreign+"/mounts/shm /dev/shm rw - tmpfs shm rw\n"+
		"3 1 8:1 /var/lib/docker/containers/"+own+"/hostname /etc/hostname rw - ext4 /dev/sda1 rw\n"), 0o600)
	if got := ownContainerID(p); got != own {
		t.Errorf("own container = %q, want %q", got, own)
	}
	host := filepath.Join(dir, "host")
	_ = os.WriteFile(host, []byte("2 1 0:5 /var/lib/docker/containers/"+foreign+"/mounts/shm /dev/shm rw - tmpfs shm rw\n"), 0o600)
	if got := ownContainerID(host); got != "" {
		t.Errorf("on the host = %q, want none", got)
	}
}

// TestC321SelfUnknownToTheDaemon: an own container the daemon does not
// know (remote daemon, Docker in Docker) gives no own logs: no capability,
// and an order for it is not found, not forbidden (D-091).
func TestC321SelfUnknownToTheDaemon(t *testing.T) {
	d, _, logs := newTestDocker(t)
	d.selfID = "id-elsewhere"
	d.checkSelf(context.Background())
	if d.selfID != "" || slices.Contains(d.Capabilities(), agentproto.CapSelfLogs) {
		t.Errorf("unknown own container kept: %q %v", d.selfID, d.Capabilities())
	}
	d.selfID = "id-db"
	d.checkSelf(context.Background())
	if d.selfID != "id-db" {
		t.Error("known own container dropped")
	}
	d.selfID = "id-gone" // removed after the start
	if r, _ := order(t, d, agentproto.TypeLogsTail, agentproto.LogsTail{Container: agentproto.SelfContainer, Lines: 5}); r.Code != agentproto.CodeNotFound {
		t.Errorf("gone own container: %+v", r)
	}
	if strings.Contains(logs.String(), "agent.forbidden") {
		t.Error("a missing own container logged as forbidden")
	}
}

// TestC320FollowSelf: logs.follow reaches the own container without the
// label, and is forbidden outside a container (D-091).
func TestC320FollowSelf(t *testing.T) {
	d, fd, _ := newTestDocker(t)
	if r, _ := order(t, d, agentproto.TypeLogsFollow, agentproto.LogsFollow{Container: agentproto.SelfContainer}); r.Code != agentproto.CodeForbidden {
		t.Errorf("outside a container: %+v", r)
	}
	d.selfID = "id-db"
	fd.logs = mux(1, "agent started\n")
	if r, _ := order(t, d, agentproto.TypeLogsFollow, agentproto.LogsFollow{Container: agentproto.SelfContainer}); r.Code != agentproto.CodeOK {
		t.Errorf("own container: %+v", r)
	}
	if r, _ := order(t, d, agentproto.TypeLogsStop, agentproto.LogsFollow{Container: agentproto.SelfContainer}); r.Code != agentproto.CodeOK {
		t.Errorf("logs.stop: %+v", r)
	}
}

// TestC73ActionLoggedBeforeTheCall: the order shows in the log before the
// call to Docker, and the result after it (AGT-003).
func TestC73ActionLoggedBeforeTheCall(t *testing.T) {
	d, _, logs := newTestDocker(t)
	if r, _ := order(t, d, agentproto.TypeContainerAction, agentproto.ContainerAction{Container: "mc", Action: "restart"}); r.Code != agentproto.CodeOK {
		t.Fatalf("restart: %+v", r)
	}
	out := logs.String()
	before, after := strings.Index(out, "container action requested"), strings.Index(out, `"docker.action"`)
	if before < 0 || after < before {
		t.Errorf("log order: requested at %d, result at %d", before, after)
	}
}
