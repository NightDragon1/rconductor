// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// testCWD is an empty git repository, the working directory of the Bash
// cases; git reads no user or system configuration in the tests.
var testCWD string

// setupRepo makes testCWD, an empty git repository; TestMain calls it
// after isolating the git environment.
func setupRepo() (func(), error) {
	dir, err := os.MkdirTemp("", "claudeguard-test")
	if err != nil {
		return nil, err
	}
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git init: %s", out)
	}
	testCWD = dir
	return func() { _ = os.RemoveAll(dir) }, nil
}

func testEnv(t *testing.T) env {
	t.Helper()
	home := t.TempDir()
	return env{home: home, xdgConfig: home + "/.config", cacheDir: home + "/.cache/" + cacheName,
		workflowForm: strings.Fields(testForm)}
}

// testForm is a made-up configured form of gh workflow run.
const testForm = "test.yml -R owner/repo-x -f ref=<sha>"

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s", args, out)
	}
}

func bashInput(cmd string) string { return bashInputIn(cmd, testCWD) }

func bashInputIn(cmd, cwd string) string {
	b, _ := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"cwd":        cwd,
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

func TestWorkflowFormFile(t *testing.T) {
	run := "gh workflow run test.yml -R owner/repo-x -f ref=" + testSHA
	for _, tc := range []struct {
		name, content string
		allow         bool
	}{
		{"missing file", "", false},
		{"empty file", "\n", false},
		{"form", "# comment\n" + testForm + "\n", true},
		{"two forms", testForm + "\n" + testForm + "\n", false},
		{"no placeholder", "test.yml -R owner/repo-x -f ref=x\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := testEnv(t)
			e.workflowForm = nil
			dir := filepath.Join(e.xdgConfig, "rconductor-guard")
			if tc.content != "" {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "workflows"), []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			loadWorkflowForm(&e)
			if got := checkBash(run, "/w", e) == ""; got != tc.allow {
				t.Errorf("allowed = %t, want %t", got, tc.allow)
			}
			if checkBash("gh workflow run ci.yml --ref main", "/w", e) != "" {
				t.Error("ci.yml on main is denied")
			}
			if checkBash("gh workflow run release.yml --ref main", "/w", e) == "" {
				t.Error("release.yml is allowed")
			}
			if checkEditPath(filepath.Join(dir, "workflows"), "/w", e) == "" {
				t.Error("the configuration is writable")
			}
			// A linked file: its target is protected as well.
			target := filepath.Join(t.TempDir(), "workflows")
			if err := os.WriteFile(target, []byte(testForm), 0o600); err != nil {
				t.Fatal(err)
			}
			_ = os.MkdirAll(dir, 0o700)
			_ = os.Remove(filepath.Join(dir, "workflows"))
			if err := os.Symlink(target, filepath.Join(dir, "workflows")); err != nil {
				t.Fatal(err)
			}
			loadWorkflowForm(&e)
			if checkBash(run, "/w", e) != "" || checkEditPath(target, "/w", e) == "" {
				t.Error("linked form denied or its target writable")
			}
		})
	}
}

// TestPushRemoteConfig: git push is denied when the repository it runs in
// has a mirror remote or a forcing or deleting push refspec, also through
// a worktree, -C, --git-dir, cd and the global configuration, and when the
// configuration cannot be read.
func TestPushRemoteConfig(t *testing.T) {
	e := testEnv(t)
	repo := func(kv ...string) string {
		dir := t.TempDir()
		gitIn(t, dir, "init", "-q")
		for i := 0; i+1 < len(kv); i += 2 {
			gitIn(t, dir, "config", kv[i], kv[i+1])
		}
		return dir
	}
	plain := repo()
	mirror := repo("remote.origin.url", "https://example.invalid/r.git", "remote.origin.mirror", "true")
	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, mirror, "worktree", "add", "-q", "--orphan", "-b", "wt", wt)
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte("[remote \"x\"]\n\tpush = +refs/heads/*\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		deny             bool
		label            string
		cwd, cmd, global string
	}{
		{false, "no push settings", plain, "git push origin main", ""},
		{true, "mirror remote", mirror, "git push origin main", ""},
		{true, "worktree of a mirror repository", wt, "git push origin main", ""},
		{true, "forcing push refspec", repo("remote.origin.push", "+refs/heads/*:refs/heads/*"), "git push", ""},
		{true, "deleting push refspec", repo("remote.origin.push", ":refs/heads/x"), "git push", ""},
		{false, "plain push refspec", repo("remote.origin.push", "refs/heads/main:refs/heads/main"), "git push", ""},
		{false, "mirror off", repo("remote.origin.mirror", "false"), "git push", ""},
		{true, "-C into a mirror repository", plain, "git -C " + mirror + " push origin main", ""},
		{true, "--git-dir of a mirror repository", plain, "git --git-dir=" + mirror + "/.git push origin main", ""},
		{true, "cd into a mirror repository", plain, "cd " + mirror + " && git push origin main", ""},
		{true, "push before cd in a loop", plain, "for d in 1 2; do git push; cd " + wt + "; done", ""},
		{true, "global configuration", plain, "git push", global},
		{false, "no push in a mirror repository", mirror, "git status", ""},
		{true, "unreadable configuration", filepath.Join(plain, "missing"), "git push", ""},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			if c.global != "" {
				t.Setenv("GIT_CONFIG_GLOBAL", c.global)
			}
			got := run(strings.NewReader(bashInputIn(c.cmd, c.cwd)), e)
			if c.deny != (got != "") {
				t.Errorf("deny=%v, got %q for %q in %s", c.deny, got, c.cmd, c.cwd)
			}
		})
	}
}
