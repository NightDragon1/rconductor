// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// Finding is one problem of a profile file; Line is 0 when no line fits.
type Finding struct {
	Line    int
	Message string
}

var (
	// yamlPos is the position goccy/go-yaml puts before its errors.
	yamlPos = regexp.MustCompile(`^\[(\d+):\d+\]\s*`)
	// problemPath is the field a problem of check() starts with, such as
	// catalog[8].params[0].
	problemPath = regexp.MustCompile(`^[a-z_]+(?:\[\d+\]|\.[a-z_]+)*`)
	lastSegment = regexp.MustCompile(`(?:\[\d+\]|\.[a-z_]+)$`)
)

// Check reads a profile file by the rules of the import and returns every
// problem with its line (CAT-006); nil means the file is valid.
func Check(data []byte) []Finding {
	_, file, err := parse(data)
	if err == nil {
		return nil
	}
	var pe *Error
	if !errors.As(err, &pe) {
		return []Finding{{Message: err.Error()}}
	}
	out := make([]Finding, 0, len(pe.Problems))
	for _, m := range pe.Problems {
		m = strings.TrimSpace(m)
		if g := yamlPos.FindStringSubmatch(m); g != nil {
			line, _ := strconv.Atoi(g[1])
			out = append(out, Finding{Line: line, Message: strings.TrimSpace(m[len(g[0]):])})
			continue
		}
		f := Finding{Message: m}
		if file != nil {
			f.Line = lineOf(file, problemPath.FindString(m))
		}
		out = append(out, f)
	}
	return out
}

// lineOf finds the line of a field path, or of the nearest parent that is
// in the file, such as the command of a missing description.
func lineOf(file *ast.File, path string) int {
	for path != "" {
		if p, err := yaml.PathString("$." + path); err == nil {
			if n, err := p.FilterFile(file); err == nil && n != nil && n.GetToken() != nil {
				return n.GetToken().Position.Line
			}
		}
		if lastSegment.FindStringIndex(path) == nil {
			return 0
		}
		path = lastSegment.ReplaceAllString(path, "")
	}
	return 0
}

// RunCheck is the command "profile check <file>...": one line per problem
// as file:line: message, exit code 0 without problems, 1 with problems and
// 2 for a wrong call or a file that cannot be read.
func RunCheck(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: profile check <file>...")
		return 2
	}
	code := 0
	for _, name := range args {
		data, err := readLimited(name)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: %s\n", printable(name), printable(err.Error()))
			code = 2
			continue
		}
		findings := Check(data)
		if len(findings) == 0 {
			_, _ = fmt.Fprintf(stdout, "%s: ok\n", printable(name))
			continue
		}
		for _, f := range findings {
			if f.Line > 0 {
				_, _ = fmt.Fprintf(stdout, "%s:%d: %s\n", printable(name), f.Line, printable(f.Message))
			} else {
				_, _ = fmt.Fprintf(stdout, "%s: %s\n", printable(name), printable(f.Message))
			}
		}
		if code == 0 {
			code = 1
		}
	}
	return code
}

// printable writes control characters from a foreign profile as \x..,
// so that they never reach the terminal.
func printable(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			fmt.Fprintf(&b, "\\x%02x", r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
