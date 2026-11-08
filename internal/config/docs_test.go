// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/doccheck"
)

const guide = "../../docs/user/configuration.md"

// TestVariablesAreDocumented covers DOC-001: every variable the panel and
// the agent read stands in the configuration guide.
func TestVariablesAreDocumented(t *testing.T) {
	doc, err := os.ReadFile(guide)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	record := func(key string) (string, bool) {
		seen[key] = true
		return "", false
	}
	noFile := func(string) ([]byte, error) { return nil, os.ErrNotExist }
	if _, err := LoadPanel(record, noFile); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAgent(record, noFile); err != nil {
		t.Fatal(err)
	}
	if len(seen) < 20 {
		t.Fatalf("only %d variables read", len(seen))
	}
	for key := range seen {
		if !strings.Contains(string(doc), "`"+key+"`") {
			t.Errorf("%s is missing in docs/user/configuration.md", key)
		}
	}
}

// docDefault are defaults the guide words in its own way: the agent takes
// the host name when RCD_AGENT_NAME is empty.
var docDefault = map[string]string{"RCD_AGENT_NAME": "host name"}

// notRead are variables of a table that the Go runtime reads, not this
// package.
var notRead = map[string]bool{"GOMEMLIMIT": true}

// TestVariableDefaultsAndLimits covers DOC-001: default and limits of
// every variable in the tables of the guide are the ones the code applies,
// and each row names a variable the program reads. The default is the
// value of the field a valid value of the variable changes; the limits
// come from the error message of an invalid value.
func TestVariableDefaultsAndLimits(t *testing.T) {
	doc, err := os.ReadFile(guide)
	if err != nil {
		t.Fatal(err)
	}
	noFile := func(string) ([]byte, error) { return nil, os.ErrNotExist }
	for _, c := range []struct {
		section string
		load    func(Lookup) (any, error)
	}{
		{"Panel variables", func(l Lookup) (any, error) { return LoadPanel(l, noFile) }},
		{"Agent variables", func(l Lookup) (any, error) { return LoadAgent(l, noFile) }},
	} {
		t.Run(c.section, func(t *testing.T) {
			rows, err := doccheck.Section(string(doc), c.section)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			base, err := c.load(func(key string) (string, bool) {
				seen[key] = true
				return "", false
			})
			if err != nil {
				t.Fatal(err)
			}
			for key := range rows {
				if !seen[key] && !notRead[key] {
					t.Errorf("%s stands in %q, but the program does not read it", key, c.section)
				}
			}
			for key := range seen {
				row, ok := rows[key]
				if !ok {
					t.Errorf("%s is missing in the table %q", key, c.section)
					continue
				}
				_, err := c.load(only(key, "not valid"))
				lo, hi, isInt := intLimits(err, key)
				if isInt {
					dlo, dhi, ok := doccheck.Range(row["Allowed"])
					if !ok || dlo != lo || dhi != hi {
						t.Errorf("%s: guide allows %q, code %s to %s", key, row["Allowed"], lo, hi)
					}
				}
				if strings.HasSuffix(key, "_FILE") {
					continue // the file of a secret; its row is the secret's
				}
				want, found := defaultOf(base, c.load, key, probes(lo, hi))
				if !found {
					t.Errorf("%s: no value changes the configuration", key)
					continue
				}
				if d, ok := docDefault[key]; ok {
					if want != "" || doccheck.Plain(row["Default"]) != d {
						t.Errorf("%s: guide says default %q, code %q", key, row["Default"], want)
					}
					continue
				}
				if got := doccheck.Plain(row["Default"]); got != want {
					t.Errorf("%s: guide says default %q, code %q", key, got, want)
				}
			}
		})
	}
}

// TestReadmeVariablesMatchTheGuide covers DOC-001: the short table of
// the README names only variables of the guide, with the same default.
func TestReadmeVariablesMatchTheGuide(t *testing.T) {
	doc, err := os.ReadFile(guide)
	if err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	short, err := doccheck.Section(string(readme), "Important variables")
	if err != nil {
		t.Fatal(err)
	}
	full := map[string]doccheck.Row{}
	for _, s := range []string{"Compose files", "Panel variables", "Agent variables"} {
		rows, err := doccheck.Section(string(doc), s)
		if err != nil {
			t.Fatal(err)
		}
		for k, r := range rows {
			full[k] = r
		}
	}
	for key, row := range short {
		r, ok := full[key]
		if !ok {
			t.Errorf("README names %s, the guide does not", key)
			continue
		}
		if got, want := doccheck.Plain(row["Default"]), doccheck.Plain(r["Default"]); got != want {
			t.Errorf("%s: README says default %q, the guide %q", key, got, want)
		}
	}
}

func only(key, value string) Lookup {
	return func(k string) (string, bool) {
		if k == key {
			return value, true
		}
		return "", false
	}
}

func intLimits(err error, key string) (lo, hi string, ok bool) {
	if err == nil {
		return "", "", false
	}
	m := regexp.MustCompile(regexp.QuoteMeta(key) + `: must be a whole number from (\d+) to (\d+)`).FindStringSubmatch(err.Error())
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// probes are values tried in turn until one changes the configuration.
func probes(lo, hi string) []string {
	p := []string{"1", ":1", "/zz", "http://a", "10.0.0.1", "true", "false", "debug", "json", "a=b"}
	if lo != "" {
		p = append([]string{lo, hi}, p...)
	}
	return p
}

// defaultOf loads the configuration with key set to each probe and returns
// the value the first changed field has without it.
func defaultOf(base any, load func(Lookup) (any, error), key string, probes []string) (string, bool) {
	for _, p := range probes {
		got, _ := load(only(key, p))
		var diff []string
		changed(reflect.ValueOf(base), reflect.ValueOf(got), &diff)
		switch len(diff) {
		case 0:
			continue
		case 1:
			return diff[0], true
		default:
			// RCD_LABEL fills key and value of the label.
			return strings.Join(diff, "="), true
		}
	}
	return "", false
}

func changed(a, b reflect.Value, out *[]string) {
	if a.Kind() == reflect.Struct {
		for i := range a.NumField() {
			changed(a.Field(i), b.Field(i), out)
		}
		return
	}
	if !reflect.DeepEqual(a.Interface(), b.Interface()) {
		*out = append(*out, format(a))
	}
}

func format(v reflect.Value) string {
	switch x := v.Interface().(type) {
	case time.Duration:
		return strconv.Itoa(int(x / time.Second))
	case slog.Level:
		return strings.ToLower(x.String())
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Slice:
		if v.IsNil() {
			return ""
		}
	}
	return fmt.Sprint(v.Interface())
}
