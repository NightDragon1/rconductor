// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"os"
	"strings"
	"testing"
)

// TestVariablesAreDocumented covers DOC-001: every variable the panel and
// the agent read stands in the configuration guide.
func TestVariablesAreDocumented(t *testing.T) {
	doc, err := os.ReadFile("../../docs/user/configuration.md")
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
