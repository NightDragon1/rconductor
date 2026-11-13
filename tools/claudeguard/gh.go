// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"slices"
	"strings"
)

// ghRule is the decision for a gh subcommand.
type ghRule int

const (
	ghAllow ghRule = iota + 1
	ghDeny
	ghPRMerge
	ghRepoSync
	ghDeployKey
	ghWorkflowRun
)

// ghTop lists the top-level gh commands. A command with a nil map allows
// every subcommand; a missing command is denied.
var ghTop = map[string]map[string]ghRule{
	"search":     nil,
	"status":     nil,
	"browse":     nil,
	"completion": nil,
	"version":    nil,
	"help":       nil,
	"pr": {
		"create": ghAllow, "new": ghAllow, "view": ghAllow, "list": ghAllow, "ls": ghAllow,
		"comment": ghAllow, "checks": ghAllow, "diff": ghAllow, "status": ghAllow, "ready": ghAllow,
		"close": ghAllow, "edit": ghAllow, "checkout": ghAllow, "co": ghAllow, "reopen": ghAllow,
		"review": ghAllow, "update-branch": ghAllow, "merge": ghPRMerge,
	},
	"issue": {
		"create": ghAllow, "new": ghAllow, "view": ghAllow, "list": ghAllow, "ls": ghAllow,
		"comment": ghAllow, "close": ghAllow, "edit": ghAllow, "reopen": ghAllow, "status": ghAllow,
		"develop": ghAllow,
	},
	"run": {
		"list": ghAllow, "ls": ghAllow, "view": ghAllow, "watch": ghAllow, "rerun": ghAllow,
		"download": ghAllow,
	},
	"workflow": {"list": ghAllow, "ls": ghAllow, "view": ghAllow, "run": ghWorkflowRun},
	"repo": {
		"view": ghAllow, "list": ghAllow, "ls": ghAllow, "clone": ghAllow, "set-default": ghAllow,
		"sync": ghRepoSync, "deploy-key": ghDeployKey,
	},
	"release":   {"view": ghAllow, "list": ghAllow, "ls": ghAllow},
	"label":     {"list": ghAllow, "ls": ghAllow, "create": ghAllow, "edit": ghAllow},
	"auth":      {"status": ghAllow},
	"ssh-key":   {"list": ghAllow, "ls": ghAllow},
	"gpg-key":   {"list": ghAllow, "ls": ghAllow},
	"extension": {"list": ghAllow, "ls": ghAllow, "search": ghAllow},
	"alias":     {"list": ghAllow, "ls": ghAllow},
	"config":    {"get": ghAllow, "list": ghAllow},
	"cache":     {"list": ghAllow, "ls": ghAllow},
	"api":       nil, // checked by ghAPI
}

var ghTopAliases = map[string]string{"extensions": "extension", "ext": "extension"}

// gh checks a gh call; args follow the program name.
func (c *checker) gh(args []arg) {
	if len(args) == 0 {
		return
	}
	top := args[0]
	if top.dyn {
		c.deny("the gh command is not literal, so the call cannot be checked")
		return
	}
	if strings.HasPrefix(top.val, "-") {
		if len(args) == 1 && (top.val == "--version" || top.val == "--help" || top.val == "-h") {
			return
		}
		c.deny("gh options before the command cannot be checked")
		return
	}
	name := top.val
	if a, ok := ghTopAliases[name]; ok {
		name = a
	}
	subs, known := ghTop[name]
	if !known {
		c.deny("gh " + name + " is not on the guard's list of known commands")
		return
	}
	if name == "api" {
		c.ghAPI(args[1:])
		return
	}
	if subs == nil || len(args) == 1 {
		return
	}
	sub := args[1]
	if sub.dyn {
		c.deny("the gh subcommand is not literal, so the call cannot be checked")
		return
	}
	if sub.val == "--help" || sub.val == "-h" {
		return
	}
	if strings.HasPrefix(sub.val, "-") {
		c.deny("gh options before the subcommand cannot be checked")
		return
	}
	rest := args[2:]
	switch subs[sub.val] {
	case ghAllow:
	case ghPRMerge:
		c.ghFlagDenied("gh pr merge", rest, "admin", setOf("-b", "--body", "-F", "--body-file", "-t",
			"--subject", "-A", "--author-email", "--match-head-commit", "-R", "--repo"))
	case ghRepoSync:
		c.ghFlagDenied("gh repo sync", rest, "force", setOf("-b", "--branch", "-s", "--source"))
	case ghDeployKey:
		if len(rest) > 0 && (rest[0].is("list") || rest[0].is("ls")) {
			return
		}
		c.deny("gh repo deploy-key may only list keys from a tool call")
	case ghWorkflowRun:
		if c.ghRepoEnv || !workflowRunAllowed(rest, c.env.workflowForm) {
			c.deny("gh workflow run is allowed only for ci.yml on main and the one configured form")
		}
	default:
		c.deny("gh " + name + " " + sub.val + " is not on the guard's list of allowed subcommands")
	}
}

