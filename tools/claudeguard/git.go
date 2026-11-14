// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// blockedConfigPrefixes are git configuration keys that redirect remotes,
// load further configuration or define commands. Section and key names are
// compared in lower case, as git does.
var blockedConfigPrefixes = []string{"remote.", "include.", "includeif.", "alias.", "url."}

func blockedConfigKey(k string) bool {
	k = strings.ToLower(k)
	// core.hooksPath decides which hooks run.
	if k == "core.hookspath" {
		return true
	}
	for _, p := range blockedConfigPrefixes {
		if strings.HasPrefix(k, p) || k == strings.TrimSuffix(p, ".") {
			return true
		}
	}
	return false
}

// gitReadOnly are subcommands that do not change refs on a remote and
// accept any arguments.
var gitReadOnly = setOf("status", "log", "diff", "show", "rev-parse", "rev-list", "ls-files", "ls-tree",
	"ls-remote", "cat-file", "blame", "annotate", "shortlog", "describe", "merge-base", "show-ref",
	"for-each-ref", "name-rev", "diff-tree", "diff-index", "diff-files", "whatchanged", "reflog",
	"check-ignore", "check-attr", "check-ref-format", "var", "version", "help", "count-objects",
	"fsck", "verify-commit", "verify-tag", "verify-pack", "range-diff", "format-patch", "archive",
	"cherry", "show-branch", "get-tar-commit-id")

// gitLocal are subcommands that write only to the local repository or
// fetch from a remote; they accept any arguments.
var gitLocal = setOf("add", "commit", "checkout", "switch", "restore", "reset", "merge", "cherry-pick",
	"revert", "stash", "worktree", "branch", "fetch", "pull", "mv", "rm", "clean", "apply", "am",
	"init", "clone", "notes", "gc", "prune", "maintenance", "update-index", "read-tree", "write-tree",
	"commit-tree", "hash-object", "update-ref", "symbolic-ref", "sparse-checkout", "mktree", "mktag",
	"pack-refs", "repack", "checkout-index", "merge-file", "merge-tree")

// git checks a git call; args follow the program name.
func (c *checker) git(args []arg, depth int) {
	var target pushTarget
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a.dyn {
			// A dynamic directory only selects the repository.
			if hasAnyPrefix(a.val, gitDirOptsEq...) && !a.split {
				target.unknown = target.unknown || strings.HasPrefix(a.val, "--git-dir=")
				continue
			}
			c.deny("git argument before the subcommand is not literal")
			return
		}
		v := a.val
		if !strings.HasPrefix(v, "-") {
			break
		}
		switch {
		case v == "-C" || v == "--git-dir" || v == "--work-tree" || v == "--namespace" ||
			v == "--super-prefix" || v == "--attr-source":
			// The value only selects a directory or namespace; the
			// decision follows the subcommand.
			i++
			if i < len(args) && args[i].split {
				c.deny("git " + v + " with a value that may split into several words cannot be checked")
				return
			}
			if i < len(args) && (v == "-C" || v == "--git-dir") {
				switch {
				case args[i].dyn:
					target.unknown = true
				case v == "-C":
					target.dirs = append(target.dirs, args[i].val)
				default:
					target.gitDir = args[i].val
				}
			}
		case v == "-c" || v == "--config-env":
			i++
			if i >= len(args) {
				return
			}
			if args[i].dyn {
				c.deny("git " + v + " with a value that is not literal")
				return
			}
			if !c.gitConfigParam(args[i].val) {
				return
			}
		case strings.HasPrefix(v, "--config-env="):
			if !c.gitConfigParam(strings.TrimPrefix(v, "--config-env=")) {
				return
			}
		case strings.HasPrefix(v, "--git-dir="):
			target.gitDir = strings.TrimPrefix(v, "--git-dir=")
		case strings.HasPrefix(v, "--work-tree=") ||
			strings.HasPrefix(v, "--namespace=") || strings.HasPrefix(v, "--super-prefix=") ||
			strings.HasPrefix(v, "--attr-source=") || strings.HasPrefix(v, "--exec-path=") ||
			strings.HasPrefix(v, "--list-cmds="):
		case gitGlobalFlags[v]:
		default:
			c.deny("unknown git option before the subcommand: " + v)
			return
		}
	}
	if i >= len(args) {
		return
	}
	sub := args[i]
	if sub.dyn {
		c.deny("the git subcommand is not literal, so the call cannot be checked")
		return
	}
	rest := args[i+1:]
	switch name := sub.val; {
	case name == "push":
		c.gitPush(rest)
		c.pushes = append(c.pushes, target)
	case name == "config":
		c.gitConfig(rest)
	case name == "remote":
		c.gitRemote(rest)
	case name == "tag":
		c.gitTag(rest)
	case name == "rebase":
		if c.gitHooks(name, rest, rebaseHooks) {
			c.gitRebase(rest, depth)
		}
	case name == "commit" || name == "am" || name == "merge" || name == "pull":
		c.gitHooks(name, rest, hookOpts[name])
	case name == "bisect":
		if len(rest) > 1 && rest[0].is("run") {
			c.call(rest[1:], nil, depth)
		}
	case name == "submodule":
		for _, a := range rest {
			if a.dyn || a.val == "foreach" {
				c.deny("git submodule foreach runs commands the guard cannot check")
				return
			}
		}
	case name == "grep":
		c.gitGrep(rest)
	case gitReadOnly[name] || gitLocal[name]:
	default:
		c.deny("git " + name + " is not on the guard's list of known subcommands")
	}
}

