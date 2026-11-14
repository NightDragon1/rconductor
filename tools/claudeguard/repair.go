// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// projectURL is the remote of this repository.
const projectURL = "https://github.com/NightDragon1/rconductor.git"

// repairForms are the only git config writes allowed from a tool call
// (PRJ-005): each sets one value of a damaged clone back to its fixed
// value. Every other key, value or option stays refused.
var repairForms = [][]string{
	{"core.bare", "false"},
	{"remote.origin.url", projectURL},
	{"--unset", "remote.origin.mirror"},
	{"--unset-all", "remote.origin.push"},
	{"remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"},
	{"core.hooksPath", ".githooks"},
}

// repairAllowed reports whether a git config write is a repair form run
// in the clone of the guard (its work tree or one of its worktrees), with
// no change of directory in the command.
func (c *checker) repairAllowed(vals []string) bool {
	if !slices.ContainsFunc(repairForms, func(f []string) bool { return slices.Equal(f, vals) }) {
		return false
	}
	if len(c.chdirs) > 0 || c.repoUnknown || c.env.projectRoot == "" {
		return false
	}
	common, ok := commonGitDir(c.cwd)
	return ok && common == filepath.Join(c.env.projectRoot, ".git")
}

// commonGitDir returns the common git directory that git finds in dir,
// links resolved.
func commonGitDir(dir string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitConfigTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	p := strings.TrimSpace(string(out))
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p, true
}
