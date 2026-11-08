// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/doccheck"
)

const guide = "../../docs/user/configuration.md"

// TestSettingsAreDocumented covers DOC-001: every setting the panel accepts
// stands in the configuration guide.
func TestSettingsAreDocumented(t *testing.T) {
	doc, err := os.ReadFile(guide)
	if err != nil {
		t.Fatal(err)
	}
	for key := range settingRules {
		if !strings.Contains(string(doc), "`"+key+"`") {
			t.Errorf("%s is missing in docs/user/configuration.md", key)
		}
	}
}

var (
	intLimits      = regexp.MustCompile(`^a whole number from (\d+) to (\d+)$`)
	durationLimits = regexp.MustCompile(`^a duration from (\S+) to (\S+)$`)
)

// TestSettingLimitsAreDocumented covers DOC-001: the table of settings
// has a row for each setting the panel accepts and none else, with the
// type and limits the panel checks. The limits come from the error of an
// invalid value; the defaults are checked in internal/app.
func TestSettingLimitsAreDocumented(t *testing.T) {
	doc, err := os.ReadFile(guide)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := doccheck.Section(string(doc), "Settings in the panel")
	if err != nil {
		t.Fatal(err)
	}
	for key := range rows {
		if _, ok := settingRules[key]; !ok {
			t.Errorf("%s stands in the guide, but the panel does not accept it", key)
		}
	}
	for key, rule := range settingRules {
		row, ok := rows[key]
		if !ok {
			t.Errorf("%s is missing in the table of settings", key)
			continue
		}
		_, err := rule(json.RawMessage(`"not valid"`))
		if err == nil {
			t.Errorf("%s: rule accepts any value", key)
			continue
		}
		lo, hi, inGuide := doccheck.Range(row["Allowed"])
		switch msg := err.Error(); {
		case intLimits.MatchString(msg):
			m := intLimits.FindStringSubmatch(msg)
			if row["Type"] != "number" || !inGuide || lo != m[1] || hi != m[2] {
				t.Errorf("%s: guide has %s %q, panel a number from %s to %s", key, row["Type"], row["Allowed"], m[1], m[2])
			}
		case durationLimits.MatchString(msg):
			m := durationLimits.FindStringSubmatch(msg)
			if row["Type"] != "duration" || !inGuide || !sameDuration(lo, m[1]) || !sameDuration(hi, m[2]) {
				t.Errorf("%s: guide has %s %q, panel a duration from %s to %s", key, row["Type"], row["Allowed"], m[1], m[2])
			}
		case msg == "true or false":
			if row["Type"] != "boolean" || row["Allowed"] != "on, off" {
				t.Errorf("%s: guide has %s %q, panel a boolean", key, row["Type"], row["Allowed"])
			}
		default:
			t.Errorf("%s: unknown kind of rule: %s", key, msg)
		}
	}
}

func sameDuration(guide, code string) bool {
	g, err := doccheck.Duration(guide)
	if err != nil {
		return false
	}
	c, err := time.ParseDuration(code)
	return err == nil && g == c
}
