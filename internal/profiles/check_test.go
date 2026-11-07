// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const checkHead = `schema: 1
id: broken
name: Broken
protocol:
  type: source-rcon
  default_port: 25575
catalog:
`

func writeProfile(t *testing.T, body string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "p.yaml")
	if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

func runCheck(args ...string) (int, string, string) {
	var out, errs bytes.Buffer
	code := RunCheck(args, &out, &errs)
	return code, out.String(), errs.String()
}

// TestCheckNamesEveryProblemWithItsLine covers CAT-006: all problems of the
// schema, each with the line of its field, and exit code 1.
func TestCheckNamesEveryProblemWithItsLine(t *testing.T) {
	name := writeProfile(t, checkHead+`  - path: list
    description: Show players
  - path: give
    description: Give an item
    params:
      - { name: item, type: text }
      - { name: count, type: number }
  - path: kick
  - path: list
    description: Twice
`)
	code, out, _ := runCheck(name)
	if code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
	for _, want := range []string{
		name + ":13: catalog[1].params[0]: text is allowed only as last param",
		name + ":15: catalog[2].description is required",
		name + ":16: catalog[3].path \"list\" is listed twice",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

// TestCheckYAMLError: a syntax error, as in the description with ": " of
// #201, comes with the line of the YAML parser.
func TestCheckYAMLError(t *testing.T) {
	name := writeProfile(t, checkHead+`  - path: backup start
    description: Start a backup (2.9.x: =oc only claimed chunks)
`)
	code, out, _ := runCheck(name)
	if code != 1 || !strings.HasPrefix(out, name+":9: ") {
		t.Errorf("exit code %d, output %q", code, out)
	}
	unknown := writeProfile(t, checkHead+"  - path: list\n    description: x\n    categry: players\n")
	if code, out, _ := runCheck(unknown); code != 1 || !strings.HasPrefix(out, unknown+":10: ") {
		t.Errorf("unknown field: exit code %d, output %q", code, out)
	}
}

// TestCheckShippedProfiles: the shipped profiles pass with exit code 0.
func TestCheckShippedProfiles(t *testing.T) {
	files, err := filepath.Glob("../../profiles/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no shipped profiles: %v", err)
	}
	code, out, errs := runCheck(files...)
	if code != 0 || errs != "" || strings.Count(out, ": ok\n") != len(files) {
		t.Errorf("exit code %d, output %q, errors %q", code, out, errs)
	}
}

// TestCheckMisuse: no file, a missing file and a file over the size limit.
func TestCheckMisuse(t *testing.T) {
	if code, _, errs := runCheck(); code != 2 || !strings.Contains(errs, "usage") {
		t.Errorf("no file: %d %q", code, errs)
	}
	good := writeProfile(t, checkHead+"  - path: list\n    description: x\n")
	if code, out, errs := runCheck(good, filepath.Join(t.TempDir(), "missing.yaml")); code != 2 || !strings.Contains(out, good+": ok") || errs == "" {
		t.Errorf("missing file: %d %q %q", code, out, errs)
	}
	big := writeProfile(t, checkHead+strings.Repeat("# filler line of a large profile\n", MaxFileSize/32+1))
	if code, out, _ := runCheck(big); code != 1 || !strings.Contains(out, "file larger than") {
		t.Errorf("large file: %d %q", code, out)
	}
}