// ghFlagDenied denies the long flag name in any pflag form. A dynamic
// argument is accepted only as the value of a value flag or as one word
// that cannot be an option.
func (c *checker) ghFlagDenied(cmd string, args []arg, flag string, valFlags map[string]bool) {
	for i, a := range args {
		if a.dyn {
			prevVal := i > 0 && !args[i-1].dyn && valFlags[args[i-1].val]
			if (!prevVal || a.split) && !a.opaque() {
				c.deny(cmd + " with an argument that is not literal cannot be checked")
				return
			}
			continue
		}
		if a.val == "--" {
			return
		}
		if a.val == "--"+flag || strings.HasPrefix(a.val, "--"+flag+"=") {
			c.deny(cmd + " --" + flag + " is not allowed")
			return
		}
	}
}

var (
	ghAPIValLong  = setOf("method", "field", "raw-field", "input", "header", "jq", "template", "cache", "hostname", "preview")
	ghAPIBoolLong = setOf("include", "paginate", "slurp", "silent", "verbose", "help")
	ghAPIValShort = "XFfHqtp"
	ghAPIBoolShrt = "ih"
)

// ghAPI checks gh api: only GET or HEAD without fields or input, and no
// GraphQL.
func (c *checker) ghAPI(args []arg) {
	type flag struct {
		name string
		val  *arg
	}
	var flags []flag
	var operands []arg
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a.dyn {
			if !a.opaque() {
				c.deny("gh api with an argument that is not literal cannot be checked")
				return
			}
			operands = append(operands, a)
			continue
		}
		v := a.val
		switch {
		case v == "--":
			operands = append(operands, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(v, "--"):
			name, val, hasVal := strings.Cut(v[2:], "=")
			switch {
			case ghAPIValLong[name]:
				f := flag{name: name}
				if hasVal {
					l := lit(val)
					f.val = &l
				} else if i+1 < len(args) {
					i++
					f.val = &args[i]
				}
				flags = append(flags, f)
			case ghAPIBoolLong[name]:
			default:
				c.deny("gh api with the unknown option --" + name)
				return
			}
		case strings.HasPrefix(v, "-") && v != "-":
			for j := 1; j < len(v); j++ {
				ch := v[j]
				switch {
				case strings.IndexByte(ghAPIValShort, ch) >= 0:
					f := flag{name: string(ch)}
					if rest := strings.TrimPrefix(v[j+1:], "="); j+1 < len(v) {
						l := lit(rest)
						f.val = &l
					} else if i+1 < len(args) {
						i++
						f.val = &args[i]
					}
					flags = append(flags, f)
					j = len(v)
				case strings.IndexByte(ghAPIBoolShrt, ch) >= 0:
					if j+1 < len(v) && v[j+1] == '=' {
						j = len(v)
					}
				default:
					c.deny("gh api with the unknown option -" + string(ch))
					return
				}
			}
		default:
			operands = append(operands, a)
		}
	}
	for _, f := range flags {
		switch f.name {
		case "method", "X":
			if f.val == nil || f.val.dyn {
				c.deny("gh api with a method that is not literal cannot be checked")
				return
			}
			if m := strings.ToUpper(f.val.val); m != "GET" && m != "HEAD" {
				c.deny("gh api with method " + m + " writes and is not allowed")
				return
			}
		case "field", "raw-field", "input", "F", "f":
			c.deny("gh api with a field or input sends a request body and is not allowed")
			return
		}
	}
	for _, o := range operands {
		ep := strings.ToLower(strings.TrimPrefix(o.val, "/"))
		if ep == "graphql" || (o.dyn && abbreviates(ep, "graphql")) {
			c.deny("gh api graphql is not allowed")
			return
		}
	}
}

// workflowRunAllowed accepts exactly two literal argument lists of gh
// workflow run: ci.yml on main, and the one form configured outside the
// repository (see loadWorkflowForm), whose <sha> is one full commit SHA.
func workflowRunAllowed(args []arg, form []string) bool {
	vals := make([]string, len(args))
	for i, a := range args {
		if a.dyn {
			return false
		}
		vals[i] = a.val
	}
	if slices.Equal(vals, []string{"ci.yml", "--ref", "main"}) {
		return true
	}
	if len(form) == 0 || len(vals) != len(form) {
		return false
	}
	for i, want := range form {
		pre, ok := strings.CutSuffix(want, shaPlaceholder)
		if !ok {
			if vals[i] != want {
				return false
			}
			continue
		}
		sha, ok := strings.CutPrefix(vals[i], pre)
		if !ok || len(sha) != 40 || strings.Trim(sha, "0123456789abcdef") != "" {
			return false
		}
	}
	return true
}
