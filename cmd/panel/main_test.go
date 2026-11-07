// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestProfileCheckWithoutConfiguration covers CAT-006: the panel binary
// checks profiles with an empty environment, so no configuration, database
// or network is needed, as in docker run with the panel image.
func TestProfileCheckWithoutConfiguration(t *testing.T) {
	ctx := context.Background()
	bin := filepath.Join(t.TempDir(), "panel")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	run := func(args ...string) (int, string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = []string{}
		cmd.Dir = t.TempDir()
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		switch {
		case err == nil:
			return 0, string(out)
		case errors.As(err, &exit):
			return exit.ExitCode(), string(out)
		default:
			t.Fatal(err)
			return 0, ""
		}
	}
	shipped, err := filepath.Abs("../../profiles/minecraft-java.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if code, out := run("profile", "check", shipped); code != 0 || !strings.Contains(out, ": ok") {
		t.Errorf("shipped profile: %d %q", code, out)
	}
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("schema: 2\nid: Bad\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := run("profile", "check", bad); code != 1 || !strings.Contains(out, bad+":1: schema must be 1") || !strings.Contains(out, bad+":2: id is required") {
		t.Errorf("bad profile: %d %q", code, out)
	}
	if code, _ := run("profile"); code != 2 {
		t.Errorf("profile without check: %d", code)
	}
}
