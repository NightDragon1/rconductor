// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain keeps every git call of the tests away from the calling clone:
// it stops when a git location points into the repository or one of its
// worktrees, then gives the tests their own HOME and git configuration.
func TestMain(m *testing.M) {
	if err := isolateGit(); err != nil {
		fmt.Fprintln(os.Stderr, "test isolation:", err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func isolateGit() error {
	roots, err := repoRoots()
	if err != nil {
		return err
	}
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY"} {
		abs, _ := filepath.Abs(os.Getenv(k))
		for _, r := range roots {
			if os.Getenv(k) != "" && within(abs, r) {
				return fmt.Errorf("%s points into the repository; run the tests without it", k)
			}
		}
		_ = os.Unsetenv(k)
	}
	home, err := os.MkdirTemp("", "claudeguard-home-")
	if err != nil {
		return err
	}
	for k, v := range map[string]string{"HOME": home, "XDG_CONFIG_HOME": home + "/.config",
		"GIT_CONFIG_GLOBAL": home + "/.gitconfig", "GIT_CONFIG_NOSYSTEM": "1"} {
		_ = os.Setenv(k, v)
	}
	return nil
}

// repoRoots returns the work tree of the tests and, in a worktree, the
// clone that owns it (the part of its gitdir before /.git/).
func repoRoots() ([]string, error) {
	cur, err := os.Getwd()
	for ; err == nil; cur = filepath.Dir(cur) {
		if b, rerr := os.ReadFile(filepath.Join(cur, ".git")); rerr == nil {
			clone, _, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(string(b), "gitdir:")), "/.git/")
			return []string{cur, clone}, nil
		}
		if fi, serr := os.Stat(filepath.Join(cur, ".git")); serr == nil && fi.IsDir() {
			return []string{cur}, nil
		}
		if cur == filepath.Dir(cur) {
			err = fmt.Errorf("no repository above the tests")
		}
	}
	return nil, err
}
