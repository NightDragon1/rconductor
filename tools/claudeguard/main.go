// SPDX-License-Identifier: AGPL-3.0-or-later

// Command claudeguard is the PreToolUse hook of Claude Code for this
// repository (NFR-013). It is the second layer behind the deny patterns in
// .claude/settings.json: the patterns match the command text, the guard
// parses it.
//
// Input is the hook JSON on stdin. For Bash the command is parsed as bash
// (mvdan.cc/sh/v3/syntax); every simple command is checked, also inside
// pipelines, lists, subshells, command substitutions, functions and loops,
// and the arguments of bash -c, sh -c, eval, env, command, exec, nice,
// timeout, nohup, stdbuf, setsid, sudo, time, xargs, find -exec, watch and
// flock. Each git and gh call is normalized before it is judged:
//
//   - git: global options before the subcommand are skipped (-C, -c,
//     --git-dir, --no-pager and the rest), abbreviated long options are
//     resolved by unique prefix as git's parse-options does, bundled short
//     options are expanded, and +refspecs and :refspecs are recognized after
//     quote removal.
//   - gh: the request method, field and input flags of gh api are read in
//     every pflag form (-XPUT, -iX PUT, --method=PUT, -iF k=v); a call with
//     a field or input flag or a method other than GET or HEAD writes.
//
// A git or gh call is allowed only when its subcommand is on a known list;
// a call the guard cannot read (parse error, a command name or argument
// that is not literal where it decides, an unknown subcommand or option) is
// denied. Edit, Write, MultiEdit and NotebookEdit are denied for paths in a
// .git directory or a .git file, for the user's git configuration and for
// the guard's own build cache.
//
// A denied call exits with status 2 and the reason on stderr; an allowed
// call exits 0 without output, so the normal permission flow applies.
// .claude/hooks/guard.sh builds this program once per source hash into the
// user cache and runs it.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// maxInput bounds the hook input that is read from stdin.
const maxInput = 16 << 20

// cacheName is the directory of the guard's build cache in the user cache.
const cacheName = "rconductor-claudeguard"

type hookInput struct {
	ToolName  string `json:"tool_name"`
	CWD       string `json:"cwd"`
	ToolInput struct {
		Command      string `json:"command"`
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	} `json:"tool_input"`
}

func main() {
	if reason := run(os.Stdin, defaultEnv()); reason != "" {
		fmt.Fprintln(os.Stderr, "RCONductor guard: "+reason)
		os.Exit(2)
	}
}

// run reads the hook input and returns the reason for a denial, or "" when
// the guard has no objection.
func run(r io.Reader, e env) (reason string) {
	defer func() {
		if p := recover(); p != nil {
			reason = fmt.Sprintf("internal error, call denied: %v", p)
		}
	}()
	data, err := io.ReadAll(io.LimitReader(r, maxInput+1))
	if err != nil {
		return "hook input could not be read"
	}
	if len(data) > maxInput {
		return "hook input is too large"
	}
	var in hookInput
	if err := json.Unmarshal(data, &in); err != nil {
		return "hook input is not valid JSON"
	}
	return decide(&in, e)
}

// env holds the paths the decisions depend on.
type env struct {
	home      string
	xdgConfig string
	cacheDir  string
}

func defaultEnv() env {
	home, _ := os.UserHomeDir()
	e := env{home: home, xdgConfig: os.Getenv("XDG_CONFIG_HOME")}
	if e.xdgConfig == "" && home != "" {
		e.xdgConfig = home + "/.config"
	}
	if c := os.Getenv("XDG_CACHE_HOME"); c != "" {
		e.cacheDir = c + "/" + cacheName
	} else if home != "" {
		e.cacheDir = home + "/.cache/" + cacheName
	}
	return e
}

func decide(in *hookInput, e env) string {
	switch in.ToolName {
	case "Bash":
		return checkBash(in.ToolInput.Command, in.CWD, e)
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		p := in.ToolInput.FilePath
		if p == "" {
			p = in.ToolInput.NotebookPath
		}
		if p == "" {
			return in.ToolName + " without a file path"
		}
		return checkEditPath(p, in.CWD, e)
	}
	return ""
}
