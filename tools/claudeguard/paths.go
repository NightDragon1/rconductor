// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"path/filepath"
	"strings"
)

// checkEditPath judges a file tool's target. The path is checked as given
// and as it resolves through symbolic links of its existing part.
func checkEditPath(p, cwd string, e env) string {
	abs := absPath(p, cwd, e)
	if r := protectedPath(abs, e); r != "" {
		return r
	}
	if resolved := resolveExisting(abs); resolved != abs {
		if r := protectedPath(resolved, e); r != "" {
			return r
		}
	}
	return ""
}

// absPath makes p absolute against cwd and expands a leading "~/".
func absPath(p, cwd string, e env) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = e.home + p[1:]
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p)
}

// resolveExisting resolves symbolic links in the longest existing prefix
// of p and appends the rest unchanged.
func resolveExisting(p string) string {
	rest := ""
	cur := p
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// protectedPath returns a reason when an absolute, clean path lies in a
// git directory, is a .git file, is the user's git configuration or lies
// in the guard's build cache.
func protectedPath(p string, e env) string {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".git" {
			return "writes to .git are not allowed from tool calls: " + p
		}
	}
	if e.home != "" {
		if p == filepath.Join(e.home, ".gitconfig") {
			return "writes to the user git configuration are not allowed: " + p
		}
	}
	if e.xdgConfig != "" && within(p, filepath.Join(e.xdgConfig, "git")) {
		return "writes to the user git configuration are not allowed: " + p
	}
	if e.cacheDir != "" && within(p, e.cacheDir) {
		return "the guard's build cache is not writable from tool calls"
	}
	return ""
}

func within(p, dir string) bool {
	dir = filepath.Clean(dir)
	return p == dir || strings.HasPrefix(p, dir+"/")
}
