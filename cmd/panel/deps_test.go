// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestNoDockerClient covers AGT-001: the panel binary contains no Docker
// client; only the agent talks to the Docker socket.
func TestNoDockerClient(t *testing.T) {
	deps := func(pkg string) string {
		t.Helper()
		out, err := exec.CommandContext(context.Background(), "go", "list", "-deps", pkg).Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pkg, err)
		}
		return string(out)
	}
	for line := range strings.Lines(deps(".")) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "github.com/moby/") || strings.HasPrefix(line, "github.com/docker/") {
			t.Errorf("panel depends on %s", line)
		}
	}
	if !strings.Contains(deps("../agent"), "github.com/moby/moby/client\n") {
		t.Error("check broken: the agent should use the Docker client")
	}
}

// TestNoTestTools: the e2e stack with its in-process agent and stand-in
// for Docker, the agent itself and the RCON test server are test tools
// and never part of the panel binary. The stack is a main package of its
// own that nothing imports; this keeps it so.
func TestNoTestTools(t *testing.T) {
	out, err := exec.CommandContext(context.Background(), "go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	const mod = "github.com/NightDragon1/rconductor/"
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, mod+"internal/e2e"),
			line == mod+"internal/agent",
			strings.HasPrefix(line, mod+"internal/rcon/rcontest"):
			t.Errorf("panel depends on the test tool %s", line)
		}
	}
	stack, err := exec.CommandContext(context.Background(), "go", "list", "-deps", "../../internal/e2e/stack").Output()
	if err != nil {
		t.Fatalf("go list stack: %v", err)
	}
	if !strings.Contains(string(stack), mod+"internal/agent\n") {
		t.Error("check broken: the stack should contain the agent")
	}
}
