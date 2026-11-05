// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"os"
	"strings"
	"testing"
)

// TestSettingsAreDocumented covers DOC-001: every setting the panel accepts
// stands in the configuration guide.
func TestSettingsAreDocumented(t *testing.T) {
	doc, err := os.ReadFile("../../docs/user/configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	for key := range settingRules {
		if !strings.Contains(string(doc), "`"+key+"`") {
			t.Errorf("%s is missing in docs/user/configuration.md", key)
		}
	}
}