var gitDirOptsEq = []string{"--git-dir=", "--work-tree=", "--namespace=", "--super-prefix=", "--attr-source="}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

var gitGlobalFlags = setOf("-p", "--paginate", "-P", "--no-pager", "--no-replace-objects", "--bare",
	"--literal-pathspecs", "--glob-pathspecs", "--noglob-pathspecs", "--icase-pathspecs",
	"--no-optional-locks", "--no-advice", "--no-lazy-fetch", "--exec-path", "--html-path",
	"--man-path", "--info-path", "--version", "-v", "--help", "-h")

// gitConfigParam checks the key of -c key=value or --config-env
// key=envvar; it returns false after a denial.
func (c *checker) gitConfigParam(p string) bool {
	k, _, _ := strings.Cut(p, "=")
	if blockedConfigKey(k) {
		c.deny("git configuration " + k + " may not be set from a tool call")
		return false
	}
	return true
}

// parseOpts describes the options of a git subcommand for parse-options.
type parseOpts struct {
	long      []string        // all long option names
	valLong   map[string]bool // long options with a required value
	flagShort string          // short options without a value
	valShort  string          // short options with a value
	// numShort are short options whose value is attached digits (tag -n).
	numShort string
}

// opt is one option a git command line sets. When an abbreviation is
// ambiguous, git refuses the call; the guard keeps every candidate.
type opt struct {
	names   []string // canonical long names or one short letter
	negated []bool
}

// parse splits git subcommand arguments into options and operands. ok is
// false after a denial.
func (c *checker) parse(cmd string, args []arg, p parseOpts) (opts []opt, operands []arg, ok bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a.dyn {
			if !a.opaque() {
				c.deny("git " + cmd + " with an argument that is not literal cannot be checked")
				return nil, nil, false
			}
			operands = append(operands, a)
			continue
		}
		v := a.val
		switch {
		case v == "--":
			for _, b := range args[i+1:] {
				if b.dyn && !b.opaque() {
					c.deny("git " + cmd + " with an argument that is not literal cannot be checked")
					return nil, nil, false
				}
			}
			return opts, append(operands, args[i+1:]...), true
		case strings.HasPrefix(v, "--"):
			name, _, hasVal := strings.Cut(v[2:], "=")
			o := resolveLong(name, p.long)
			if len(o.names) == 0 {
				c.deny("git " + cmd + " with the unknown option --" + name)
				return nil, nil, false
			}
			takesVal := false
			for k, n := range o.names {
				if !o.negated[k] && p.valLong[n] {
					takesVal = true
				}
			}
			if takesVal && !hasVal {
				i++
			}
			opts = append(opts, o)
		case strings.HasPrefix(v, "-") && v != "-":
			for j := 1; j < len(v); j++ {
				ch := v[j]
				switch {
				case strings.IndexByte(p.valShort, ch) >= 0:
					opts = append(opts, opt{names: []string{string(ch)}, negated: []bool{false}})
					if j == len(v)-1 {
						i++
					}
					j = len(v)
				case strings.IndexByte(p.numShort, ch) >= 0:
					opts = append(opts, opt{names: []string{string(ch)}, negated: []bool{false}})
					j = len(v)
				case strings.IndexByte(p.flagShort, ch) >= 0:
					opts = append(opts, opt{names: []string{string(ch)}, negated: []bool{false}})
				default:
					c.deny("git " + cmd + " with the unknown option -" + string(ch))
					return nil, nil, false
				}
			}
		default:
			operands = append(operands, a)
		}
	}
	return opts, operands, true
}

