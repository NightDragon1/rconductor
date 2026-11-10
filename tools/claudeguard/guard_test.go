// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func testEnv(t *testing.T) env {
	t.Helper()
	home := t.TempDir()
	return env{home: home, xdgConfig: home + "/.config", cacheDir: home + "/.cache/" + cacheName}
}

func bashInput(cmd string) string {
	b, _ := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"cwd":        "/w",
		"tool_input": map[string]any{"command": cmd},
	})
	return string(b)
}

type bashCase struct {
	deny  bool
	label string
	cmd   string
}

// loadCases reads testdata/bash-cases.tsv: want, label, command.
func loadCases(t *testing.T) []bashCase {
	t.Helper()
	f, err := os.Open("testdata/bash-cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var cases []bashCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 || (parts[0] != "deny" && parts[0] != "allow") {
			t.Fatalf("bad case line %q", line)
		}
		cases = append(cases, bashCase{deny: parts[0] == "deny", label: parts[1], cmd: parts[2]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return cases
}

// TestBashCases covers the forms C34 (git push), C35 (gh
// api bundled flags), C36 (git metadata and gh settings) and C38 (credential and
// extension commands), the earlier deny list (C27, C28, C30), the
// fail-closed rules (FC) and the everyday commands that stay allowed (ok).
func TestBashCases(t *testing.T) {
	e := testEnv(t)
	for _, c := range loadCases(t) {
		t.Run(c.label+" "+c.cmd, func(t *testing.T) {
			got := run(strings.NewReader(bashInput(c.cmd)), e)
			if c.deny && got == "" {
				t.Errorf("not denied: %s", c.cmd)
			}
			if !c.deny && got != "" {
				t.Errorf("denied (%s): %s", got, c.cmd)
			}
		})
	}
}

// TestBashMultiline covers commands with here-documents and line breaks.
func TestBashMultiline(t *testing.T) {
	e := testEnv(t)
	cases := []bashCase{
		{true, "C34 here-document into bash", "bash <<'E'\ngit push -vvf origin main\nE"},
		{true, "C34 here-string into sh", "sh <<< 'git push --del origin x'"},
		{true, "C34 second line", "git status\ngit push --mirr origin"},
		{true, "C36 here-document into a hook file", "cat > .git/hooks/pre-commit <<'E'\nexit 0\nE"},
		{true, "FC here-document with expansion into bash", "bash <<E\n$cmd\nE"},
		{false, "ok commit message from a here-document", "git commit -m \"$(cat <<'E'\nfeat: x (NFR-013)\n\nbody\nE\n)\""},
		{false, "ok loop over runs", "for id in 1 2; do\n  gh run view \"$id\" --log-failed | tail -5\ndone"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			got := run(strings.NewReader(bashInput(c.cmd)), e)
			if c.deny != (got != "") {
				t.Errorf("deny=%v, got %q for %q", c.deny, got, c.cmd)
			}
		})
	}
}

// TestFileTools covers Edit, Write, MultiEdit and NotebookEdit on git
// metadata and the user git configuration (C36).
func TestFileTools(t *testing.T) {
	e := testEnv(t)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0o750); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repo, ".git"), filepath.Join(wt, "meta")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		deny  bool
		label string
		tool  string
		field string
		path  string
	}{
		{true, "C36 Edit .git/config", "Edit", "file_path", repo + "/.git/config"},
		{true, "C36 Write a hook", "Write", "file_path", repo + "/.git/hooks/pre-commit"},
		{true, "C36 MultiEdit .git/config", "MultiEdit", "file_path", repo + "/.git/config"},
		{true, "C36 NotebookEdit under .git", "NotebookEdit", "notebook_path", repo + "/.git/x.ipynb"},
		{true, "C36 Write the .git file of a worktree", "Write", "file_path", wt + "/.git"},
		{true, "C36 relative path", "Edit", "file_path", ".git/config"},
		{true, "C36 path through ..", "Edit", "file_path", repo + "/src/../.git/config"},
		{true, "C36 symlink into .git", "Write", "file_path", wt + "/meta/config"},
		{true, "C36 user gitconfig", "Edit", "file_path", e.home + "/.gitconfig"},
		{true, "C36 user git config dir", "Write", "file_path", e.xdgConfig + "/git/config"},
		{true, "FC guard cache", "Write", "file_path", e.cacheDir + "/x/claudeguard"},
		{true, "FC no path", "Write", "file_path", ""},
		{false, "ok source file", "Edit", "file_path", repo + "/internal/x.go"},
		{false, "ok .github workflow", "Edit", "file_path", repo + "/.github/workflows/ci.yml"},
		{false, "ok .gitignore", "Write", "file_path", repo + "/.gitignore"},
		{false, "ok .githooks", "Write", "file_path", repo + "/.githooks/pre-commit"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			b, _ := json.Marshal(map[string]any{
				"tool_name":  c.tool,
				"cwd":        repo,
				"tool_input": map[string]any{c.field: c.path},
			})
			got := run(strings.NewReader(string(b)), e)
			if c.deny != (got != "") {
				t.Errorf("deny=%v, got %q for %s", c.deny, got, c.path)
			}
		})
	}
}

// TestInputErrors: input the guard cannot read is denied; other tools pass.
func TestInputErrors(t *testing.T) {
	e := testEnv(t)
	if got := run(strings.NewReader("{"), e); got == "" {
		t.Error("broken JSON was not denied")
	}
	big := strings.NewReader(bashInput("echo " + strings.Repeat("x", maxInput)))
	if got := run(big, e); got == "" {
		t.Error("oversized input was not denied")
	}
	deep := strings.Repeat("bash -c '", 1) + strings.Repeat("eval ", maxDepth+2) + "git status'"
	if got := run(strings.NewReader(bashInput(deep)), e); got == "" {
		t.Error("deep nesting was not denied")
	}
	if got := run(strings.NewReader(`{"tool_name":"Read","tool_input":{"file_path":"/w/.git/config"}}`), e); got != "" {
		t.Errorf("Read was denied: %s", got)
	}
}

// TestParallel runs the decisions concurrently; the guard keeps no shared
// state.
func TestParallel(t *testing.T) {
	e := testEnv(t)
	cases := loadCases(t)
	var wg sync.WaitGroup
	for _, c := range cases {
		wg.Go(func() {
			got := run(strings.NewReader(bashInput(c.cmd)), e)
			if c.deny != (got != "") {
				t.Errorf("deny=%v, got %q for %q", c.deny, got, c.cmd)
			}
		})
	}
	wg.Wait()
}

// TestLargeCommand checks that a long command with many simple commands is
// judged, not rejected for its size.
func TestLargeCommand(t *testing.T) {
	e := testEnv(t)
	cmd := strings.Repeat("git status; ", 5000) + "git push -vvf origin main"
	if got := run(strings.NewReader(bashInput(cmd)), e); got == "" {
		t.Error("the last command of a long list was not checked")
	}
	cmd = strings.Repeat("git status; ", 5000)
	if got := run(strings.NewReader(bashInput(cmd)), e); got != "" {
		t.Errorf("long harmless list denied: %s", got)
	}
}
