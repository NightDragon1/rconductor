// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/agentproto"
)

// TestSelfLogs covers D-091: the agent gives the logs of its own container
// without the label, and nothing else: no action and no version search on
// it, other unlabeled containers stay forbidden, and outside a container
// there is no own container.
func TestSelfLogs(t *testing.T) {
	d, fd, _ := newTestDocker(t)
	if r, _ := order(t, d, agentproto.TypeLogsTail, agentproto.LogsTail{Container: agentproto.SelfContainer, Lines: 5}); r.Code != agentproto.CodeForbidden ||
		slices.Contains(d.Capabilities(), agentproto.CapSelfLogs) {
		t.Errorf("outside a container: %+v %v", r, d.Capabilities())
	}
	d.selfID = "id-db" // the agent's container carries no label
	fd.logs = mux(1, "agent started\n")
	r, rec := order(t, d, agentproto.TypeLogsTail, agentproto.LogsTail{Container: agentproto.SelfContainer, Lines: 5})
	ll, _ := agentproto.Payload[agentproto.LogLines](rec.of(agentproto.TypeLogLines)[0])
	if r.Code != agentproto.CodeOK || strings.Join(ll.Lines, "|") != "agent started" || !slices.Contains(d.Capabilities(), agentproto.CapSelfLogs) {
		t.Errorf("own logs: %+v %q", r, ll.Lines)
	}
	if r, _ := order(t, d, agentproto.TypeLogsStop, agentproto.LogsFollow{Container: agentproto.SelfContainer}); r.Code != agentproto.CodeOK {
		t.Errorf("logs.stop: %+v", r)
	}
	for typ, payload := range map[string]any{
		agentproto.TypeContainerAction: agentproto.ContainerAction{Container: agentproto.SelfContainer, Action: "restart"},
		agentproto.TypeVersionDetect:   agentproto.VersionDetect{Container: agentproto.SelfContainer, Pattern: "x"},
	} {
		if r, _ := order(t, d, typ, payload); r.Code != agentproto.CodeInvalid {
			t.Errorf("%s on the own container: %+v", typ, r)
		}
	}
	if r, _ := order(t, d, agentproto.TypeLogsTail, agentproto.LogsTail{Container: "db", Lines: 5}); r.Code != agentproto.CodeForbidden {
		t.Errorf("the own container by name: %+v", r)
	}
	if len(fd.actions) != 0 {
		t.Errorf("docker called: %v", fd.actions)
	}
}

// TestOwnContainerID reads the ID from the mounts of a container; outside
// one it is empty.
func TestOwnContainerID(t *testing.T) {
	dir := t.TempDir()
	id := strings.Repeat("ab12", 16)
	in := filepath.Join(dir, "in")
	_ = os.WriteFile(in, []byte("1 2 0:1 / / rw - overlay overlay rw\n3 1 8:1 /var/lib/docker/containers/"+id+"/hostname /etc/hostname rw - ext4 /dev/sda1 rw\n"), 0o600)
	out := filepath.Join(dir, "out")
	_ = os.WriteFile(out, []byte("1 2 0:1 / / rw - ext4 /dev/sda1 rw\n"), 0o600)
	if got := ownContainerID(in); got != id {
		t.Errorf("in a container: %q", got)
	}
	if got := ownContainerID(out) + ownContainerID(filepath.Join(dir, "none")); got != "" {
		t.Errorf("outside: %q", got)
	}
}