// resolveLong resolves a long option name as git's parse-options does: an
// exact name, its "no-" negation, or every option the name abbreviates.
func resolveLong(name string, long []string) opt {
	for _, l := range long {
		if name == l {
			return opt{names: []string{l}, negated: []bool{false}}
		}
	}
	if n, ok := strings.CutPrefix(name, "no-"); ok {
		for _, l := range long {
			if n == l {
				return opt{names: []string{l}, negated: []bool{true}}
			}
		}
	}
	for _, l := range long {
		if n, ok := strings.CutPrefix(l, "no-"); ok && n == name {
			return opt{names: []string{l}, negated: []bool{true}}
		}
	}
	var o opt
	for _, l := range long {
		if name != "" && strings.HasPrefix(l, name) {
			o.names = append(o.names, l)
			o.negated = append(o.negated, false)
		}
		if strings.HasPrefix("no-"+l, name) && len(name) > 0 && !strings.HasPrefix(l, "no-") {
			o.names = append(o.names, l)
			o.negated = append(o.negated, true)
		}
	}
	return o
}

// has reports whether an option may set one of names (not negated).
func (o opt) has(names ...string) bool {
	for k, n := range o.names {
		if !o.negated[k] && contains(names, n) {
			return true
		}
	}
	return false
}

var pushOpts = parseOpts{
	long: []string{"verbose", "quiet", "repo", "all", "branches", "mirror", "delete", "tags", "dry-run",
		"porcelain", "force", "force-with-lease", "force-if-includes", "recurse-submodules", "thin",
		"receive-pack", "exec", "set-upstream", "progress", "prune", "no-verify", "follow-tags",
		"signed", "atomic", "push-option", "ipv4", "ipv6"},
	valLong:   setOf("repo", "recurse-submodules", "receive-pack", "exec", "push-option"),
	flagShort: "vqdnfu46",
	valShort:  "o",
}

func (c *checker) gitPush(args []arg) {
	opts, operands, ok := c.parse("push", args, pushOpts)
	if !ok {
		return
	}
	for _, o := range opts {
		if o.has("force", "force-with-lease", "force-if-includes", "f") {
			c.deny("git push with force is not allowed")
			return
		}
		if o.has("delete", "d") {
			c.deny("git push --delete is not allowed")
			return
		}
		if o.has("mirror", "prune") {
			c.deny("git push --mirror and --prune are not allowed")
			return
		}
		if o.has("no-verify") {
			c.deny("git push --no-verify skips the hooks and is not allowed")
			return
		}
	}
	for _, a := range operands {
		if strings.HasPrefix(a.val, "+") {
			c.deny("git push with a +refspec forces the update and is not allowed")
			return
		}
		if strings.HasPrefix(a.val, ":") {
			c.deny("git push with a :refspec deletes a remote ref and is not allowed")
			return
		}
	}
}

var configOpts = parseOpts{
	long: []string{"global", "system", "local", "worktree", "file", "blob", "get", "get-all",
		"get-regexp", "get-urlmatch", "replace-all", "add", "unset", "unset-all", "rename-section",
		"remove-section", "list", "fixed-value", "edit", "get-color", "get-colorbool", "type", "bool",
		"int", "bool-or-int", "bool-or-str", "path", "expiry-date", "null", "name-only", "includes",
		"show-origin", "show-scope", "default", "comment", "value", "url", "all", "regexp", "append"},
	valLong:   setOf("file", "blob", "type", "default", "comment", "value", "url"),
	flagShort: "lez",
	valShort:  "f",
}

var configReadOps = []string{"get", "get-all", "get-regexp", "get-urlmatch", "list", "l", "get-color", "get-colorbool"}

func (c *checker) gitConfig(args []arg) {
	for _, a := range args {
		if a.dyn {
			c.deny("git config with an argument that is not literal cannot be checked")
			return
		}
	}
	opts, operands, ok := c.parse("config", args, configOpts)
	if !ok {
		return
	}
	sub := ""
	if len(operands) > 0 {
		sub = operands[0].val
	}
	if sub == "edit" {
		c.deny("git config edit is not allowed from tool calls")
		return
	}
	readOp, writeOp := false, false
	for _, o := range opts {
		if o.has("edit", "e") {
			c.deny("git config --edit is not allowed from tool calls")
			return
		}
		readOp = readOp || o.has(configReadOps...)
		writeOp = writeOp || o.has("replace-all", "add", "unset", "unset-all", "rename-section", "remove-section")
	}
	switch sub {
	case "get", "list":
		readOp = true
	case "set", "unset", "rename-section", "remove-section":
		writeOp = true
	}
	read := !writeOp && (readOp || len(operands) <= 1)
	if read {
		return
	}
	vals := make([]string, len(args))
	for i, a := range args {
		vals[i] = a.val
	}
	if !c.repairAllowed(vals) {
		c.deny("git config may write only the fixed repair values of this clone from a tool call")
	}
}

