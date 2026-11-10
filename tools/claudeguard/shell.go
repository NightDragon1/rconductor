// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// maxDepth bounds nested shell strings (bash -c, eval, watch, ...).
const maxDepth = 8

// arg is a shell word after quote removal.
type arg struct {
	// val is the value of a literal word; for a dynamic word it is the
	// literal text before the first expansion.
	val string
	// suffix is the literal text after the last expansion.
	suffix string
	// dyn is set when the word contains an expansion whose value the guard
	// does not know.
	dyn bool
	// split is set when the word may expand to zero or several words
	// (unquoted expansion, pathname or brace expansion).
	split bool
}

func lit(s string) arg { return arg{val: s, suffix: s} }

// is reports whether the word is the literal s.
func (a arg) is(s string) bool { return !a.dyn && a.val == s }

// opaque reports whether a dynamic word is still one word that can be
// neither an option nor a refspec with a leading "+" or ":".
func (a arg) opaque() bool {
	return a.dyn && !a.split && a.val != "" && !strings.ContainsRune("-+:", rune(a.val[0]))
}

type checker struct {
	cwd    string
	env    env
	reason string
}

func (c *checker) deny(r string) {
	if c.reason == "" {
		c.reason = r
	}
}

func checkBash(cmd, cwd string, e env) string {
	c := &checker{cwd: cwd, env: e}
	c.script(cmd, 0)
	return c.reason
}

// script parses and checks a piece of shell code.
func (c *checker) script(src string, depth int) {
	if depth > maxDepth {
		c.deny("shell code is nested too deeply to check")
		return
	}
	f, err := syntax.NewParser().Parse(strings.NewReader(src), "")
	if err != nil {
		c.deny("the command could not be parsed as shell: " + err.Error())
		return
	}
	syntax.Walk(f, func(n syntax.Node) bool {
		if c.reason != "" {
			return false
		}
		switch n := n.(type) {
		case *syntax.Stmt:
			c.redirects(n.Redirs)
			if call, ok := n.Cmd.(*syntax.CallExpr); ok && len(call.Args) > 0 {
				args := make([]arg, len(call.Args))
				for i, w := range call.Args {
					args[i] = word(w)
				}
				c.call(args, n.Redirs, depth)
			}
		case *syntax.Assign:
			if n.Name != nil {
				c.assignName(n.Name.Value)
			}
		}
		return true
	})
}

// assignName denies variables that inject git configuration.
func (c *checker) assignName(name string) {
	if strings.HasPrefix(name, "GIT_CONFIG") {
		c.deny("setting " + name + " is not allowed; it injects git configuration")
	}
}

// redirects checks the targets of output redirections.
func (c *checker) redirects(rs []*syntax.Redirect) {
	for _, r := range rs {
		switch r.Op {
		case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.RdrAll, syntax.AppAll, syntax.RdrInOut:
			if r.Word == nil {
				continue
			}
			if a := word(r.Word); !a.dyn {
				c.writeTarget(a.val)
			}
		}
	}
}

func (c *checker) writeTarget(p string) {
	if p == "" || strings.HasPrefix(p, "/dev/") {
		return
	}
	if r := protectedPath(absPath(p, c.cwd, c.env), c.env); r != "" {
		c.deny(r)
	}
}

