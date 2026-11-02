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