func (c *checker) gitRemote(args []arg) {
	for _, a := range args {
		if a.dyn {
			c.deny("git remote with an argument that is not literal cannot be checked")
			return
		}
		switch a.val {
		case "-v", "--verbose":
			continue
		case "show", "get-url", "prune", "update", "set-head":
			return
		}
		c.deny("git remote " + a.val + " changes remote configuration and is not allowed")
		return
	}
}

var tagOpts = parseOpts{
	long: []string{"list", "delete", "verify", "contains", "no-contains", "merged", "no-merged", "sort",
		"points-at", "format", "color", "ignore-case", "column", "annotate", "sign", "local-user",
		"force", "message", "file", "edit", "cleanup", "create-reflog", "trailer", "omit-empty"},
	valLong: setOf("contains", "no-contains", "merged", "no-merged", "sort", "points-at", "format",
		"local-user", "message", "file", "cleanup", "trailer"),
	flagShort: "ldvasfei",
	valShort:  "umF",
	numShort:  "n",
}

func (c *checker) gitTag(args []arg) {
	opts, _, ok := c.parse("tag", args, tagOpts)
	if !ok {
		return
	}
	for _, o := range opts {
		if o.has("delete", "d") {
			c.deny("git tag --delete is not allowed")
			return
		}
	}
}

// hookSpec describes the options of a git command that takes --no-verify.
type hookSpec struct {
	shortN   bool     // -n means --no-verify
	valShort string   // short options with a value
	optShort string   // short options with an optional attached value
	valLong  []string // long options with a required value
}

var hookOpts = map[string]hookSpec{
	"commit": {
		shortN: true, valShort: "FmcCtU", optShort: "Su",
		valLong: []string{"file", "message", "reedit-message", "reuse-message", "fixup", "squash", "author",
			"date", "template", "cleanup", "trailer", "pathspec-from-file", "unified"},
	},
	"am": {
		shortN: true, valShort: "Cp", optShort: "S",
		valLong: []string{"whitespace", "directory", "exclude", "include", "patch-format", "resolvemsg", "quoted-cr", "empty"},
	},
	"merge": {
		valShort: "mFsX", optShort: "S",
		valLong: []string{"message", "file", "strategy", "strategy-option", "cleanup", "into-name"},
	},
	"pull": {
		valShort: "sXo", optShort: "Sr",
		valLong: []string{"strategy", "strategy-option", "depth", "deepen", "shallow-since", "shallow-exclude",
			"upload-pack", "server-option", "negotiation-tip", "refmap", "cleanup"},
	},
}

var rebaseHooks = hookSpec{
	valShort: "sXxC", optShort: "S",
	valLong: []string{"onto", "strategy", "strategy-option", "exec", "whitespace", "empty"},
}

// gitHooks denies --no-verify in every form git accepts: exact, as an
// abbreviation, and as -n where it means --no-verify (commit, am), also in
// a bundle. A dynamic word is accepted as the value of an option or when
// it starts with literal text that is not an option. It returns false
// after a denial.
func (c *checker) gitHooks(cmd string, args []arg, h hookSpec) bool {
	deny := func() bool {
		c.deny("git " + cmd + " --no-verify skips the hooks and is not allowed")
		return false
	}
	valueNext := false
	for _, a := range args {
		if a.dyn {
			if valueNext && !a.split {
				valueNext = false
				continue
			}
			if !a.opaque() {
				c.deny("git " + cmd + " with an argument that is not literal cannot be checked")
				return false
			}
			valueNext = false
			continue
		}
		if valueNext {
			valueNext = false
			continue
		}
		v := a.val
		switch {
		case v == "--":
			return true
		case strings.HasPrefix(v, "--"):
			name, _, hasVal := strings.Cut(v[2:], "=")
			if len(name) >= 2 && abbreviates(name, "no-verify") {
				return deny()
			}
			if !hasVal && name != "" {
				for _, l := range h.valLong {
					if abbreviates(name, l) {
						valueNext = true
					}
				}
			}
		case strings.HasPrefix(v, "-") && v != "-":
			for j := 1; j < len(v); j++ {
				ch := v[j]
				if ch == 'n' && h.shortN {
					return deny()
				}
				if strings.IndexByte(h.valShort, ch) >= 0 {
					valueNext = j == len(v)-1
					break
				}
				if strings.IndexByte(h.optShort, ch) >= 0 {
					break
				}
			}
		}
	}
	return true
}