// word resolves a shell word after quote removal.
func word(w *syntax.Word) arg {
	var a arg
	var pre, suf strings.Builder
	add := func(s string) {
		if !a.dyn {
			pre.WriteString(s)
		}
		suf.WriteString(s)
	}
	dyn := func(split bool) {
		a.dyn = true
		a.split = a.split || split
		suf.Reset()
	}
	for _, p := range w.Parts {
		switch p := p.(type) {
		case *syntax.Lit:
			s, special := unescapeUnquoted(p.Value)
			add(s)
			if special {
				dyn(true)
			}
		case *syntax.SglQuoted:
			if p.Dollar && strings.Contains(p.Value, `\`) {
				dyn(false)
			} else {
				add(p.Value)
			}
		case *syntax.DblQuoted:
			for _, q := range p.Parts {
				if l, ok := q.(*syntax.Lit); ok {
					add(unescapeDouble(l.Value))
				} else {
					dyn(false)
				}
			}
		case *syntax.ProcSubst:
			add("/dev/fd/63")
		case *syntax.ArithmExp:
			dyn(false)
		default:
			dyn(true)
		}
	}
	a.val = pre.String()
	a.suffix = suf.String()
	return a
}

// unescapeUnquoted removes backslash quoting from an unquoted literal and
// reports whether it contains pathname or brace expansion.
func unescapeUnquoted(s string) (string, bool) {
	var b strings.Builder
	special := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '\\' && i+1 < len(s):
			i++
			if s[i] != '\n' {
				b.WriteByte(s[i])
			}
			continue
		case ch == '*' || ch == '?' || ch == '[':
			special = true
		case ch == '{':
			if end := strings.IndexByte(s[i:], '}'); end > 0 {
				inner := s[i+1 : i+end]
				if strings.Contains(inner, ",") || strings.Contains(inner, "..") {
					special = true
				}
			}
		}
		if special {
			// The literal part before the expansion is the known prefix.
			return b.String(), true
		}
		b.WriteByte(ch)
	}
	return b.String(), false
}

// unescapeDouble removes the backslash quoting of a double-quoted literal.
func unescapeDouble(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("$`\"\\\n", s[i+1]) >= 0 {
			i++
			if s[i] != '\n' {
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// call checks one simple command.
func (c *checker) call(args []arg, redirs []*syntax.Redirect, depth int) {
	if len(args) == 0 || c.reason != "" {
		return
	}
	for _, a := range args {
		if strings.Contains(a.val, cacheName) || strings.Contains(a.suffix, cacheName) {
			c.deny("the guard's build cache is not accessible from tool calls")
			return
		}
	}
	name, ok := commandName(args[0])
	if !ok {
		c.deny("the command name is not literal, so the call cannot be checked")
		return
	}
	rest := args[1:]
	switch {
	case name == "git":
		c.git(rest, depth)
	case strings.HasPrefix(name, "git-"):
		c.git(append([]arg{lit(strings.TrimPrefix(name, "git-"))}, rest...), depth)
	case name == "gh":
		c.gh(rest)
	case name == "bash" || name == "sh" || name == "dash" || name == "zsh" || name == "ksh" ||
		name == "mksh" || name == "ash" || name == "rbash":
		c.shell(name, rest, redirs, depth)
	case name == "eval":
		c.shellString("eval", rest, depth)
	case name == "alias":
		for _, a := range rest {
			if a.dyn {
				c.deny("alias with a value that is not literal")
				return
			}
			if _, v, ok := strings.Cut(a.val, "="); ok {
				c.script(v, depth+1)
			}
		}
	case name == "tee":
		for _, a := range rest {
			if !a.dyn && !strings.HasPrefix(a.val, "-") {
				c.writeTarget(a.val)
			}
		}
	case name == "find":
		c.find(rest, depth)
	case name == "xargs":
		inner, ok := c.unwrap(name, rest)
		if ok && len(inner) > 0 {
			// xargs appends words read from standard input.
			c.call(append(inner, arg{dyn: true, split: true}), nil, depth)
		}
	case name == "watch":
		c.watch(rest, depth)
	case name == "flock":
		c.flock(rest, depth)
	default:
		if _, known := wrappers[name]; known {
			if inner, ok := c.unwrap(name, rest); ok {
				c.call(inner, nil, depth)
			}
		}
	}
}

// commandName returns the program name of the first word, without its
// directory. A dynamic word is accepted only as one word whose literal
// suffix after the last expansion holds a "/" and a name.
func commandName(a arg) (string, bool) {
	if !a.dyn {
		return path.Base(a.val), a.val != ""
	}
	if a.split {
		return "", false
	}
	i := strings.LastIndexByte(a.suffix, '/')
	if i < 0 || i == len(a.suffix)-1 {
		return "", false
	}
	return a.suffix[i+1:], true
}

// shellString joins literal arguments and checks them as shell code.
func (c *checker) shellString(what string, args []arg, depth int) {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if a.dyn {
			c.deny(what + " of text that is not literal cannot be checked")
			return
		}
		parts = append(parts, a.val)
	}
	c.script(strings.Join(parts, " "), depth+1)
}

// shell checks bash, sh and similar: the string of -c, a here-document or
// here-string on standard input; a script file is not read.
func (c *checker) shell(name string, args []arg, redirs []*syntax.Redirect, depth int) {
	cflag, sflag := false, false
	i := 0
loop:
	for ; i < len(args); i++ {
		a := args[i]
		if a.dyn {
			if a.split || strings.HasPrefix(a.val, "-") || strings.HasPrefix(a.val, "+") || a.val == "" {
				c.deny(name + " with an argument that is not literal cannot be checked")
				return
			}
			break
		}
		v := a.val
		switch {
		case v == "--" || v == "-":
			i++
			break loop
		case strings.HasPrefix(v, "--"):
			if v == "--rcfile" || v == "--init-file" {
				i++
			}
		case len(v) > 1 && (v[0] == '-' || v[0] == '+'):
			for _, ch := range v[1:] {
				switch ch {
				case 'c':
					cflag = true
				case 's':
					sflag = true
				case 'o', 'O':
					i++
				}
			}
		default:
			break loop
		}
	}
	switch {
	case cflag:
		if i >= len(args) {
			return
		}
		if args[i].dyn {
			c.deny(name + " -c with a command that is not literal cannot be checked")
			return
		}
		c.script(args[i].val, depth+1)
	case i < len(args) && !sflag:
		// A script file; its content is not checked here.
	default:
		c.shellStdin(name, redirs, depth)
	}
}

// shellStdin checks the commands a shell reads from standard input.
func (c *checker) shellStdin(name string, redirs []*syntax.Redirect, depth int) {
	for _, r := range redirs {
		switch r.Op {
		case syntax.Hdoc, syntax.DashHdoc:
			if r.Hdoc == nil {
				c.script("", depth+1)
				return
			}
			var b strings.Builder
			for _, p := range r.Hdoc.Parts {
				l, ok := p.(*syntax.Lit)
				if !ok {
					c.deny(name + " reading a here-document with expansions cannot be checked")
					return
				}
				b.WriteString(l.Value)
			}
			c.script(b.String(), depth+1)
			return
		case syntax.WordHdoc:
			a := word(r.Word)
			if a.dyn {
				c.deny(name + " reading a here-string that is not literal cannot be checked")
				return
			}
			c.script(a.val, depth+1)
			return
		case syntax.RdrIn:
			// A script file on standard input; not checked here.
			return
		}
	}
	c.deny(name + " reading commands from a pipe or terminal cannot be checked")
}

// wrapSpec describes the options of a command that runs another command.
type wrapSpec struct {
	flagShort string
	valShort  string
	flagLong  []string
	valLong   []string
	// assigns accepts NAME=VALUE words before the command (env).
	assigns bool
	// numeric accepts options like -10 (nice).
	numeric bool
	// skip is the number of operands before the command (timeout).
	skip int
	// noExec lists short options after which no command runs (command -v).
	noExec string
}

var wrappers = map[string]wrapSpec{
	"env": {
		flagShort: "i0v", valShort: "uC",
		flagLong: []string{"ignore-environment", "null", "debug"},
		valLong:  []string{"unset", "chdir"}, assigns: true,
	},
	"command": {flagShort: "p", noExec: "vV"},
	"exec":    {flagShort: "cl", valShort: "a"},
	"builtin": {},
	"nice":    {valShort: "n", valLong: []string{"adjustment"}, numeric: true},
	"nohup":   {},
	"timeout": {
		flagShort: "v", valShort: "sk",
		flagLong: []string{"foreground", "preserve-status", "verbose"},
		valLong:  []string{"signal", "kill-after"}, skip: 1,
	},
	"stdbuf": {valShort: "ioe", valLong: []string{"input", "output", "error"}},
	"setsid": {flagShort: "cfw", flagLong: []string{"ctty", "fork", "wait"}},
	"ionice": {flagShort: "t", valShort: "cn", valLong: []string{"class", "classdata"}, flagLong: []string{"ignore"}},
	"sudo": {
		flagShort: "AbEHiKkllnPSsV", valShort: "CDghprTUu",
		flagLong: []string{"preserve-env", "login", "shell", "non-interactive", "stdin"},
		valLong:  []string{"user", "group", "chdir", "prompt", "host", "role", "type", "other-user"},
	},
	"doas":    {flagShort: "ns", valShort: "uC"},
	"time":    {flagShort: "apvq", valShort: "fo", flagLong: []string{"append", "portability", "verbose", "quiet"}, valLong: []string{"format", "output"}},
	"chronic": {flagShort: "ev"},
	"xargs": {
		flagShort: "0prtx", valShort: "adEILnPs",
		flagLong: []string{"null", "no-run-if-empty", "verbose", "interactive", "exit", "open-tty"},
		valLong:  []string{"arg-file", "delimiter", "eof", "replace", "max-lines", "max-args", "max-procs", "max-chars", "process-slot-var"},
	},
}

// unwrap returns the command a wrapper runs. ok is false when the call was
// denied or runs no command.
func (c *checker) unwrap(name string, args []arg) ([]arg, bool) {
	spec := wrappers[name]
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a.dyn {
			if a.split || a.val == "" || strings.HasPrefix(a.val, "-") {
				c.deny(name + " with an argument that is not literal cannot be checked")
				return nil, false
			}
			break
		}
		v := a.val
		if v == "--" {
			i++
			break
		}
		if spec.assigns && !strings.HasPrefix(v, "-") && strings.Contains(v, "=") {
			n, _, _ := strings.Cut(v, "=")
			c.assignName(n)
			continue
		}
		if !strings.HasPrefix(v, "-") || v == "-" {
			break
		}
		if strings.HasPrefix(v, "--") {
			n, _, hasVal := strings.Cut(v[2:], "=")
			switch {
			case name == "env" && n == "split-string":
				c.deny("env --split-string cannot be checked")
				return nil, false
			case contains(spec.valLong, n):
				if !hasVal {
					i++
				}
			case contains(spec.flagLong, n):
			default:
				c.deny(name + " with the unknown option --" + n + " cannot be checked")
				return nil, false
			}
			continue
		}
		if spec.numeric && isDigits(v[1:]) {
			continue
		}
		for j := 1; j < len(v); j++ {
			ch := v[j]
			switch {
			case strings.IndexByte(spec.noExec, ch) >= 0:
				return nil, false
			case name == "env" && ch == 'S':
				c.deny("env -S cannot be checked")
				return nil, false
			case strings.IndexByte(spec.valShort, ch) >= 0:
				if j == len(v)-1 {
					i++
				}
				j = len(v)
			case strings.IndexByte(spec.flagShort, ch) >= 0:
			default:
				c.deny(name + " with the unknown option -" + string(ch) + " cannot be checked")
				return nil, false
			}
		}
	}
	for k := 0; k < spec.skip && i < len(args); k++ {
		if args[i].split {
			c.deny(name + " with an operand that is not literal cannot be checked")
			return nil, false
		}
		i++
	}
	if i >= len(args) {
		return nil, false
	}
	return args[i:], true
}

// find checks the commands of -exec, -execdir, -ok and -okdir.
func (c *checker) find(args []arg, depth int) {
	for i := 0; i < len(args); i++ {
		if args[i].dyn {
			continue
		}
		switch args[i].val {
		case "-exec", "-execdir", "-ok", "-okdir":
		default:
			continue
		}
		var cmd []arg
		j := i + 1
		for ; j < len(args); j++ {
			if args[j].is(";") || args[j].is("+") {
				break
			}
			a := args[j]
			if !a.dyn && strings.Contains(a.val, "{}") {
				before, _, _ := strings.Cut(a.val, "{}")
				a = arg{val: before, dyn: true, split: j+1 < len(args) && args[j+1].is("+")}
			}
			cmd = append(cmd, a)
		}
		c.call(cmd, nil, depth)
		i = j
	}
}

// watch checks the command of watch, which runs it through sh -c unless
// -x is given.
func (c *checker) watch(args []arg, depth int) {
	execMode := false
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a.dyn {
			if a.split || a.val == "" || strings.HasPrefix(a.val, "-") {
				c.deny("watch with an argument that is not literal cannot be checked")
				return
			}
			break
		}
		v := a.val
		if v == "--" {
			i++
			break
		}
		if !strings.HasPrefix(v, "-") {
			break
		}
		if strings.HasPrefix(v, "--") {
			n, _, hasVal := strings.Cut(v[2:], "=")
			switch n {
			case "exec":
				execMode = true
			case "interval", "differences", "chgexit", "beep", "errexit", "no-title", "color", "no-color", "precise", "no-wrap", "equexit", "no-linewrap":
				if n == "interval" && !hasVal {
					i++
				}
			default:
				c.deny("watch with the unknown option --" + n + " cannot be checked")
				return
			}
			continue
		}
		for j := 1; j < len(v); j++ {
			switch ch := v[j]; {
			case ch == 'x':
				execMode = true
			case ch == 'n' || ch == 'q':
				if j == len(v)-1 {
					i++
				}
				j = len(v)
			case strings.IndexByte("dgtbecpwr", ch) >= 0:
			default:
				c.deny("watch with the unknown option -" + string(ch) + " cannot be checked")
				return
			}
		}
	}
	if i >= len(args) {
		return
	}
	if execMode {
		c.call(args[i:], nil, depth)
		return
	}
	c.shellString("watch", args[i:], depth)
}

// flock checks "flock [options] file command..." and "flock file -c cmd".
func (c *checker) flock(args []arg, depth int) {
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a.dyn {
			if a.split || a.val == "" || strings.HasPrefix(a.val, "-") {
				c.deny("flock with an argument that is not literal cannot be checked")
				return
			}
			break
		}
		v := a.val
		if !strings.HasPrefix(v, "-") {
			break
		}
		switch v {
		case "-s", "--shared", "-x", "-e", "--exclusive", "-u", "--unlock", "-n", "--nb", "--nonblock",
			"-o", "--close", "-F", "--no-fork", "--verbose":
		case "-w", "--wait", "--timeout", "-E", "--conflict-exit-code":
			i++
		case "-c", "--command":
			c.deny("flock -c before the lock file cannot be checked")
			return
		default:
			c.deny("flock with the unknown option " + v + " cannot be checked")
			return
		}
	}
	i++ // the lock file or descriptor
	if i >= len(args) {
		return
	}
	if args[i].is("-c") || args[i].is("--command") {
		if i+1 < len(args) {
			c.shellString("flock -c", args[i+1:i+2], depth)
		}
		return
	}
	c.call(args[i:], nil, depth)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