// gitRebase checks the commands of rebase --exec.
func (c *checker) gitRebase(args []arg, depth int) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a.dyn {
			continue
		}
		v := a.val
		var cmd *arg
		switch {
		case strings.HasPrefix(v, "--"):
			name, val, hasVal := strings.Cut(v[2:], "=")
			if len(name) >= 2 && abbreviates(name, "exec") {
				if hasVal {
					l := lit(val)
					cmd = &l
				} else if i+1 < len(args) {
					i++
					cmd = &args[i]
				}
			}
		case strings.HasPrefix(v, "-"):
			if k := strings.IndexByte(v, 'x'); k > 0 {
				if k < len(v)-1 {
					l := lit(v[k+1:])
					cmd = &l
				} else if i+1 < len(args) {
					i++
					cmd = &args[i]
				}
			}
		}
		if cmd != nil {
			if cmd.dyn {
				c.deny("git rebase --exec with a command that is not literal cannot be checked")
				return
			}
			c.script(cmd.val, depth+1)
		}
	}
}

// gitGrep denies --open-files-in-pager, which runs a program, and words
// that are not literal unless they are the value of -e.
func (c *checker) gitGrep(args []arg) {
	for i, a := range args {
		if a.dyn && (a.split || (!a.opaque() && (i == 0 || !args[i-1].is("-e")))) {
			c.deny("git grep with an argument that is not literal cannot be checked")
			return
		}
		v := a.val
		if v == "--" {
			return
		}
		if strings.HasPrefix(v, "--") {
			name, _, _ := strings.Cut(v[2:], "=")
			if len(name) >= 2 && abbreviates(name, "open-files-in-pager") {
				c.deny("git grep --open-files-in-pager runs a program and is not allowed")
				return
			}
			continue
		}
		if strings.HasPrefix(v, "-") && strings.IndexByte(v, 'O') > 0 {
			c.deny("git grep -O runs a program and is not allowed")
			return
		}
	}
}

// abbreviates reports whether s is a prefix of full.
func abbreviates(s, full string) bool {
	return len(s) <= len(full) && full[:len(s)] == s
}

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// pushTarget is the repository a git push runs in, relative to the
// directory of the call: the -C directories in order and --git-dir.
type pushTarget struct {
	dirs    []string
	gitDir  string
	unknown bool
}

// gitConfigTimeout bounds one read of the git configuration; the hook
// timeout of Claude Code would let the call through.
const gitConfigTimeout = 30 * time.Second

// checkPushes denies a git push when the configuration of a repository it
// may run in sets a mirror remote or a push refspec that forces or deletes,
// or when that configuration cannot be read.
func (c *checker) checkPushes() {
	if len(c.pushes) == 0 {
		return
	}
	bases, ok := c.bases()
	if !ok || c.repoUnknown {
		c.deny("git push after a change of directory or repository that is not literal cannot be checked")
		return
	}
	for _, p := range c.pushes {
		if p.unknown {
			c.deny("git push in a repository that is not literal cannot be checked")
			return
		}
		for _, b := range bases {
			dir := b
			for _, d := range p.dirs {
				dir = absPath(d, dir, c.env)
			}
			if r := pushConfigReason(dir, p.gitDir); r != "" {
				c.deny(r)
				return
			}
		}
	}
}

// pushConfigReason reads the remote push settings that git sees in dir
// (all scopes, includes and the common directory of a worktree).
func pushConfigReason(dir, gitDir string) string {
	args := []string{"config", "--null", "--get-regexp", `^remote\..*\.(mirror|push)$`}
	if gitDir != "" {
		args = append([]string{"--git-dir=" + gitDir}, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitConfigTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && len(out) == 0 {
		return "" // no such key
	}
	if err != nil {
		return "the remote configuration in " + dir + " could not be read, so git push is denied"
	}
	for _, entry := range strings.Split(string(out), "\x00") {
		key, val, hasVal := strings.Cut(entry, "\n")
		val = strings.TrimSpace(val)
		switch {
		case strings.HasSuffix(key, ".mirror") && (!hasVal || !contains([]string{"false", "no", "off", "0", ""}, strings.ToLower(val))):
			return "git push is denied: the configuration sets " + key
		case strings.HasSuffix(key, ".push") && (strings.HasPrefix(val, "+") || (strings.HasPrefix(val, ":") && val != ":")):
			return "git push is denied: the configuration sets a forcing or deleting refspec in " + key
		}
	}
	return ""
}
